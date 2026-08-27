package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rapatel0/alpha/internal/permission"
	"github.com/rapatel0/alpha/internal/tools/bashtool"
)

// MonitorState is where a background command is in its life.
type MonitorState string

const (
	// MonitorRunning is still executing.
	MonitorRunning MonitorState = "running"
	// MonitorExited finished on its own, successfully or not.
	MonitorExited MonitorState = "exited"
	// MonitorStopped was cancelled by the user or the session ending.
	MonitorStopped MonitorState = "stopped"
)

// maxLines bounds what one monitor keeps.
//
// A build that prints a megabyte would otherwise sit in memory and, worse,
// reach the model as a tool result. The tail is kept rather than the head
// because a failure explains itself at the end.
const maxLines = 200

// Monitor is one supervised background command.
type Monitor struct {
	ID      string
	Command string
	State   MonitorState

	// ExitCode is meaningful once State is MonitorExited.
	ExitCode int
	Err      string

	StartedAt  time.Time
	FinishedAt time.Time

	mu    sync.Mutex
	lines []string
	stop  context.CancelFunc
	done  chan struct{}
}

// appendChunk splits streamed output into lines and keeps the tail.
func (m *Monitor) appendChunk(chunk string) {
	if chunk == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for line := range strings.SplitSeq(strings.TrimSuffix(chunk, "\n"), "\n") {
		m.lines = append(m.lines, line)
	}
	if len(m.lines) > maxLines {
		m.lines = m.lines[len(m.lines)-maxLines:]
	}
}

// MonitorInfo is a copyable view of a monitor.
//
// Monitor itself owns a mutex, so handing one out by value would copy the lock
// and race with the goroutine still writing to it.
type MonitorInfo struct {
	ID      string
	Command string
	State   MonitorState

	ExitCode int
	Err      string

	StartedAt  time.Time
	FinishedAt time.Time

	Lines []string
}

// Tail returns at most n trailing lines as one string.
func (m MonitorInfo) Tail(n int) string {
	lines := m.Lines
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Runtime reports how long the command ran, or has been running.
func (m MonitorInfo) Runtime(now time.Time) time.Duration {
	if m.FinishedAt.IsZero() {
		return now.Sub(m.StartedAt)
	}
	return m.FinishedAt.Sub(m.StartedAt)
}

// snapshot copies the fields a reader needs, so a caller cannot observe a
// half-written terminal state.
func (m *Monitor) snapshot() MonitorInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return MonitorInfo{
		ID: m.ID, Command: m.Command, State: m.State,
		ExitCode: m.ExitCode, Err: m.Err,
		StartedAt: m.StartedAt, FinishedAt: m.FinishedAt,
		Lines: append([]string(nil), m.lines...),
	}
}

// ErrNoGate reports a supervisor built without a permission gate.
//
// A background command is still a command. Running one outside the gate would
// give the model a way to execute anything by calling MonitorCreate instead of
// bash, so a missing gate refuses rather than defaults to allow.
var ErrNoGate = errors.New("monitor: no permission gate: background commands must pass the same gate as bash")

// Monitors supervises background commands.
type Monitors struct {
	mu     sync.Mutex
	byID   map[string]*Monitor
	order  []string
	nextID int

	// gate decides whether a command may run at all. It is the same gate the
	// bash tool uses.
	gate permission.Gate

	// run executes a gated command, streaming output. It is the bash tool's
	// executor, so there is one audited shell sink rather than two. Tests
	// replace it to avoid depending on a shell.
	run runner
}

// runner executes a command, calling onChunk as output arrives.
type runner func(ctx context.Context, command string, onChunk func(string)) (exitCode int, canceled bool, err error)

// execRunner routes through the bash tool's executor.
func execRunner(ctx context.Context, command string, onChunk func(string)) (int, bool, error) {
	res, err := bashtool.ExecShell(ctx, command, bashtool.ShellExecOptions{OnChunk: onChunk})
	return res.ExitCode, res.Canceled, err
}

// NewMonitors returns a supervisor that runs commands only when gate allows.
func NewMonitors(gate permission.Gate) *Monitors {
	return &Monitors{byID: map[string]*Monitor{}, gate: gate, run: execRunner}
}

// Start launches a command in the background and returns immediately.
//
// The gate runs first and a denial is returned with its reason, so the model
// cannot use a monitor to sidestep the permission it would need for bash.
func (m *Monitors) Start(ctx context.Context, command string, now time.Time) (MonitorInfo, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return MonitorInfo{}, errors.New("monitor: empty command")
	}
	if m.gate == nil {
		return MonitorInfo{}, ErrNoGate
	}

	decision, reason := m.gate.Check(ctx, permission.Request{
		Action:  permission.ActionBash,
		Tool:    "MonitorCreate",
		Command: command,
	})
	if decision != permission.Allow {
		if reason == "" {
			reason = "denied by the permission policy"
		}
		return MonitorInfo{}, fmt.Errorf("monitor: %q not allowed: %s", command, reason)
	}

	runCtx, cancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("mon-%d", m.nextID)
	mon := &Monitor{
		ID: id, Command: command, State: MonitorRunning,
		StartedAt: now, stop: cancel, done: make(chan struct{}),
	}
	m.byID[id] = mon
	m.order = append(m.order, id)
	run := m.run
	m.mu.Unlock()

	go supervise(runCtx, mon, command, run)
	return mon.snapshot(), nil
}

// supervise runs the command and records the terminal state.
func supervise(ctx context.Context, mon *Monitor, command string, run runner) {
	defer close(mon.done)

	exitCode, canceled, err := run(ctx, command, mon.appendChunk)

	mon.mu.Lock()
	defer mon.mu.Unlock()
	mon.FinishedAt = time.Now()
	mon.ExitCode = exitCode

	// A cancelled command did not fail on its own terms, so it is reported
	// as stopped rather than as a failing build.
	switch {
	case mon.State == MonitorStopped, canceled:
		mon.State = MonitorStopped
	default:
		mon.State = MonitorExited
		if err != nil {
			mon.Err = err.Error()
		}
	}
	mon.stop()
}

// List returns a snapshot of the monitors in start order.
func (m *Monitors) List() []MonitorInfo {
	m.mu.Lock()
	mons := make([]*Monitor, 0, len(m.order))
	for _, id := range m.order {
		if mon, ok := m.byID[id]; ok {
			mons = append(mons, mon)
		}
	}
	m.mu.Unlock()

	out := make([]MonitorInfo, 0, len(mons))
	for _, mon := range mons {
		out = append(out, mon.snapshot())
	}
	return out
}

// Get returns a snapshot of one monitor.
func (m *Monitors) Get(id string) (MonitorInfo, error) {
	mon, err := m.lookup(id)
	if err != nil {
		return MonitorInfo{}, err
	}
	return mon.snapshot(), nil
}

func (m *Monitors) lookup(id string) (*Monitor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mon, ok := m.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return mon, nil
}

// Stop cancels a running command and waits for it to exit.
func (m *Monitors) Stop(id string) error {
	mon, err := m.lookup(id)
	if err != nil {
		return err
	}

	mon.mu.Lock()
	if mon.State != MonitorRunning {
		mon.mu.Unlock()
		return nil
	}
	mon.State = MonitorStopped
	stop := mon.stop
	mon.mu.Unlock()

	stop()
	<-mon.done
	return nil
}

// StopAll cancels every running command. The shell calls it at shutdown, so a
// background build does not outlive the session that started it.
func (m *Monitors) StopAll() {
	for _, mon := range m.List() {
		_ = m.Stop(mon.ID)
	}
}

// Running counts the commands still executing.
func (m *Monitors) Running() int {
	m.mu.Lock()
	mons := make([]*Monitor, 0, len(m.byID))
	for _, mon := range m.byID {
		mons = append(mons, mon)
	}
	m.mu.Unlock()

	n := 0
	for _, mon := range mons {
		mon.mu.Lock()
		if mon.State == MonitorRunning {
			n++
		}
		mon.mu.Unlock()
	}
	return n
}
