package loop

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// State is where a loop is in its life.
type State string

const (
	// StateActive fires on schedule.
	StateActive State = "active"
	// StatePaused keeps the loop and its history but stops firing.
	StatePaused State = "paused"
	// StateDone is terminal: the fire budget ran out or the user cancelled.
	StateDone State = "done"
)

// Loop is one scheduled prompt.
type Loop struct {
	ID       string
	Prompt   string
	Schedule Schedule
	State    State

	// MaxFires bounds a loop that would otherwise run forever. Zero means
	// unbounded, which is right for a genuine watchdog and wrong for
	// anything that polls for a condition that will eventually hold.
	MaxFires int
	Fires    int

	// LastError is why the most recent fire did not reach the agent. It is
	// kept because a loop that silently fails to wake anyone looks exactly
	// like a loop that has nothing to report.
	LastError string

	CreatedAt time.Time
	NextAt    time.Time
	LastAt    time.Time
}

// Remaining reports how many fires are left, and whether the loop is bounded.
func (l Loop) Remaining() (int, bool) {
	if l.MaxFires <= 0 {
		return 0, false
	}
	return max(l.MaxFires-l.Fires, 0), true
}

// ErrNotFound reports an ID that no loop has.
var ErrNotFound = errors.New("loop: no such loop")

// Store holds the loops. It is the single owner of loop state: the scheduler
// reads through it and the tools mutate through it, so there is one lock.
type Store struct {
	mu     sync.Mutex
	loops  map[string]*Loop
	order  []string // creation order, for stable listing
	nextID int
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{loops: map[string]*Loop{}}
}

// Add creates a loop and returns a copy of it.
func (s *Store) Add(prompt string, sched Schedule, maxFires int, now time.Time) (Loop, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Loop{}, errors.New("loop: empty prompt: a loop with nothing to say cannot do anything when it fires")
	}
	if sched == nil {
		return Loop{}, errors.New("loop: no schedule")
	}
	if maxFires < 0 {
		return Loop{}, fmt.Errorf("loop: maxFires %d is negative: use 0 for unbounded", maxFires)
	}

	next := sched.Next(now)
	if next.IsZero() {
		return Loop{}, fmt.Errorf("loop: schedule %q never fires", sched.String())
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("loop-%d", s.nextID)
	l := &Loop{
		ID: id, Prompt: prompt, Schedule: sched, State: StateActive,
		MaxFires: maxFires, CreatedAt: now, NextAt: next,
	}
	s.loops[id] = l
	s.order = append(s.order, id)
	return *l, nil
}

// List returns the loops in creation order.
func (s *Store) List() []Loop {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Loop, 0, len(s.order))
	for _, id := range s.order {
		if l, ok := s.loops[id]; ok {
			out = append(out, *l)
		}
	}
	return out
}

// Get returns one loop.
func (s *Store) Get(id string) (Loop, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.loops[id]
	if !ok {
		return Loop{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return *l, nil
}

// SetState pauses, resumes, or ends a loop.
//
// Resuming recomputes the next firing from now rather than from when it was
// paused, or a loop paused over a weekend fires immediately on resume and then
// again for every schedule slot it missed.
func (s *Store) SetState(id string, state State, now time.Time) (Loop, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.loops[id]
	if !ok {
		return Loop{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if l.State == StateDone && state != StateDone {
		return Loop{}, fmt.Errorf("loop: %s already finished and cannot be restarted", id)
	}
	l.State = state
	if state == StateActive {
		l.NextAt = l.Schedule.Next(now)
		l.LastError = ""
	}
	return *l, nil
}

// Remove deletes a loop.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.loops[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	delete(s.loops, id)
	s.order = slicesDelete(s.order, id)
	return nil
}

// Due returns the active loops whose time has come, oldest first so a backlog
// drains in the order it built up.
func (s *Store) Due(now time.Time) []Loop {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []Loop
	for _, id := range s.order {
		l, ok := s.loops[id]
		if !ok || l.State != StateActive || l.NextAt.IsZero() || l.NextAt.After(now) {
			continue
		}
		due = append(due, *l)
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].NextAt.Before(due[j].NextAt) })
	return due
}

// RecordFire advances a loop after it fired.
//
// fireErr is stored rather than returned: the loop keeps running, and the
// reason belongs in LoopList where the user will look for it.
func (s *Store) RecordFire(id string, now time.Time, fireErr error) (Loop, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.loops[id]
	if !ok {
		return Loop{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	l.Fires++
	l.LastAt = now
	l.LastError = ""
	if fireErr != nil {
		l.LastError = fireErr.Error()
	}

	if l.MaxFires > 0 && l.Fires >= l.MaxFires {
		l.State = StateDone
		l.NextAt = time.Time{}
		return *l, nil
	}

	l.NextAt = l.Schedule.Next(now)
	if l.NextAt.IsZero() {
		l.State = StateDone
	}
	return *l, nil
}

// slicesDelete removes the first occurrence of id.
func slicesDelete(ids []string, id string) []string {
	for i, v := range ids {
		if v == id {
			return append(ids[:i], ids[i+1:]...)
		}
	}
	return ids
}
