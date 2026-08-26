// Package goal is the pi-goal analog: a session-scoped objective the agent
// keeps working toward across turns.
//
// Without it, a long task drifts. The agent finishes a turn, the user says
// "continue", and the objective slowly blurs. A goal pins the objective, feeds
// it back at the start of each turn, and ends only for a stated reason:
// complete, blocked, or waiting on something external.
//
// The stale-id guard matters. A turn that started under an older goal must not
// be able to close the goal that replaced it, so every closing tool takes the
// id it believes is active and is rejected when that id is not current.
package goal

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Status is where a goal currently stands.
type Status string

const (
	// StatusActive means the agent should keep working.
	StatusActive Status = "active"
	// StatusComplete means the work is done and verified.
	StatusComplete Status = "complete"
	// StatusBlocked means only the user or an external system can proceed.
	StatusBlocked Status = "blocked"
	// StatusWaiting means the goal expects an external event.
	StatusWaiting Status = "waiting"
	// StatusPaused means the user stopped it, or a safety limit tripped.
	StatusPaused Status = "paused"
)

// Done reports whether the goal no longer wants turns.
func (s Status) Done() bool {
	return s == StatusComplete || s == StatusBlocked || s == StatusPaused
}

// Goal is one session objective.
type Goal struct {
	ID        string
	Objective string
	Status    Status
	Reason    string // why it is blocked, waiting, or paused
	Summary   string // what completing it accomplished
	Turns     int    // turns spent since activation
	MaxTurns  int    // safety limit; 0 means no limit
	Started   time.Time
}

// Active reports whether the goal still wants turns.
func (g *Goal) Active() bool { return g != nil && g.Status == StatusActive }

// DefaultMaxTurns stops a goal that is looping without finishing. A goal that
// needs more can be restarted with an explicit limit.
const DefaultMaxTurns = 40

// Errors a caller can test for.
var (
	// ErrNoGoal means no goal is active.
	ErrNoGoal = errors.New("no goal is active")
	// ErrStaleID means the caller named a goal that is no longer current,
	// which happens when a turn outlives the goal that started it.
	ErrStaleID = errors.New("that goal is no longer active")
	// ErrEmptyObjective means /goal was called with nothing to pursue.
	ErrEmptyObjective = errors.New("say what the goal is: /goal <objective>")
)

// State holds the session's goal. It is guarded because tool calls and the
// turn hook run on different goroutines.
type State struct {
	mu   sync.RWMutex
	goal *Goal
	seq  int
}

// Start replaces any current goal with a new one.
func (s *State) Start(objective string, maxTurns int) (*Goal, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return nil, ErrEmptyObjective
	}
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.goal = &Goal{
		ID:        fmt.Sprintf("goal-%d", s.seq),
		Objective: objective,
		Status:    StatusActive,
		MaxTurns:  maxTurns,
		Started:   time.Now(),
	}
	return s.snapshotLocked(), nil
}

// Current returns a copy of the goal, or nil when there is none.
func (s *State) Current() *Goal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

// snapshotLocked copies the goal so a caller cannot mutate shared state.
func (s *State) snapshotLocked() *Goal {
	if s.goal == nil {
		return nil
	}
	g := *s.goal
	return &g
}

// Clear drops the goal entirely.
func (s *State) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goal = nil
}

// close moves the goal to a terminal state, rejecting a stale id.
func (s *State) close(id string, status Status, reason, summary string) (*Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.goal == nil {
		return nil, ErrNoGoal
	}
	// A turn that began under an older goal must not close its replacement.
	if id != "" && id != s.goal.ID {
		return nil, fmt.Errorf("%w: %q is not %q", ErrStaleID, id, s.goal.ID)
	}
	s.goal.Status = status
	s.goal.Reason = reason
	if summary != "" {
		s.goal.Summary = summary
	}
	return s.snapshotLocked(), nil
}

// Complete marks the goal finished. summary states what was accomplished.
func (s *State) Complete(id, summary string) (*Goal, error) {
	if strings.TrimSpace(summary) == "" {
		return nil, errors.New("say what was completed and what verified it")
	}
	return s.close(id, StatusComplete, "", summary)
}

// Block stops the goal because a user or external action is required.
func (s *State) Block(id, reason string) (*Goal, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("say what action is required to unblock the goal")
	}
	return s.close(id, StatusBlocked, reason, "")
}

// Wait parks the goal until an external event arrives. Unlike Block it can
// resume, so it is not a terminal failure.
func (s *State) Wait(id, reason string) (*Goal, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("say which event the goal is waiting for")
	}
	return s.close(id, StatusWaiting, reason, "")
}

// Pause stops the goal without judging it, for /goal pause.
func (s *State) Pause() (*Goal, error) { return s.close("", StatusPaused, "paused by the user", "") }

// Resume returns a paused or waiting goal to active.
func (s *State) Resume() (*Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.goal == nil {
		return nil, ErrNoGoal
	}
	if s.goal.Status == StatusComplete {
		return nil, errors.New("that goal is already complete; start a new one")
	}
	s.goal.Status = StatusActive
	s.goal.Reason = ""
	return s.snapshotLocked(), nil
}

// RecordTurn counts a turn against the goal and pauses it at the limit.
//
// The limit is a safety net, not a schedule: a goal that keeps starting turns
// without finishing is usually looping, and stopping beats burning tokens.
func (s *State) RecordTurn() *Goal {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.goal == nil || s.goal.Status != StatusActive {
		return s.snapshotLocked()
	}
	s.goal.Turns++
	if s.goal.MaxTurns > 0 && s.goal.Turns >= s.goal.MaxTurns {
		s.goal.Status = StatusPaused
		s.goal.Reason = fmt.Sprintf(
			"stopped after %d turns without finishing. Resume with /goal resume, or restart with a new objective.",
			s.goal.Turns)
	}
	return s.snapshotLocked()
}

// Reminder is the text injected at the start of a turn while a goal is active.
// It is deliberately short: it competes with the user's own prompt for
// attention, so it states the objective and the exit conditions, nothing more.
func Reminder(g *Goal) string {
	if !g.Active() {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n# Active goal\n")
	fmt.Fprintf(&b, "Objective: %s\n", g.Objective)
	fmt.Fprintf(&b, "Goal id: %s (turn %d", g.ID, g.Turns)
	if g.MaxTurns > 0 {
		fmt.Fprintf(&b, " of %d", g.MaxTurns)
	}
	b.WriteString(")\n")
	b.WriteString("Keep working until the goal is met. Do not stop with only a plan.\n")
	b.WriteString("Close it with goal_complete, goal_blocked, or goal_wait, passing the goal id above.\n")
	return b.String()
}

// Describe renders the goal for /goal status.
func Describe(g *Goal) string {
	if g == nil {
		return "No goal is active. Start one with /goal <objective>."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Goal %s: %s\n", g.ID, g.Objective)
	fmt.Fprintf(&b, "Status: %s", g.Status)
	if g.MaxTurns > 0 {
		fmt.Fprintf(&b, " (turn %d of %d)", g.Turns, g.MaxTurns)
	}
	if g.Reason != "" {
		fmt.Fprintf(&b, "\nReason: %s", g.Reason)
	}
	if g.Summary != "" {
		fmt.Fprintf(&b, "\nSummary: %s", g.Summary)
	}
	return b.String()
}

// Footer renders the compact status line.
func Footer(g *Goal) string {
	if g == nil {
		return ""
	}
	if g.Active() {
		return fmt.Sprintf("goal:%d/%d", g.Turns, g.MaxTurns)
	}
	return "goal:" + string(g.Status)
}
