package loop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/permission"
)

// fixedGate answers every request the same way, which is what a test needs to
// prove the gate is consulted at all.
type fixedGate struct {
	decision permission.Decision
	reason   string

	mu       sync.Mutex
	requests []permission.Request
}

func (g *fixedGate) Check(_ context.Context, req permission.Request) (permission.Decision, string) {
	g.mu.Lock()
	g.requests = append(g.requests, req)
	g.mu.Unlock()
	return g.decision, g.reason
}

func (g *fixedGate) seen() []permission.Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]permission.Request(nil), g.requests...)
}

func allowAll() *fixedGate { return &fixedGate{decision: permission.Allow} }

// scriptedRunner replays output instead of starting a shell, so the suite does
// not depend on the host having one.
func scriptedRunner(output string, exitCode int, block <-chan struct{}) runner {
	return func(ctx context.Context, _ string, onChunk func(string)) (int, bool, error) {
		if output != "" {
			onChunk(output)
		}
		if block != nil {
			select {
			case <-block:
			case <-ctx.Done():
				return 0, true, nil
			}
		}
		if ctx.Err() != nil {
			return 0, true, nil
		}
		if exitCode != 0 {
			return exitCode, false, errors.New("exit status")
		}
		return 0, false, nil
	}
}

// newMonitors builds a supervisor with a scripted runner.
func newMonitors(gate permission.Gate, r runner) *Monitors {
	m := NewMonitors(gate)
	m.run = r
	return m
}

// waitFor polls until cond holds, so a test never sleeps a fixed duration.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition never held")
}

func TestStartRunsAndRecordsSuccess(t *testing.T) {
	m := newMonitors(allowAll(), scriptedRunner("building\ndone\n", 0, nil))

	mon, err := m.Start(t.Context(), "go build ./...", time.Now())
	require.NoError(t, err)
	assert.Equal(t, MonitorRunning, mon.State)

	waitFor(t, func() bool {
		got, _ := m.Get(mon.ID)
		return got.State == MonitorExited
	})

	got, err := m.Get(mon.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, got.ExitCode)
	assert.Equal(t, []string{"building", "done"}, got.Lines)
}

func TestStartRecordsAFailure(t *testing.T) {
	m := newMonitors(allowAll(), scriptedRunner("FAIL\n", 2, nil))

	mon, err := m.Start(t.Context(), "go test ./...", time.Now())
	require.NoError(t, err)
	waitFor(t, func() bool {
		got, _ := m.Get(mon.ID)
		return got.State == MonitorExited
	})

	got, _ := m.Get(mon.ID)
	assert.Equal(t, 2, got.ExitCode, "the exit code is how the agent knows it failed")
	assert.Contains(t, got.Tail(1), "FAIL")
}

// A background command is still a command. Without the gate, the model could
// run anything by calling MonitorCreate instead of bash.
func TestStartRequiresThePermissionGate(t *testing.T) {
	gate := &fixedGate{decision: permission.Deny, reason: "rm is denied by policy"}
	m := newMonitors(gate, scriptedRunner("", 0, nil))

	_, err := m.Start(t.Context(), "rm -rf /", time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rm is denied by policy", "the reason must reach the caller")
	assert.Empty(t, m.List(), "a denied command must not become a monitor")
}

// Ask is not Allow. A background command cannot show a prompt, so anything
// short of Allow has to be refused rather than run unattended.
func TestStartRefusesWhenTheGateWouldAsk(t *testing.T) {
	m := newMonitors(&fixedGate{decision: permission.Ask}, scriptedRunner("", 0, nil))

	_, err := m.Start(t.Context(), "curl example.com", time.Now())
	assert.Error(t, err)
}

func TestStartWithoutAGateRefuses(t *testing.T) {
	m := newMonitors(nil, scriptedRunner("", 0, nil))

	_, err := m.Start(t.Context(), "ls", time.Now())
	assert.ErrorIs(t, err, ErrNoGate)
}

// The gate must see the real command, or an allowlist cannot do its job.
func TestStartAsksTheGateAboutTheRealCommand(t *testing.T) {
	gate := allowAll()
	m := newMonitors(gate, scriptedRunner("", 0, nil))

	_, err := m.Start(t.Context(), "go test ./...", time.Now())
	require.NoError(t, err)

	seen := gate.seen()
	require.Len(t, seen, 1)
	assert.Equal(t, "go test ./...", seen[0].Command)
	assert.Equal(t, permission.ActionBash, seen[0].Action,
		"a background command is a bash action and must be judged as one")
}

func TestStartRejectsAnEmptyCommand(t *testing.T) {
	m := newMonitors(allowAll(), scriptedRunner("", 0, nil))

	_, err := m.Start(t.Context(), "   ", time.Now())
	assert.Error(t, err)
}

// A cancelled command did not fail on its own terms, so calling it a failing
// build would misreport it.
func TestStopReportsStoppedNotFailed(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	m := newMonitors(allowAll(), scriptedRunner("serving\n", 0, block))

	mon, err := m.Start(t.Context(), "npm run dev", time.Now())
	require.NoError(t, err)

	require.NoError(t, m.Stop(mon.ID))

	got, err := m.Get(mon.ID)
	require.NoError(t, err)
	assert.Equal(t, MonitorStopped, got.State)
	assert.Empty(t, got.Err, "a deliberate stop is not an error")
}

func TestStopIsIdempotent(t *testing.T) {
	m := newMonitors(allowAll(), scriptedRunner("", 0, nil))
	mon, err := m.Start(t.Context(), "true", time.Now())
	require.NoError(t, err)

	require.NoError(t, m.Stop(mon.ID))
	require.NoError(t, m.Stop(mon.ID), "stopping a finished monitor must not error")
}

// A background build must not outlive the session that started it.
func TestStopAll(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	m := newMonitors(allowAll(), scriptedRunner("", 0, block))

	for _, c := range []string{"a", "b"} {
		_, err := m.Start(t.Context(), c, time.Now())
		require.NoError(t, err)
	}
	waitFor(t, func() bool { return m.Running() == 2 })

	m.StopAll()
	assert.Zero(t, m.Running())
}

// A build that prints a megabyte must not reach the model.
func TestOutputIsBounded(t *testing.T) {
	var b strings.Builder
	for i := range maxLines * 3 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 1))
		b.WriteString(string(rune('0' + i%10)))
		b.WriteString("\n")
	}
	m := newMonitors(allowAll(), scriptedRunner(b.String(), 0, nil))

	mon, err := m.Start(t.Context(), "noisy", time.Now())
	require.NoError(t, err)
	waitFor(t, func() bool {
		got, _ := m.Get(mon.ID)
		return got.State == MonitorExited
	})

	got, _ := m.Get(mon.ID)
	assert.Len(t, got.Lines, maxLines, "the tail is capped")
}

// The tail is kept rather than the head, because a failure explains itself at
// the end.
func TestOutputKeepsTheTail(t *testing.T) {
	m := newMonitors(allowAll(), scriptedRunner(strings.Repeat("noise\n", maxLines)+"the real error\n", 0, nil))

	mon, err := m.Start(t.Context(), "noisy", time.Now())
	require.NoError(t, err)
	waitFor(t, func() bool {
		got, _ := m.Get(mon.ID)
		return got.State == MonitorExited
	})

	got, _ := m.Get(mon.ID)
	assert.Equal(t, "the real error", got.Tail(1))
}

func TestListIsInStartOrder(t *testing.T) {
	m := newMonitors(allowAll(), scriptedRunner("", 0, nil))
	first, err := m.Start(t.Context(), "first", time.Now())
	require.NoError(t, err)
	second, err := m.Start(t.Context(), "second", time.Now())
	require.NoError(t, err)

	got := m.List()
	require.Len(t, got, 2)
	assert.Equal(t, first.ID, got[0].ID)
	assert.Equal(t, second.ID, got[1].ID)
}

func TestUnknownMonitorIsReported(t *testing.T) {
	m := newMonitors(allowAll(), scriptedRunner("", 0, nil))

	_, err := m.Get("mon-99")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, m.Stop("mon-99"), ErrNotFound)
}
