package goal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/tools"
)

// toolNamed finds a registered tool, so the test exercises what the model
// actually sees rather than an internal helper.
func toolNamed(t *testing.T, h *ext.Host, name string) tools.Tool {
	t.Helper()
	for _, tl := range h.Tools() {
		if tl.Definition.Name == name {
			return tl
		}
	}
	t.Fatalf("tool %q was not registered", name)
	return tools.Tool{}
}

func newPlugin(t *testing.T) (*Plugin, *ext.Host) {
	t.Helper()
	p := &Plugin{}
	h := ext.NewHost()
	require.NoError(t, p.Register(h))
	return p, h
}

func TestCommandStartsAndReportsAGoal(t *testing.T) {
	p, _ := newPlugin(t)

	res, err := p.run(t.Context(), []string{"ship", "the", "port"})
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "ship the port")
	assert.True(t, res.StatusSet)

	res, err = p.run(t.Context(), []string{"status"})
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "ship the port")
	assert.Contains(t, res.Toast, "active")
}

// A bare /goal reports rather than failing, so it is safe to type.
func TestCommandWithoutArgsReports(t *testing.T) {
	p, _ := newPlugin(t)
	res, err := p.run(t.Context(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "No goal is active")
}

func TestCommandParsesTurnLimit(t *testing.T) {
	p, _ := newPlugin(t)
	_, err := p.run(t.Context(), []string{"--turns", "3", "do", "the", "thing"})
	require.NoError(t, err)

	g := p.state.Current()
	assert.Equal(t, 3, g.MaxTurns)
	assert.Equal(t, "do the thing", g.Objective, "the flag must not leak into the objective")
}

func TestCommandPauseResumeClear(t *testing.T) {
	p, _ := newPlugin(t)
	_, err := p.run(t.Context(), []string{"objective"})
	require.NoError(t, err)

	_, err = p.run(t.Context(), []string{"pause"})
	require.NoError(t, err)
	assert.Equal(t, StatusPaused, p.state.Current().Status)

	_, err = p.run(t.Context(), []string{"resume"})
	require.NoError(t, err)
	assert.Equal(t, StatusActive, p.state.Current().Status)

	_, err = p.run(t.Context(), []string{"clear"})
	require.NoError(t, err)
	assert.Nil(t, p.state.Current())
}

// The three closing tools must be visible to the model.
func TestClosingToolsAreRegistered(t *testing.T) {
	_, h := newPlugin(t)
	for _, name := range []string{"goal_complete", "goal_blocked", "goal_wait"} {
		tl := toolNamed(t, h, name)
		assert.Contains(t, tl.Definition.Params.Required, "goal_id",
			"%s must require the id that guards against closing a newer goal", name)
	}
}

func runTool(t *testing.T, tl tools.Tool, args map[string]string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	res, err := tl.Run(t.Context(), raw)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	return out
}

func TestGoalCompleteTool(t *testing.T) {
	p, h := newPlugin(t)
	_, err := p.run(t.Context(), []string{"objective"})
	require.NoError(t, err)
	id := p.state.Current().ID

	out := runTool(t, toolNamed(t, h, "goal_complete"), map[string]string{
		"goal_id": id, "summary": "did it, tests pass",
	})
	assert.Equal(t, true, out["ok"])
	assert.Equal(t, StatusComplete, p.state.Current().Status)
}

// A stale id must be reported to the model rather than failing the turn, so it
// can correct itself.
func TestGoalCompleteToolRejectsStaleID(t *testing.T) {
	p, h := newPlugin(t)
	_, err := p.run(t.Context(), []string{"first"})
	require.NoError(t, err)
	stale := p.state.Current().ID
	_, err = p.run(t.Context(), []string{"second"})
	require.NoError(t, err)

	out := runTool(t, toolNamed(t, h, "goal_complete"), map[string]string{
		"goal_id": stale, "summary": "done",
	})
	assert.Equal(t, false, out["ok"])
	assert.Contains(t, out["error"], "no longer active")
	assert.Equal(t, StatusActive, p.state.Current().Status, "the current goal must survive")
}

// Missing evidence is a rejection, not a crash.
func TestGoalCompleteToolRequiresSummary(t *testing.T) {
	p, h := newPlugin(t)
	_, err := p.run(t.Context(), []string{"objective"})
	require.NoError(t, err)

	out := runTool(t, toolNamed(t, h, "goal_complete"), map[string]string{
		"goal_id": p.state.Current().ID, "summary": "",
	})
	assert.Equal(t, false, out["ok"])
	assert.Equal(t, StatusActive, p.state.Current().Status)
}

func TestGoalBlockedAndWaitTools(t *testing.T) {
	p, h := newPlugin(t)

	_, err := p.run(t.Context(), []string{"objective"})
	require.NoError(t, err)
	out := runTool(t, toolNamed(t, h, "goal_blocked"), map[string]string{
		"goal_id": p.state.Current().ID, "reason": "needs a token",
	})
	assert.Equal(t, true, out["ok"])
	assert.Equal(t, StatusBlocked, p.state.Current().Status)

	_, err = p.run(t.Context(), []string{"another"})
	require.NoError(t, err)
	out = runTool(t, toolNamed(t, h, "goal_wait"), map[string]string{
		"goal_id": p.state.Current().ID, "reason": "waiting for CI",
	})
	assert.Equal(t, true, out["ok"])
	assert.Equal(t, StatusWaiting, p.state.Current().Status)
}

// The footer must stay quiet with no goal and report progress with one.
func TestFooterTracksTheGoal(t *testing.T) {
	p, h := newPlugin(t)
	assert.Empty(t, h.FooterBits())

	_, err := p.run(t.Context(), []string{"--turns", "5", "objective"})
	require.NoError(t, err)
	require.Len(t, h.FooterBits(), 1)
	assert.Equal(t, "goal:0/5", h.FooterBits()[0])
}
