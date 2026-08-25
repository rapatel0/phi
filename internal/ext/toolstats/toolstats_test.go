package toolstats_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/ext/toolstats"
	"github.com/rapatel0/alpha/internal/hooks"
)

// newManager registers the plugin on a fresh host and returns both the plugin
// and a manager carrying its hook entries, which is how the controller wires it.
func newManager(t *testing.T) (*toolstats.Plugin, *hooks.Manager) {
	t.Helper()
	p := toolstats.New()
	h := ext.NewHost()
	require.NoError(t, h.Add(p))
	return p, hooks.NewManager(h.HookEntries()...)
}

// post_tool entries are async, so the counter lands shortly after the call
// rather than during it. Wait for the expected summary instead of racing it.
func requireSummary(t *testing.T, p *toolstats.Plugin, want string) {
	t.Helper()
	require.Eventually(t, func() bool { return p.Summary() == want },
		2*time.Second, 5*time.Millisecond,
		"want %q, last saw %q", want, p.Summary())
}

func TestEmptySummary(t *testing.T) {
	p := toolstats.New()
	require.Equal(t, "no tool calls yet", p.Summary())
}

// The whole point: tool calls must reach the counter through the hook manager.
func TestCountsToolCallsThroughManager(t *testing.T) {
	p, mgr := newManager(t)

	mgr.PostTool(t.Context(), hooks.Event{Tool: "bash"})
	mgr.PostTool(t.Context(), hooks.Event{Tool: "bash"})
	mgr.PostTool(t.Context(), hooks.Event{Tool: "read"})

	requireSummary(t, p, "3 tool calls: bash 2, read 1")
}

func TestCountsErrors(t *testing.T) {
	p, mgr := newManager(t)

	mgr.PostTool(t.Context(), hooks.Event{Tool: "bash"})
	mgr.PostTool(t.Context(), hooks.Event{Tool: "bash", Err: "exit 1"})

	requireSummary(t, p, "2 tool calls: bash 2 (1 failed)")
}

// Ties break by name, so repeated calls render the same string.
func TestSummaryOrderIsStable(t *testing.T) {
	p, mgr := newManager(t)
	for _, tool := range []string{"zed", "alpha", "mid", "mid"} {
		mgr.PostTool(t.Context(), hooks.Event{Tool: tool})
	}

	want := "4 tool calls: mid 2, alpha 1, zed 1"
	requireSummary(t, p, want)
	require.Equal(t, want, p.Summary(), "summary must not reshuffle")
}

// Counts describe one session, so a new session starts from zero.
func TestSessionStartResets(t *testing.T) {
	p, mgr := newManager(t)
	mgr.PostTool(t.Context(), hooks.Event{Tool: "bash"})
	requireSummary(t, p, "1 tool calls: bash 1")

	mgr.SessionStart(t.Context(), hooks.SessionEvent{Kind: hooks.KindSessionStart})

	requireSummary(t, p, "no tool calls yet")
}

// The command must be reachable by name and report the live count.
func TestCommandReportsCounts(t *testing.T) {
	p, mgr := newManager(t)
	mgr.PostTool(t.Context(), hooks.Event{Tool: "grep"})
	requireSummary(t, p, "1 tool calls: grep 1")

	res, err := mgr.RunCommand(t.Context(), "toolstats", hooks.CommandEvent{})
	require.NoError(t, err)
	require.Equal(t, "1 tool calls: grep 1", res.Toast)
}

func TestRegistersExpectedEntries(t *testing.T) {
	h := ext.NewHost()
	require.NoError(t, h.Add(toolstats.New()))

	kinds := map[hooks.Kind]int{}
	for _, e := range h.HookEntries() {
		kinds[e.Kind]++
	}
	require.Equal(t, 1, kinds[hooks.KindPostTool], "counts tool calls")
	require.Equal(t, 1, kinds[hooks.KindSessionStart], "resets per session")
	require.Equal(t, 1, kinds[hooks.KindCommand], "exposes /toolstats")

	cmds := h.Commands()
	require.Len(t, cmds, 1)
	require.Equal(t, "toolstats", cmds[0].Name)
	require.NotEmpty(t, cmds[0].Description, "the palette shows this text")
}

// A tool name is required; an empty one would create a nameless bucket.
func TestIgnoresEmptyToolName(t *testing.T) {
	p, mgr := newManager(t)
	mgr.PostTool(t.Context(), hooks.Event{Tool: ""})
	require.Never(t, func() bool { return p.Summary() != "no tool calls yet" },
		200*time.Millisecond, 10*time.Millisecond,
		"an empty tool name must not create a bucket")
}
