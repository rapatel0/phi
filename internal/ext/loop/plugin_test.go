package loop

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/tools"
)

// newPlugin registers a plugin on an isolated host, so one test cannot see
// another's loops.
func newPlugin(t *testing.T) (*Plugin, *ext.Host) {
	t.Helper()
	h := ext.NewHost()
	p := &Plugin{}
	require.NoError(t, h.Add(p))
	return p, h
}

// toolByName finds a registered tool, failing with the available names so a
// rename does not produce a nil-pointer panic.
func toolByName(t *testing.T, h *ext.Host, name string) tools.Tool {
	t.Helper()
	var have []string
	for _, tool := range h.Tools() {
		if tool.Definition.Name == name {
			return tool
		}
		have = append(have, tool.Definition.Name)
	}
	t.Fatalf("no tool %q; registered: %v", name, have)
	return tools.Tool{}
}

// call runs a tool with JSON arguments and decodes its result.
func call(t *testing.T, tool tools.Tool, args any) (map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)

	res, err := tool.Run(t.Context(), raw)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	return out, nil
}

func TestPluginRegistersItsTools(t *testing.T) {
	_, h := newPlugin(t)

	for _, name := range []string{
		"LoopCreate", "LoopList", "LoopUpdate", "LoopDelete",
		"MonitorCreate", "MonitorList", "MonitorLogs", "MonitorStop",
	} {
		toolByName(t, h, name)
	}
}

func TestLoopCreateAndList(t *testing.T) {
	p, h := newPlugin(t)

	got, err := call(t, toolByName(t, h, "LoopCreate"), map[string]any{
		"prompt": "run the checks", "schedule": "30m", "maxFires": 3,
	})
	require.NoError(t, err)
	assert.Equal(t, "loop-1", got["id"])
	assert.NotEmpty(t, got["nextAt"], "the caller needs to know when it fires")

	listed, err := call(t, toolByName(t, h, "LoopList"), map[string]any{})
	require.NoError(t, err)
	loops := listed["loops"].([]any)
	require.Len(t, loops, 1)

	entry := loops[0].(map[string]any)
	assert.Equal(t, "active", entry["state"])
	assert.InDelta(t, 3, entry["remaining"], 0.001)
	assert.Len(t, p.store.List(), 1)
}

// A bad schedule is a mistake worth reporting at creation, not at 3am.
func TestLoopCreateRejectsABadSchedule(t *testing.T) {
	_, h := newPlugin(t)

	_, err := call(t, toolByName(t, h, "LoopCreate"), map[string]any{
		"prompt": "x", "schedule": "whenever",
	})
	assert.Error(t, err)
}

func TestLoopUpdatePausesAndResumes(t *testing.T) {
	_, h := newPlugin(t)
	created, err := call(t, toolByName(t, h, "LoopCreate"), map[string]any{
		"prompt": "x", "schedule": "30m",
	})
	require.NoError(t, err)
	id := created["id"].(string)

	got, err := call(t, toolByName(t, h, "LoopUpdate"), map[string]any{"id": id, "state": "paused"})
	require.NoError(t, err)
	assert.Equal(t, "paused", got["state"])

	got, err = call(t, toolByName(t, h, "LoopUpdate"), map[string]any{"id": id, "state": "active"})
	require.NoError(t, err)
	assert.Equal(t, "active", got["state"])
}

// "done" is terminal and reached by the loop itself, so accepting it here
// would let the model retire a loop while pretending to pause it.
func TestLoopUpdateRejectsAnUnknownState(t *testing.T) {
	_, h := newPlugin(t)
	created, err := call(t, toolByName(t, h, "LoopCreate"), map[string]any{
		"prompt": "x", "schedule": "30m",
	})
	require.NoError(t, err)

	_, err = call(t, toolByName(t, h, "LoopUpdate"), map[string]any{
		"id": created["id"], "state": "done",
	})
	assert.Error(t, err)
}

func TestLoopDelete(t *testing.T) {
	p, h := newPlugin(t)
	created, err := call(t, toolByName(t, h, "LoopCreate"), map[string]any{
		"prompt": "x", "schedule": "30m",
	})
	require.NoError(t, err)

	_, err = call(t, toolByName(t, h, "LoopDelete"), map[string]any{"id": created["id"]})
	require.NoError(t, err)
	assert.Empty(t, p.store.List())

	_, err = call(t, toolByName(t, h, "LoopDelete"), map[string]any{"id": created["id"]})
	assert.Error(t, err, "deleting it twice must report the second")
}

// The gate is installed by the shell. Until then a background command must
// refuse, or MonitorCreate becomes a way around the permission the model would
// need for bash.
func TestMonitorCreateRefusesBeforeTheGateArrives(t *testing.T) {
	_, h := newPlugin(t)

	_, err := call(t, toolByName(t, h, "MonitorCreate"), map[string]any{"command": "ls"})
	assert.ErrorIs(t, err, ErrNoGate)
}

func TestMonitorLifecycleThroughTheTools(t *testing.T) {
	p, h := newPlugin(t)
	p.SetGate(allowAll())
	p.monitors.run = scriptedRunner("compiling\ndone\n", 0, nil)

	created, err := call(t, toolByName(t, h, "MonitorCreate"), map[string]any{"command": "go build ./..."})
	require.NoError(t, err)
	id := created["id"].(string)

	waitFor(t, func() bool {
		got, _ := p.monitors.Get(id)
		return got.State == MonitorExited
	})

	logs, err := call(t, toolByName(t, h, "MonitorLogs"), map[string]any{"id": id})
	require.NoError(t, err)
	assert.Contains(t, logs["output"], "done")
	assert.Equal(t, "exited", logs["state"])

	listed, err := call(t, toolByName(t, h, "MonitorList"), map[string]any{})
	require.NoError(t, err)
	assert.Len(t, listed["monitors"], 1)
}

func TestMonitorStopThroughTheTool(t *testing.T) {
	p, h := newPlugin(t)
	p.SetGate(allowAll())
	block := make(chan struct{})
	defer close(block)
	p.monitors.run = scriptedRunner("", 0, block)

	created, err := call(t, toolByName(t, h, "MonitorCreate"), map[string]any{"command": "npm run dev"})
	require.NoError(t, err)

	_, err = call(t, toolByName(t, h, "MonitorStop"), map[string]any{"id": created["id"]})
	require.NoError(t, err)
	assert.Zero(t, p.monitors.Running())
}

// Background work nobody can see is background work nobody stops.
func TestFooterShowsBackgroundWork(t *testing.T) {
	p, h := newPlugin(t)
	assert.Empty(t, h.FooterBits(), "an idle extension adds nothing")

	_, err := call(t, toolByName(t, h, "LoopCreate"), map[string]any{"prompt": "x", "schedule": "30m"})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(h.FooterBits(), " "), "1 loops")

	p.SetGate(allowAll())
	block := make(chan struct{})
	defer close(block)
	p.monitors.run = scriptedRunner("", 0, block)
	_, err = call(t, toolByName(t, h, "MonitorCreate"), map[string]any{"command": "serve"})
	require.NoError(t, err)

	waitFor(t, func() bool { return p.monitors.Running() == 1 })
	assert.Contains(t, strings.Join(h.FooterBits(), " "), "1 running")
}

// A paused loop is not doing anything, so counting it would overstate the
// background work.
func TestFooterCountsOnlyActiveLoops(t *testing.T) {
	_, h := newPlugin(t)
	created, err := call(t, toolByName(t, h, "LoopCreate"), map[string]any{"prompt": "x", "schedule": "30m"})
	require.NoError(t, err)

	_, err = call(t, toolByName(t, h, "LoopUpdate"), map[string]any{"id": created["id"], "state": "paused"})
	require.NoError(t, err)

	assert.NotContains(t, strings.Join(h.FooterBits(), " "), "loops")
}

// The plugin owns background work, so the shell has to be able to find it and
// shut it down.
func TestPluginIsDiscoverableAsBackgroundWork(t *testing.T) {
	p, h := newPlugin(t)

	bgs := h.Backgrounds()
	require.Len(t, bgs, 1, "the shell must be able to find it")

	p.SetGate(allowAll())
	block := make(chan struct{})
	defer close(block)
	p.monitors.run = scriptedRunner("", 0, block)
	_, err := p.monitors.Start(t.Context(), "serve", time.Now())
	require.NoError(t, err)
	waitFor(t, func() bool { return p.monitors.Running() == 1 })

	bgs[0].Stop()
	assert.Zero(t, p.monitors.Running(), "nothing may outlive the session")
}
