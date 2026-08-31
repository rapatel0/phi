package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/permission"
	"github.com/rapatel0/alpha/internal/tools"
	"github.com/rapatel0/alpha/internal/util"
)

func init() { ext.Register(&Plugin{}) }

// Plugin registers the loop and monitor tools.
//
// A loop keeps a schedule outside the conversation; a monitor keeps a long
// command outside the turn. Both exist so the model stops reaching for a shell
// `sleep`, which blocks the tool loop for the whole wait.
type Plugin struct {
	store    *Store
	monitors *Monitors
	sched    *Scheduler
	host     *ext.Host
}

func (*Plugin) Name() string { return "loop" }

// Register wires the tools. The scheduler starts only once the shell installs
// a wake function, because a loop with nowhere to fire is worse than no loop.
func (p *Plugin) Register(h *ext.Host) error {
	p.store = NewStore()
	p.host = h
	p.monitors = NewMonitors(nil)
	p.sched = NewScheduler(p.store, h.Wake)

	h.RegisterTool(p.loopCreateTool())
	h.RegisterTool(p.loopListTool())
	h.RegisterTool(p.loopUpdateTool())
	h.RegisterTool(p.loopDeleteTool())
	h.RegisterTool(p.monitorCreateTool())
	h.RegisterTool(p.monitorListTool())
	h.RegisterTool(p.monitorLogsTool())
	h.RegisterTool(p.monitorStopTool())

	h.AddFooter(p.footer)
	return nil
}

// SetGate installs the permission gate for background commands and starts the
// scheduler. The shell calls it once the gate exists.
//
// Monitors refuse to run until this happens: a background command must pass
// the same gate as bash, and a supervisor without one would be a way around it.
func (p *Plugin) SetGate(gate permission.Gate) {
	p.monitors.gate = gate
}

// Start begins firing loops. Stop halts them and kills background commands.
func (p *Plugin) Start(ctx context.Context) { p.sched.Start(ctx) }

// Stop halts the scheduler and every background command, so nothing outlives
// the session that started it.
func (p *Plugin) Stop() {
	p.sched.Stop()
	p.monitors.StopAll()
}

// footer reports active loops and running commands, because background work
// nobody can see is background work nobody stops.
func (p *Plugin) footer() string {
	var bits []string
	active := 0
	for _, l := range p.store.List() {
		if l.State == StateActive {
			active++
		}
	}
	if active > 0 {
		bits = append(bits, fmt.Sprintf("%d loops", active))
	}
	if n := p.monitors.Running(); n > 0 {
		bits = append(bits, fmt.Sprintf("%d running", n))
	}
	return strings.Join(bits, " · ")
}

func (p *Plugin) loopCreateTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name: "LoopCreate",
			Description: "Run a prompt on a schedule. Use for recurring checks and reminders " +
				"instead of a shell sleep, which blocks the tool loop for the whole wait. " +
				"schedule is an interval (30m, 2h) or cron (0 9 * * 1-5). " +
				"Set maxFires for anything that polls for a condition; omit it only for a genuine watchdog.",
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"prompt": llm.Object{
						"type":        "string",
						"description": "What to do each time it fires. It is sent as a user message.",
					},
					"schedule": llm.Object{
						"type":        "string",
						"description": "Interval (30s, 5m, 2h) or a 5-field cron expression.",
					},
					"maxFires": llm.Object{
						"type":        "integer",
						"description": "Stop after this many fires. 0 means unbounded.",
					},
				},
				Required: []string{"prompt", "schedule"},
			},
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in struct{ Schedule string }
			_ = json.Unmarshal(raw, &in)
			return in.Schedule
		},
		Run: func(_ context.Context, raw json.RawMessage) (tools.Result, error) {
			var in struct {
				Prompt   string `json:"prompt"`
				Schedule string `json:"schedule"`
				MaxFires int    `json:"maxFires"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return tools.Result{}, err
			}
			sched, err := ParseSchedule(in.Schedule)
			if err != nil {
				return tools.Result{}, err
			}
			l, err := p.store.Add(in.Prompt, sched, in.MaxFires, time.Now())
			if err != nil {
				return tools.Result{}, err
			}
			body := util.MustJSON(map[string]any{
				"id": l.ID, "schedule": sched.String(),
				"nextAt": l.NextAt.Format(time.RFC3339), "maxFires": l.MaxFires,
			})
			return tools.Result{
				Content: body,
				Detail:  fmt.Sprintf("%s every %s", l.ID, sched.String()),
				Output:  body,
			}, nil
		},
	}
}

func (p *Plugin) loopListTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        "LoopList",
			Description: "List scheduled loops with their state, next fire, and last error.",
			Params:      &llm.FunctionParameters{Type: "object", Properties: llm.Object{}},
		},
		Run: func(_ context.Context, _ json.RawMessage) (tools.Result, error) {
			loops := p.store.List()
			out := make([]map[string]any, 0, len(loops))
			for _, l := range loops {
				entry := map[string]any{
					"id": l.ID, "prompt": l.Prompt, "state": string(l.State),
					"schedule": l.Schedule.String(), "fires": l.Fires,
				}
				if !l.NextAt.IsZero() {
					entry["nextAt"] = l.NextAt.Format(time.RFC3339)
				}
				if n, bounded := l.Remaining(); bounded {
					entry["remaining"] = n
				}
				// A loop that cannot reach the agent looks exactly like one
				// with nothing to report, so the reason is surfaced.
				if l.LastError != "" {
					entry["lastError"] = l.LastError
				}
				out = append(out, entry)
			}
			body := util.MustJSON(map[string]any{"loops": out})
			return tools.Result{Content: body, Detail: fmt.Sprintf("%d loops", len(out)), Output: body}, nil
		},
	}
}

func (p *Plugin) loopUpdateTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name: "LoopUpdate",
			Description: "Pause or resume a loop. Resuming recomputes the next fire from now, " +
				"so a loop paused for a day does not fire for every slot it missed.",
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"id": llm.Object{"type": "string"},
					"state": llm.Object{
						"type": "string",
						"enum": []string{string(StateActive), string(StatePaused)},
					},
				},
				Required: []string{"id", "state"},
			},
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in struct{ ID, State string }
			_ = json.Unmarshal(raw, &in)
			return in.ID + " " + in.State
		},
		Run: func(_ context.Context, raw json.RawMessage) (tools.Result, error) {
			var in struct {
				ID    string `json:"id"`
				State string `json:"state"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return tools.Result{}, err
			}
			state := State(in.State)
			if state != StateActive && state != StatePaused {
				return tools.Result{}, fmt.Errorf("LoopUpdate: state must be %q or %q, got %q",
					StateActive, StatePaused, in.State)
			}
			l, err := p.store.SetState(in.ID, state, time.Now())
			if err != nil {
				return tools.Result{}, err
			}
			body := util.MustJSON(map[string]any{"id": l.ID, "state": string(l.State)})
			return tools.Result{Content: body, Detail: l.ID + " " + string(l.State), Output: body}, nil
		},
	}
}

func (p *Plugin) loopDeleteTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name: "LoopDelete",
			Description: "Delete a loop. A completed iteration or an unchanged result is not a " +
				"reason to delete: recurring loops are meant to persist. Pause instead when in doubt.",
			Params: &llm.FunctionParameters{
				Type:       "object",
				Properties: llm.Object{"id": llm.Object{"type": "string"}},
				Required:   []string{"id"},
			},
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in struct{ ID string }
			_ = json.Unmarshal(raw, &in)
			return in.ID
		},
		Run: func(_ context.Context, raw json.RawMessage) (tools.Result, error) {
			var in struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return tools.Result{}, err
			}
			if err := p.store.Remove(in.ID); err != nil {
				return tools.Result{}, err
			}
			body := util.MustJSON(map[string]any{"deleted": in.ID})
			return tools.Result{Content: body, Detail: in.ID, Output: body}, nil
		},
	}
}

func (p *Plugin) monitorCreateTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name: "MonitorCreate",
			Description: "Start a long command in the background and keep working. Use for test " +
				"suites, builds, and dev servers. The command passes the same permission gate as bash. " +
				"Read output with MonitorLogs; stop it with MonitorStop.",
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"command": llm.Object{"type": "string", "description": "Shell command to run."},
				},
				Required: []string{"command"},
			},
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in struct{ Command string }
			_ = json.Unmarshal(raw, &in)
			return in.Command
		},
		Run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			var in struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return tools.Result{}, err
			}
			mon, err := p.monitors.Start(ctx, in.Command, time.Now())
			if err != nil {
				return tools.Result{}, err
			}
			body := util.MustJSON(map[string]any{"id": mon.ID, "state": string(mon.State), "command": mon.Command})
			return tools.Result{Content: body, Detail: mon.ID + " started", Output: body}, nil
		},
	}
}

func (p *Plugin) monitorListTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        "MonitorList",
			Description: "List background commands with their state, exit code, and runtime.",
			Params:      &llm.FunctionParameters{Type: "object", Properties: llm.Object{}},
		},
		Run: func(_ context.Context, _ json.RawMessage) (tools.Result, error) {
			now := time.Now()
			mons := p.monitors.List()
			out := make([]map[string]any, 0, len(mons))
			for _, mon := range mons {
				entry := map[string]any{
					"id": mon.ID, "command": mon.Command, "state": string(mon.State),
					"runtime": mon.Runtime(now).Round(time.Second).String(),
				}
				if mon.State == MonitorExited {
					entry["exitCode"] = mon.ExitCode
				}
				out = append(out, entry)
			}
			body := util.MustJSON(map[string]any{"monitors": out})
			return tools.Result{
				Content: body,
				Detail:  fmt.Sprintf("%d running", p.monitors.Running()),
				Output:  body,
			}, nil
		},
	}
}

func (p *Plugin) monitorLogsTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name: "MonitorLogs",
			Description: "Read the tail of a background command's output. The tail is kept rather " +
				"than the head, because a failure explains itself at the end.",
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"id":    llm.Object{"type": "string"},
					"lines": llm.Object{"type": "integer", "description": "How many trailing lines. Default 50."},
				},
				Required: []string{"id"},
			},
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in struct{ ID string }
			_ = json.Unmarshal(raw, &in)
			return in.ID
		},
		Run: func(_ context.Context, raw json.RawMessage) (tools.Result, error) {
			var in struct {
				ID    string `json:"id"`
				Lines int    `json:"lines"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return tools.Result{}, err
			}
			if in.Lines <= 0 {
				in.Lines = 50
			}
			mon, err := p.monitors.Get(in.ID)
			if err != nil {
				return tools.Result{}, err
			}
			body := util.MustJSON(map[string]any{
				"id": mon.ID, "state": string(mon.State),
				"exitCode": mon.ExitCode, "output": mon.Tail(in.Lines),
			})
			return tools.Result{
				Content: body,
				Detail:  fmt.Sprintf("%s (%s)", mon.ID, mon.State),
				Output:  body,
			}, nil
		},
	}
}

func (p *Plugin) monitorStopTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        "MonitorStop",
			Description: "Stop a background command.",
			Params: &llm.FunctionParameters{
				Type:       "object",
				Properties: llm.Object{"id": llm.Object{"type": "string"}},
				Required:   []string{"id"},
			},
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in struct{ ID string }
			_ = json.Unmarshal(raw, &in)
			return in.ID
		},
		Run: func(_ context.Context, raw json.RawMessage) (tools.Result, error) {
			var in struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return tools.Result{}, err
			}
			if err := p.monitors.Stop(in.ID); err != nil {
				return tools.Result{}, err
			}
			body := util.MustJSON(map[string]any{"stopped": in.ID})
			return tools.Result{Content: body, Detail: in.ID + " stopped", Output: body}, nil
		},
	}
}
