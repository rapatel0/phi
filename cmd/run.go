package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/pulseaiclub/phi/internal/agent"
	"github.com/pulseaiclub/phi/internal/cli"
	"github.com/pulseaiclub/phi/internal/hooks"
	"github.com/pulseaiclub/phi/internal/mcp"
	"github.com/pulseaiclub/phi/internal/session"
)

// runOptions holds parsed `phi run` flags.
type runOptions struct {
	prompt       string
	jsonl        bool
	yolo         bool
	maxRounds    int
	timeout      time.Duration
	session      string
	continueLast bool
	sessionDir   string
}

// newRunCommand binds `phi run` flags into o and returns the command.
func newRunCommand(o *runOptions) *cli.Command {
	c := &cli.Command{
		Name:    "run",
		Desc:    "Run one agent loop headlessly and exit.",
		ArgsUse: "-p STRING",
		Long: "Human logs go to stderr; with --jsonl, machine-readable events go to stdout (one JSON object per line).\n\n" +
			"exit codes:\n  0 success   1 runtime/LLM error   2 max rounds   3 config/usage",
	}
	cli.String(c, "prompt", "p", "prompt to run (required)", &o.prompt)
	cli.Bool(c, "jsonl", "", "emit JSONL events to stdout", &o.jsonl)
	cli.Bool(c, "yolo", "", "skip all permission checks for this run (benchmarks / CI only)", &o.yolo)
	cli.Var(c, "max-rounds", "", "N", "cap tool rounds (default 64)", &o.maxRounds, positiveInt)
	cli.Var(
		c,
		"timeout",
		"",
		"DURATION",
		"stop after a wall-clock duration (default unlimited)",
		&o.timeout,
		positiveDuration,
	)
	cli.String(c, "session", "", "resume a persisted session by id or unique prefix", &o.session)
	cli.Bool(c, "continue-last", "", "resume the newest persisted session for this directory", &o.continueLast)
	cli.String(c, "session-dir", "", "override the session storage directory", &o.sessionDir)
	c.Run = func(args []string) error {
		if err := validateRunArgs(c, o, args); err != nil {
			return err
		}
		if code := runMain(o); code != ExitOK {
			return &exitError{code: code, err: errRunFailed, silent: true}
		}
		return nil
	}
	return c
}

var errRunFailed = errors.New("run failed")

// validateRunArgs rejects positionals and contradictory flag combinations.
func validateRunArgs(c *cli.Command, o *runOptions, args []string) error {
	if len(args) > 0 {
		return c.Usagef("unexpected argument %q", args[0])
	}
	if strings.TrimSpace(o.prompt) == "" {
		return c.Usagef("prompt is required (-p \"...\")")
	}
	if o.continueLast && o.session != "" {
		return c.Usagef("--continue-last and --session are mutually exclusive")
	}
	return nil
}

// parseRunArgs is a test hook that parses flags without running the command.
func parseRunArgs(args []string) (runOptions, error) {
	o := &runOptions{}
	if _, err := newRunCommand(o).Parse(args); err != nil {
		return runOptions{}, err
	}
	return *o, nil
}

func positiveInt(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, errors.New("must be a positive integer")
	}
	return n, nil
}

func positiveDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, errors.New("must be a positive duration")
	}
	return d, nil
}

// runMain executes the run: everything after flag parsing. It prints its own
// diagnostics (prefixed "phi run:") and returns the process exit code.
func runMain(o *runOptions) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	bs, err := loadRunBootstrap(ctx, o.sessionDir, o.yolo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "phi run:", err)
		return ExitUsage
	}
	if o.yolo {
		fmt.Fprintln(os.Stderr, "warning: --yolo skips all permission checks for this run")
	}

	resumeID, resumePath := "", ""
	if o.continueLast {
		list, err := session.ListSessions(bs.SessionDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "phi run:", err)
			return ExitError
		}
		if len(list) == 0 {
			fmt.Fprintln(os.Stderr, "phi run: --continue-last found no sessions in", bs.SessionDir)
			return ExitError
		}
		resumePath = list[0].File
	} else if o.session != "" {
		resumeID = o.session
	}

	engineOpts := agent.EngineOpts{
		Model: bs.Config.Model(),
		SessionOpts: agent.SessionOpts{
			Cwd:        bs.Cwd,
			SessionDir: bs.SessionDir,
			Persist:    true,
			ResumeID:   resumeID,
			ResumePath: resumePath,
		},
		Gate: bs.Gate,
		// Ask is nil: in headless mode any Ask decision is denied, so no
		// approval UI is ever reachable (Ask≡Deny even if the config mode
		// does not fold Ask).
		Ask:   nil,
		Hooks: loadRunHooks(bs),
	}
	if pool, err := mcp.LoadPool(bs.Proj.MCPConfigFile()); err != nil {
		fmt.Fprintln(os.Stderr, "warning: mcp:", err)
	} else if pool != nil {
		engineOpts.MCP = pool
		defer func() { _ = pool.Close() }()
	}
	if bs.Config.Agents.Enabled {
		hooksMgr := engineOpts.Hooks
		jobs, jobErr := agent.NewJobManager(bs.Proj.JobsDir(), bs.Config.Model(), nil, func() *hooks.Manager {
			return hooksMgr
		})
		if jobErr != nil {
			fmt.Fprintln(os.Stderr, "phi run:", jobErr)
			return ExitUsage
		}
		defer func() { _ = jobs.Close(context.Background()) }()
		engineOpts.Jobs = jobs
	}

	engine, err := agent.NewEngine(engineOpts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "phi run:", err)
		return ExitUsage
	}
	if o.maxRounds > 0 {
		if err := engine.SetMaxRounds(o.maxRounds); err != nil {
			fmt.Fprintln(os.Stderr, "phi run:", err)
			return ExitUsage
		}
	}

	fmt.Fprintf(os.Stderr, "session: %s\n", engine.SessionID())
	if f := engine.SessionFile(); f != "" {
		fmt.Fprintf(os.Stderr, "file: %s\n", f)
	}

	runCtx := ctx
	if o.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	return runLoop(runCtx, engine, *o)
}

// loadRunHooks discovers user + project hooks for headless `phi run`.
// Failures are non-fatal (fail-open). Warnings go to debuglog and a one-line stderr hint.
func loadRunHooks(bs *runBootstrap) *hooks.Manager {
	if bs == nil || bs.Proj == nil {
		return nil
	}
	mgr, warns, err := hooks.Load(bs.Proj.Global().HooksDir(), bs.Proj.HooksDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: hooks:", err)
		return nil
	}
	hooks.LogWarnings(warns)
	if summary := hooks.FormatWarningsSummary(warns); summary != "" {
		fmt.Fprintln(os.Stderr, summary)
	}
	return mgr
}

// runLoop consumes the same engine.Loop the TUI uses — no second loop is
// implemented here — and maps events to stdout/stderr + exit codes.
func runLoop(ctx context.Context, engine *agent.Engine, opts runOptions) int {
	enc := &jsonlEncoder{out: os.Stdout, enabled: opts.jsonl}

	exit := ExitOK
	finalText := ""

	for ev, err := range engine.Loop(ctx, opts.prompt, agent.LoopOpts{}) {
		if err != nil {
			exit = classifyRunError(err)
			enc.errorEvent(err.Error())
			fmt.Fprintln(os.Stderr, "error:", err)
			break
		}
		if ev == nil {
			continue
		}
		enc.event(ev)
		if !opts.jsonl {
			switch e := ev.(type) {
			case session.AssistantMessageUpdate:
				if e.Message.State == session.StateComplete {
					finalText = e.Message.FlatText()
				}
			case session.ToolData:
				r := e.Run
				fmt.Fprintf(os.Stderr, "tool: %s [%s] %s\n", r.Name, r.Status, truncate(r.Detail, 100))
				if r.Error != "" {
					fmt.Fprintln(os.Stderr, "  ", truncate(r.Error, 200))
				}
			}
		}
	}

	if ctxErr := ctx.Err(); ctxErr != nil && exit == ExitOK {
		exit = ExitError // context cancellation is not success
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			enc.errorEvent(ctxErr.Error())
			fmt.Fprintln(os.Stderr, "error:", ctxErr)
		}
	}

	if !opts.jsonl && exit == ExitOK && strings.TrimSpace(finalText) != "" {
		fmt.Fprintln(os.Stdout, finalText)
	}

	enc.doneEvent(engine.SessionID(), engine.SessionFile(), exit)
	return exit
}

// --- JSONL event schema ---------------------------------------------------

// jsonlEncoder writes the pinned event schema to a writer. Fields are
// explicit so the wire format never depends on Go struct tags of internal
// session types and never carries API keys or other config secrets.
type jsonlEncoder struct {
	out     io.Writer
	enabled bool
}

func (enc *jsonlEncoder) emit(v any) {
	if enc == nil || !enc.enabled {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(enc.out, `{"type":"error","message":%q}`+"\n", "encode event: "+err.Error())
		return
	}
	_, _ = enc.out.Write(data)
	_, _ = enc.out.Write([]byte("\n"))
}

type jsonlAssistant struct {
	Type     string      `json:"type"` // "assistant"
	ID       string      `json:"id"`
	State    string      `json:"state"` // streaming | complete | cancelled | error
	Reason   string      `json:"reason,omitempty"`
	Text     string      `json:"text"`
	Thinking string      `json:"thinking,omitempty"`
	Usage    *jsonlUsage `json:"usage,omitempty"`
}

type jsonlUsage struct {
	Prompt     int `json:"prompt"`
	Completion int `json:"completion"`
	Total      int `json:"total"`
}

type jsonlTool struct {
	Type      string `json:"type"` // "tool"
	ToolUseID string `json:"toolUseId"`
	ToolName  string `json:"toolName,omitempty"`
	Status    string `json:"status"` // in-progress | done | error | cancelled | rejected
	Detail    string `json:"detail,omitempty"`
	Output    string `json:"output,omitempty"`
}

type jsonlCompaction struct {
	Type   string `json:"type"`  // "compaction"
	Phase  string `json:"phase"` // started | complete
	Failed bool   `json:"failed,omitempty"`
}

type jsonlError struct {
	Type    string `json:"type"` // "error"
	Message string `json:"message"`
}

type jsonlDone struct {
	Type      string `json:"type"` // "done"
	SessionID string `json:"sessionId,omitempty"`
	File      string `json:"file,omitempty"`
	ExitCode  int    `json:"exitCode"`
}

func (enc *jsonlEncoder) event(ev session.Event) {
	switch e := ev.(type) {
	case session.AssistantMessageUpdate:
		m := e.Message
		var usage *jsonlUsage
		if m.Usage.Reported() {
			usage = &jsonlUsage{
				Prompt:     m.Usage.PromptTokens,
				Completion: m.Usage.CompletionTokens,
				Total:      m.Usage.TotalTokens,
			}
		}
		enc.emit(jsonlAssistant{
			Type:     "assistant",
			ID:       m.ID,
			State:    m.State.String(),
			Reason:   reasonString(m.StopReason),
			Text:     assistantText(m),
			Thinking: thinkingText(m.Content),
			Usage:    usage,
		})
	case session.ToolData:
		r := e.Run
		enc.emit(jsonlTool{
			Type:      "tool",
			ToolUseID: r.ToolUseID,
			ToolName:  r.Name,
			Status:    r.Status.String(),
			Detail:    r.Detail,
			Output:    r.Output,
		})
	case session.CompactionStarted:
		enc.emit(jsonlCompaction{Type: "compaction", Phase: "started"})
	case session.CompactionComplete:
		enc.emit(jsonlCompaction{Type: "compaction", Phase: "complete", Failed: e.Failed})
	}
}

func (enc *jsonlEncoder) errorEvent(message string) {
	enc.emit(jsonlError{Type: "error", Message: message})
}

func (enc *jsonlEncoder) doneEvent(sessionID, file string, exit int) {
	enc.emit(jsonlDone{Type: "done", SessionID: sessionID, File: file, ExitCode: exit})
}

func reasonString(r session.StopReason) string {
	switch r {
	case session.StopEndTurn:
		return "end_turn"
	case session.StopToolUse:
		return "tool_use"
	case session.StopMaxTokens:
		return "max_tokens"
	default:
		return ""
	}
}

func thinkingText(blocks []session.ContentBlock) string {
	var out strings.Builder
	for _, b := range blocks {
		if b.Type == session.BlockThinking {
			out.WriteString(b.Text)
		}
	}
	return out.String()
}

// assistantText joins the text blocks of an assistant event. It does not use
// Message.FlatText because raw events may carry the zero Role (RoleUser),
// which would make FlatText fall back to m.Text.
func assistantText(m session.Message) string {
	var out strings.Builder
	for _, b := range m.Content {
		if b.Type == session.BlockText {
			out.WriteString(b.Text)
		}
	}
	if out.Len() == 0 {
		return m.Text
	}
	return out.String()
}

// classifyRunError maps a loop error to the exit-code contract:
// max rounds → 2, anything else → 1.
func classifyRunError(err error) int {
	if errors.Is(err, agent.ErrMaxRounds) {
		return ExitMaxRounds
	}
	return ExitError
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
