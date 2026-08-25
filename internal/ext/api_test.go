package ext_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

func okCmd(string) func(context.Context, []string) (hooks.CommandResult, error) {
	return func(context.Context, []string) (hooks.CommandResult, error) {
		return hooks.CommandResult{}, nil
	}
}

func TestRegisterCommand(t *testing.T) {
	h := ext.NewHost()
	h.RegisterCommand(ext.Command{Name: "greet", Description: "hi", Run: okCmd("greet")})

	cmds := h.Commands()
	require.Len(t, cmds, 1)
	require.Equal(t, "greet", cmds[0].Name)
	require.Equal(t, "hi", cmds[0].Description)
}

// A leading slash is what a user types, not what the registry stores.
func TestRegisterCommandTrimsSlash(t *testing.T) {
	h := ext.NewHost()
	h.RegisterCommand(ext.Command{Name: "/slashed", Run: okCmd("slashed")})

	cmds := h.Commands()
	require.Len(t, cmds, 1)
	require.Equal(t, "slashed", cmds[0].Name)
}

func TestRegisterCommandRejectsUnusable(t *testing.T) {
	h := ext.NewHost()
	h.RegisterCommand(ext.Command{Name: "", Run: okCmd("")})   // no name
	h.RegisterCommand(ext.Command{Name: "/", Run: okCmd("/")}) // only a slash
	h.RegisterCommand(ext.Command{Name: "no-run"})             // nil Run
	require.Empty(t, h.Commands(), "unusable commands must not register")
}

// First registration wins, so a collision does not depend on load order.
func TestRegisterCommandDuplicateKeepsFirst(t *testing.T) {
	h := ext.NewHost()
	h.RegisterCommand(ext.Command{Name: "dup", Description: "first", Run: okCmd("dup")})
	h.RegisterCommand(ext.Command{Name: "dup", Description: "second", Run: okCmd("dup")})

	cmds := h.Commands()
	require.Len(t, cmds, 1)
	require.Equal(t, "first", cmds[0].Description)
}

// The command result must survive the trip through the hook adapter.
func TestCommandEntryRunsAndCarriesArgs(t *testing.T) {
	h := ext.NewHost()
	var gotArgs []string
	h.RegisterCommand(ext.Command{
		Name: "echo",
		Run: func(_ context.Context, args []string) (hooks.CommandResult, error) {
			gotArgs = args
			return hooks.CommandResult{Toast: "ran"}, nil
		},
	})

	mgr := hooks.NewManager(h.HookEntries()...)
	res, err := mgr.RunCommand(t.Context(), "echo", hooks.CommandEvent{Args: []string{"a", "b"}})
	require.NoError(t, err)
	require.Equal(t, "ran", res.Toast)
	require.Equal(t, []string{"a", "b"}, gotArgs)
}

func TestCommandEntryKind(t *testing.T) {
	h := ext.NewHost()
	h.RegisterCommand(ext.Command{Name: "c", Run: okCmd("c")})

	entries := h.HookEntries()
	require.Len(t, entries, 1)
	require.Equal(t, hooks.KindCommand, entries[0].Kind)
	require.Equal(t, "c", entries[0].Hook.Name())
	require.False(t, entries[0].FailClosed, "a compiled-in extension must not block the tool loop")
}

// session_start entries are async, so the callback reports through a channel
// rather than a variable the test reads straight after the call.
func TestOnSessionFires(t *testing.T) {
	h := ext.NewHost()
	got := make(chan hooks.SessionEvent, 1)
	h.OnSession(hooks.KindSessionStart, func(_ context.Context, ev hooks.SessionEvent) error {
		got <- ev
		return nil
	})

	mgr := hooks.NewManager(h.HookEntries()...)
	mgr.SessionStart(t.Context(), hooks.SessionEvent{
		Kind: hooks.KindSessionStart, SessionID: "s1", Reason: "startup",
	})

	select {
	case ev := <-got:
		require.Equal(t, "s1", ev.SessionID)
		require.Equal(t, "startup", ev.Reason)
	case <-time.After(2 * time.Second):
		t.Fatal("session_start hook did not fire")
	}
}

func TestOnSessionRejectsUnknownKind(t *testing.T) {
	h := ext.NewHost()
	h.OnSession(hooks.KindPreTool, func(context.Context, hooks.SessionEvent) error { return nil })
	h.OnSession(hooks.Kind("nonsense"), func(context.Context, hooks.SessionEvent) error { return nil })
	h.OnSession(hooks.KindSessionStart, nil)

	require.Empty(t, h.HookEntries(), "only session kinds with a callback subscribe")
}

// before_switch is the one session event a hook can veto.
func TestOnSessionBeforeSwitchDenies(t *testing.T) {
	h := ext.NewHost()
	h.OnSession(hooks.KindSessionBeforeSwitch, func(context.Context, hooks.SessionEvent) error {
		return errors.New("unsaved work")
	})

	entries := h.HookEntries()
	require.Len(t, entries, 1)
	require.False(t, entries[0].Async, "a veto cannot be fire-and-forget")

	mgr := hooks.NewManager(entries...)
	out := mgr.SessionBeforeSwitch(t.Context(), hooks.SessionEvent{
		Kind: hooks.KindSessionBeforeSwitch,
	})
	require.True(t, out.Denied, "returning an error must deny the switch")
	require.Contains(t, out.Reason, "unsaved work")
}

// A session that already started cannot be un-started, so an error there is
// reported rather than acted on.
func TestOnSessionStartErrorDoesNotDeny(t *testing.T) {
	h := ext.NewHost()
	h.OnSession(hooks.KindSessionStart, func(context.Context, hooks.SessionEvent) error {
		return errors.New("boom")
	})

	entries := h.HookEntries()
	require.Len(t, entries, 1)
	require.True(t, entries[0].Async, "session_start is fire-and-forget")

	out := hooks.NewManager(entries...).SessionStart(t.Context(), hooks.SessionEvent{
		Kind: hooks.KindSessionStart,
	})
	require.False(t, out.Denied)
}

func TestOnToolPreObservesAndBlocks(t *testing.T) {
	h := ext.NewHost()
	var seen string
	h.OnTool("bash", func(_ context.Context, ev hooks.Event) error {
		seen = ev.Tool
		return errors.New("no shell today")
	}, nil)

	mgr := hooks.NewManager(h.HookEntries()...)
	out := mgr.PreTool(t.Context(), hooks.Event{Tool: "bash"})

	require.Equal(t, "bash", seen)
	require.True(t, out.Denied, "a pre error must block the tool")
	require.Contains(t, out.Reason, "no shell today")
}

// match scopes a subscription to one tool.
func TestOnToolMatchScopesTool(t *testing.T) {
	h := ext.NewHost()
	calls := 0
	h.OnTool("bash", func(context.Context, hooks.Event) error {
		calls++
		return nil
	}, nil)

	mgr := hooks.NewManager(h.HookEntries()...)
	mgr.PreTool(t.Context(), hooks.Event{Tool: "read"})
	require.Zero(t, calls, "a non-matching tool must not fire the hook")

	mgr.PreTool(t.Context(), hooks.Event{Tool: "bash"})
	require.Equal(t, 1, calls)
}

func TestOnToolEmptyMatchSeesEveryTool(t *testing.T) {
	h := ext.NewHost()
	var seen []string
	h.OnTool("", func(_ context.Context, ev hooks.Event) error {
		seen = append(seen, ev.Tool)
		return nil
	}, nil)

	mgr := hooks.NewManager(h.HookEntries()...)
	mgr.PreTool(t.Context(), hooks.Event{Tool: "bash"})
	mgr.PreTool(t.Context(), hooks.Event{Tool: "read"})
	require.Equal(t, []string{"bash", "read"}, seen)
}

func TestOnToolPostEntry(t *testing.T) {
	h := ext.NewHost()
	h.OnTool("", nil, func(context.Context, hooks.Event) error { return nil })

	entries := h.HookEntries()
	require.Len(t, entries, 1)
	require.Equal(t, hooks.KindPostTool, entries[0].Kind)
	require.True(t, entries[0].Async, "post_tool is fire-and-forget")
}

func TestOnToolBothCallbacksMakeTwoEntries(t *testing.T) {
	h := ext.NewHost()
	h.OnTool("bash",
		func(context.Context, hooks.Event) error { return nil },
		func(context.Context, hooks.Event) error { return nil },
	)

	entries := h.HookEntries()
	require.Len(t, entries, 2)
	require.Equal(t, hooks.KindPreTool, entries[0].Kind)
	require.Equal(t, hooks.KindPostTool, entries[1].Kind)
}

func TestOnToolRequiresACallback(t *testing.T) {
	h := ext.NewHost()
	h.OnTool("bash", nil, nil)
	require.Empty(t, h.HookEntries())
}

// A nil host is the zero value a test or a partially built process can hold.
func TestNilHostIsSafe(t *testing.T) {
	var h *ext.Host
	require.NotPanics(t, func() {
		h.RegisterCommand(ext.Command{Name: "x", Run: okCmd("x")})
		h.OnSession(hooks.KindSessionStart, func(context.Context, hooks.SessionEvent) error { return nil })
		h.OnTool("", func(context.Context, hooks.Event) error { return nil }, nil)
	})
	require.Nil(t, h.Commands())
	require.Nil(t, h.HookEntries())
}

// Extension hooks and discovered hooks must coexist in one manager.
func TestEntriesMergeWithOtherHooks(t *testing.T) {
	h := ext.NewHost()
	h.RegisterCommand(ext.Command{Name: "fromext", Run: okCmd("fromext")})

	other := hooks.Entry{
		Hook: hooks.FuncHook{
			HookName: "discovered",
			Cmd: func(context.Context, hooks.CommandEvent) (hooks.CommandResult, error) {
				return hooks.CommandResult{Toast: "other"}, nil
			},
		},
		Kind: hooks.KindCommand,
	}

	mgr := hooks.NewManager(append([]hooks.Entry{other}, h.HookEntries()...)...)
	names := make([]string, 0, 2)
	for _, e := range mgr.CommandEntries() {
		names = append(names, e.Hook.Name())
	}
	require.ElementsMatch(t, []string{"discovered", "fromext"}, names)
}
