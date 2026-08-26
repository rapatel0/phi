package ext

import (
	"context"
	"strings"

	"github.com/rapatel0/alpha/internal/debuglog"
	"github.com/rapatel0/alpha/internal/hooks"
)

// This file connects compiled-in Go extensions to the hook manager.
//
// Alpha already had two halves that never met. External process hooks could
// register slash commands, observe the tool loop, and watch session lifecycle
// events, all through hooks.Manager. Go extensions could only add a tool and a
// footer string. The machinery an extension needed was there; it was reachable
// from a shell script and not from Go.
//
// The three methods below close that gap. They collect callbacks at startup and
// hand them to the controller as hooks.Entry values, so extension hooks and
// discovered hooks run through one manager. Ordering, fail-closed behavior, and
// async semantics stay in hooks, and none of it is duplicated here.

// Command is an extension-provided slash command.
//
// Name carries no leading slash. Run returns a hooks.CommandResult, the same
// vocabulary external command hooks use: Submit a message, Toast a line, set
// the footer Status, or push a List page.
type Command struct {
	Name        string
	Description string
	Run         func(ctx context.Context, args []string) (hooks.CommandResult, error)
}

// SessionFunc handles a session lifecycle event.
//
// Returning a non-nil error denies the event when it can be denied:
// KindSessionBeforeSwitch blocks the switch, KindSessionBeforeCompact skips
// compaction. The other kinds report the error and continue, because work that
// already happened cannot be undone. hooks.CanDeny reports which is which.
type SessionFunc func(ctx context.Context, ev hooks.SessionEvent) error

// PreFunc runs before a tool, after the model asks for it and before the
// permission gate. Returning an error blocks the tool with that reason.
type PreFunc func(ctx context.Context, ev hooks.Event) error

// PostFunc runs after a tool returns. The error is reported, not acted on: the
// tool already ran.
type PostFunc func(ctx context.Context, ev hooks.Event) error

// ResultFunc runs after a tool returns and can add a note to the result the
// model reads. It is how an extension tells the model something about work it
// just did, such as a problem in a file it wrote.
//
// The returned string is appended to the tool result. Returning "" adds
// nothing, which is the right answer when there is nothing to report: a note
// after every call trains the model to skip the section.
//
// An error is logged and the note dropped. The tool already ran, so a failing
// observer must not look like a failing tool.
type ResultFunc func(ctx context.Context, ev hooks.Event) (string, error)

// PromptFunc can replace the system prompt for the turn that is starting. It
// receives the user prompt and the current system prompt, and returns the
// prompt to use. Returning "" or the prompt unchanged keeps the current one.
type PromptFunc func(ctx context.Context, prompt, systemPrompt string) (string, error)

// RegisterCommand adds a slash command. A command with an empty name or a nil
// Run is dropped, because both make it unusable from the palette.
//
// Registering the same name twice keeps the first. Extensions load in a fixed
// order, so this makes a collision deterministic rather than dependent on it.
func (h *Host) RegisterCommand(cmd Command) {
	if h == nil || cmd.Name == "" || cmd.Run == nil {
		return
	}
	name := strings.TrimPrefix(cmd.Name, "/")
	if name == "" {
		return
	}
	cmd.Name = name

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, existing := range h.commands {
		if existing.Name == name {
			return
		}
	}
	h.commands = append(h.commands, cmd)
}

// Commands returns a copy of the registered commands, for the palette and /ext.
func (h *Host) Commands() []Command {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Command(nil), h.commands...)
}

// OnSession subscribes to a session lifecycle event. Unknown kinds are dropped
// here rather than at dispatch, so a typo fails at startup instead of silently
// never firing.
func (h *Host) OnSession(kind hooks.Kind, fn SessionFunc) {
	if h == nil || fn == nil {
		return
	}
	if !hooks.IsSessionKind(kind) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onSession = append(h.onSession, sessionSub{kind: kind, fn: fn})
}

// OnBeforeAgentStart subscribes to before_agent_start, which runs after the
// user submits and before the turn starts.
//
// fn receives the user prompt and the system prompt the turn will use, and
// returns the prompt to use instead. Returning "" keeps the current one.
// Handlers run in order, so fn sees any earlier handler's replacement.
//
// An error is logged and the turn continues unchanged: a styling concern must
// never cost a turn.
func (h *Host) OnBeforeAgentStart(fn PromptFunc) {
	if h == nil || fn == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onPrompt = append(h.onPrompt, fn)
}

// OnBeforeProviderRequest subscribes to before_provider_request, which runs
// once per model request after the message list is assembled and before it
// becomes a provider payload.
//
// fn returns the messages to send. Returning the input unchanged observes
// without rewriting. An error is logged and the request proceeds unchanged: a
// budget must never cost a turn.
//
// The hook sees the message list rather than a provider payload, so one
// implementation covers every provider.
func (h *Host) OnBeforeProviderRequest(name string, fn hooks.ProviderFunc) {
	if h == nil || fn == nil {
		return
	}
	if name == "" {
		name = "ext"
	}
	hooks.RegisterProviderHook(name, fn)
}

// OnTool observes the tool loop. match is a tool name, or "" for every tool.
// Either callback may be nil.
//
// A pre callback runs before the permission gate, so it can block a tool, but
// it cannot grant permission. The gate stays the only thing that allows a tool.
func (h *Host) OnTool(match string, pre PreFunc, post PostFunc) {
	if h == nil || (pre == nil && post == nil) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onTool = append(h.onTool, toolSub{match: match, pre: pre, post: post})
}

// OnToolResult registers a handler that can add a note to a tool result the
// model reads. match takes the same comma-separated tool names as OnTool.
//
// Unlike OnTool's post handler, this one runs synchronously: the note has to
// be ready before the result is handed to the model, and a detached handler
// would deliver it a turn late. Keep the work short, and bound anything that
// waits on a process or the network.
func (h *Host) OnToolResult(match string, fn ResultFunc) {
	if h == nil || fn == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onToolResult = append(h.onToolResult, resultSub{match: match, fn: fn})
}

// HookEntries converts everything registered on this host into hook entries.
//
// The controller merges these with entries from directory discovery, so one
// manager dispatches both. Entries are not fail-closed: an extension is
// compiled in, and a panic in one should not make the agent refuse to run
// tools. Post and session entries are async, matching the discovered ones.
func (h *Host) HookEntries() []hooks.Entry {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	cmds := append([]Command(nil), h.commands...)
	sessions := append([]sessionSub(nil), h.onSession...)
	toolSubs := append([]toolSub(nil), h.onTool...)
	results := append([]resultSub(nil), h.onToolResult...)
	prompts := append([]PromptFunc(nil), h.onPrompt...)
	h.mu.Unlock()

	var entries []hooks.Entry
	for _, cmd := range cmds {
		entries = append(entries, hooks.Entry{
			Hook: hooks.FuncHook{
				HookName: cmd.Name,
				Cmd:      commandAdapter(cmd),
			},
			Kind: hooks.KindCommand,
		})
	}
	for _, sub := range sessions {
		entries = append(entries, hooks.Entry{
			Hook: hooks.FuncHook{HookName: "ext:" + string(sub.kind), Sess: sessionAdapter(sub)},
			Kind: sub.kind,
			// An event whose result is used must be waited on, or the
			// answer arrives after the decision it was meant to change.
			Async: !hooks.CanDeny(sub.kind) && sub.kind != hooks.KindBeforeAgentStart,
		})
	}
	for _, fn := range prompts {
		entries = append(entries, hooks.Entry{
			Hook: hooks.FuncHook{HookName: "ext:before_agent_start", Sess: promptAdapter(fn)},
			Kind: hooks.KindBeforeAgentStart,
			// Never async: an detached handler's prompt would arrive after
			// the request was already built.
		})
	}
	for _, sub := range toolSubs {
		if sub.pre != nil {
			entries = append(entries, hooks.Entry{
				Hook: hooks.FuncHook{
					HookName: "ext:pre_tool",
					MatchFn:  matcher(sub.match),
					Pre:      preAdapter(sub.pre),
				},
				Kind: hooks.KindPreTool,
			})
		}
		if sub.post != nil {
			entries = append(entries, hooks.Entry{
				Hook: hooks.FuncHook{
					HookName: "ext:post_tool",
					MatchFn:  matcher(sub.match),
					Post:     postAdapter(sub.post),
				},
				Kind:  hooks.KindPostTool,
				Async: true,
			})
		}
	}
	for _, sub := range results {
		entries = append(entries, hooks.Entry{
			Hook: hooks.FuncHook{
				HookName: "ext:tool_result",
				MatchFn:  matcher(sub.match),
				Post:     resultAdapter(sub.fn),
			},
			Kind: hooks.KindPostTool,
			// Never async: the manager detaches async post entries and
			// discards their result, so the note would never reach the
			// model it was written for.
		})
	}
	return entries
}

type sessionSub struct {
	kind hooks.Kind
	fn   SessionFunc
}

// promptAdapter turns a replacement prompt into a SessionResult. An empty
// return means "leave it alone", which is distinct from replacing the prompt
// with nothing.
func promptAdapter(fn PromptFunc) func(context.Context, hooks.SessionEvent) (hooks.SessionResult, error) {
	return func(ctx context.Context, ev hooks.SessionEvent) (hooks.SessionResult, error) {
		next, err := fn(ctx, ev.Prompt, ev.SystemPrompt)
		if err != nil {
			return hooks.SessionResult{Action: hooks.ActionAllow, Reason: err.Error()}, nil
		}
		if next == "" || next == ev.SystemPrompt {
			return hooks.SessionResult{Action: hooks.ActionAllow}, nil
		}
		return hooks.SessionResult{
			Action:          hooks.ActionAllow,
			SystemPrompt:    next,
			SystemPromptSet: true,
		}, nil
	}
}

type toolSub struct {
	match string
	pre   PreFunc
	post  PostFunc
}

type resultSub struct {
	match string
	fn    ResultFunc
}

// resultAdapter turns a note into a PostResult the executor appends to the
// tool result. An error is swallowed after logging: the tool already ran, so a
// failing observer must not be reported as a failing tool.
func resultAdapter(fn ResultFunc) func(context.Context, hooks.Event) (hooks.PostResult, error) {
	return func(ctx context.Context, ev hooks.Event) (hooks.PostResult, error) {
		note, err := fn(ctx, ev)
		if err != nil {
			debuglog.Logf("ext: tool result handler for %s: %v", ev.Tool, err)
			return hooks.PostResult{}, nil
		}
		return hooks.PostResult{Context: note}, nil
	}
}

func commandAdapter(cmd Command) func(context.Context, hooks.CommandEvent) (hooks.CommandResult, error) {
	return func(ctx context.Context, ev hooks.CommandEvent) (hooks.CommandResult, error) {
		return cmd.Run(ctx, ev.Args)
	}
}

// sessionAdapter maps an error to a decision. Only before_switch can be
// denied; for the other kinds the event already happened, so the error is
// surfaced as a reason and the action stays Allow.
func sessionAdapter(sub sessionSub) func(context.Context, hooks.SessionEvent) (hooks.SessionResult, error) {
	return func(ctx context.Context, ev hooks.SessionEvent) (hooks.SessionResult, error) {
		if err := sub.fn(ctx, ev); err != nil {
			// On a reporting event the work already happened, so the error
			// is recorded rather than acted on.
			if hooks.CanDeny(sub.kind) {
				return hooks.SessionResult{Action: hooks.ActionDeny, Reason: err.Error()}, nil
			}
			return hooks.SessionResult{Action: hooks.ActionAllow, Reason: err.Error()}, nil
		}
		return hooks.SessionResult{Action: hooks.ActionAllow}, nil
	}
}

func preAdapter(fn PreFunc) func(context.Context, hooks.Event) (hooks.PreResult, error) {
	return func(ctx context.Context, ev hooks.Event) (hooks.PreResult, error) {
		if err := fn(ctx, ev); err != nil {
			return hooks.PreResult{Action: hooks.ActionDeny, Reason: err.Error()}, nil
		}
		return hooks.PreResult{Action: hooks.ActionAllow}, nil
	}
}

func postAdapter(fn PostFunc) func(context.Context, hooks.Event) (hooks.PostResult, error) {
	return func(ctx context.Context, ev hooks.Event) (hooks.PostResult, error) {
		if err := fn(ctx, ev); err != nil {
			return hooks.PostResult{Reason: err.Error()}, nil
		}
		return hooks.PostResult{}, nil
	}
}
