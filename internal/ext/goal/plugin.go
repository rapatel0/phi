package goal

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools"
)

func init() { ext.Register(&Plugin{}) }

// Plugin keeps the agent working toward a session goal.
type Plugin struct {
	state State
}

func (*Plugin) Name() string { return "goal" }

func (p *Plugin) Register(h *ext.Host) error {
	h.RegisterCommand(ext.Command{
		Name:        "goal",
		Description: "Set a session goal. /goal <objective>, or status, pause, resume, clear",
		Run:         p.run,
	})

	// The reminder is appended each turn, so the objective stays in front of
	// the model rather than fading as the conversation grows.
	h.OnBeforeAgentStart(func(_ context.Context, _, systemPrompt string) (string, error) {
		g := p.state.RecordTurn()
		reminder := Reminder(g)
		if reminder == "" {
			return stripReminder(systemPrompt), nil
		}
		return stripReminder(systemPrompt) + reminder, nil
	})

	h.RegisterTool(closeTool(
		"goal_complete",
		"Mark the active goal complete. Use only after the work is done and verified.",
		"summary", "What was completed, and what evidence verified it.",
		func(id, text string) (*Goal, error) { return p.state.Complete(id, text) },
	))
	h.RegisterTool(closeTool(
		"goal_blocked",
		"Stop the active goal because user or external action is required.",
		"reason", "The specific action required to unblock the goal.",
		func(id, text string) (*Goal, error) { return p.state.Block(id, text) },
	))
	h.RegisterTool(closeTool(
		"goal_wait",
		"Park the active goal until an external event arrives. It can resume later.",
		"reason", "Which event the goal is waiting for.",
		func(id, text string) (*Goal, error) { return p.state.Wait(id, text) },
	))

	h.AddFooter(func() string { return Footer(p.state.Current()) })
	return nil
}

// closeTool builds one of the three closing tools. They differ only in the
// name, the text field, and which transition they make.
func closeTool(name, desc, field, fieldDesc string, apply func(id, text string) (*Goal, error)) tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        name,
			Description: desc,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"goal_id": llm.Object{
						"type":        "string",
						"description": "The goal id shown in the active goal block. Guards against closing a newer goal.",
					},
					field: llm.Object{"type": "string", "description": fieldDesc},
				},
				Required: []string{"goal_id", field},
			},
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in map[string]string
			_ = json.Unmarshal(raw, &in)
			return in["goal_id"]
		},
		Run: func(_ context.Context, raw json.RawMessage) (tools.Result, error) {
			var in map[string]string
			if err := json.Unmarshal(raw, &in); err != nil {
				return tools.Result{}, err
			}
			g, err := apply(in["goal_id"], in[field])
			if err != nil {
				// Report rather than fail the turn: the model can correct
				// a stale id or supply the missing text.
				body := mustJSON(map[string]any{"ok": false, "error": err.Error()})
				return tools.Result{Content: body, Detail: "rejected", Output: body}, nil
			}
			body := mustJSON(map[string]any{"ok": true, "status": string(g.Status)})
			return tools.Result{Content: body, Detail: string(g.Status), Output: body}, nil
		},
	}
}

func (p *Plugin) run(_ context.Context, args []string) (hooks.CommandResult, error) {
	if len(args) == 0 {
		return hooks.CommandResult{Toast: Describe(p.state.Current())}, nil
	}

	switch strings.ToLower(args[0]) {
	case "status":
		return hooks.CommandResult{Toast: Describe(p.state.Current())}, nil
	case "clear":
		p.state.Clear()
		return hooks.CommandResult{Toast: "Goal cleared.", StatusSet: true}, nil
	case "pause":
		g, err := p.state.Pause()
		if err != nil {
			return hooks.CommandResult{}, err
		}
		return goalResult(g, "Goal paused."), nil
	case "resume":
		g, err := p.state.Resume()
		if err != nil {
			return hooks.CommandResult{}, err
		}
		return goalResult(g, "Goal resumed."), nil
	}

	objective, maxTurns := parseStart(args)
	g, err := p.state.Start(objective, maxTurns)
	if err != nil {
		return hooks.CommandResult{}, err
	}
	return goalResult(g, "Goal set: "+g.Objective), nil
}

// parseStart reads the objective and an optional --turns N limit.
func parseStart(args []string) (string, int) {
	var words []string
	maxTurns := 0
	for i := 0; i < len(args); i++ {
		if args[i] == "--turns" && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				maxTurns = n
				i++
				continue
			}
		}
		words = append(words, args[i])
	}
	return strings.Join(words, " "), maxTurns
}

func goalResult(g *Goal, toast string) hooks.CommandResult {
	return hooks.CommandResult{Toast: toast, Status: Footer(g), StatusSet: true}
}

// reminderHeader marks the injected block so a later turn replaces it rather
// than appending another copy.
const reminderHeader = "\n\n# Active goal\n"

// stripReminder removes a block a previous turn appended.
func stripReminder(prompt string) string {
	before, _, _ := strings.Cut(prompt, reminderHeader)
	return before
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"error":"marshal failed"}`
	}
	return string(b)
}
