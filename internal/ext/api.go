package ext

import (
	"context"
	"strings"

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
// Returning a non-nil error on KindSessionBeforeSwitch denies the switch. The
// other kinds report the error and continue, because a session that has already
// started cannot be un-started.
type SessionFunc func(ctx context.Context, ev hooks.SessionEvent) error

// PreFunc runs before a tool, after the model asks for it and before the
// permission gate. Returning an error blocks the tool with that reason.
type PreFunc func(ctx context.Context, ev hooks.Event) error

// PostFunc runs after a tool returns. The error is reported, not acted on: the
// tool already ran.
type PostFunc func(ctx context.Context, ev hooks.Event) error

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
	switch kind {
	case hooks.KindSessionStart, hooks.KindSessionShutdown,
		hooks.KindSessionBeforeSwitch, hooks.KindPostTurn:
	default:
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onSession = append(h.onSession, sessionSub{kind: kind, fn: fn})
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
			Hook:  hooks.FuncHook{HookName: "ext:" + string(sub.kind), Sess: sessionAdapter(sub)},
			Kind:  sub.kind,
			Async: sub.kind != hooks.KindSessionBeforeSwitch,
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
	return entries
}

type sessionSub struct {
	kind hooks.Kind
	fn   SessionFunc
}

type toolSub struct {
	match string
	pre   PreFunc
	post  PostFunc
}

// matcher turns a tool name into a Match function. An empty name matches every
// tool, which is what hooks.Hook already means by a nil MatchFn.
func matcher(match string) func(string) bool {
	if match == "" {
		return nil
	}
	return func(tool string) bool { return tool == match }
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
			if sub.kind == hooks.KindSessionBeforeSwitch {
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
