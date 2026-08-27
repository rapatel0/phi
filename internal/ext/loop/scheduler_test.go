package loop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock lets a daily schedule be verified without waiting a day.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// recorder collects the prompts the scheduler sent.
type recorder struct {
	mu     sync.Mutex
	got    []string
	err    error
	notify chan struct{}
}

func newRecorder() *recorder { return &recorder{notify: make(chan struct{}, 16)} }

func (r *recorder) wake(text string) error {
	r.mu.Lock()
	r.got = append(r.got, text)
	err := r.err
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
	return err
}

func (r *recorder) prompts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

func (r *recorder) setErr(err error) {
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
}

// newTestScheduler wires a store to a fake clock, and fires on demand rather
// than on the real ticker.
func newTestScheduler(_ *testing.T, s *Store, r *recorder, now time.Time) (*Scheduler, *fakeClock) {
	clk := &fakeClock{now: now}
	sched := NewScheduler(s, r.wake)
	sched.clock = clk
	return sched, clk
}

func TestFireDueWakesTheAgent(t *testing.T) {
	now := at(time.March, 2, 9, 0)
	s := NewStore()
	_, err := s.Add("run the checks", every{d: time.Minute}, 0, now)
	require.NoError(t, err)

	r := newRecorder()
	sched, clk := newTestScheduler(t, s, r, now)

	sched.fireDue(t.Context())
	assert.Empty(t, r.prompts(), "nothing is due yet")

	clk.advance(2 * time.Minute)
	sched.fireDue(t.Context())
	assert.Equal(t, []string{"run the checks"}, r.prompts())
}

// One prompt per loop, in order. Batching would blur which loop asked for
// what, and the agent cannot report on a loop it cannot name.
func TestFireDueSendsOnePromptPerLoop(t *testing.T) {
	now := at(time.March, 2, 9, 0)
	s := NewStore()
	_, err := s.Add("first", every{d: time.Minute}, 0, now)
	require.NoError(t, err)
	_, err = s.Add("second", every{d: 2 * time.Minute}, 0, now)
	require.NoError(t, err)

	r := newRecorder()
	sched, clk := newTestScheduler(t, s, r, now)
	clk.advance(5 * time.Minute)
	sched.fireDue(t.Context())

	assert.Equal(t, []string{"first", "second"}, r.prompts())
}

// A fire has to advance the schedule, or the same loop fires on every tick
// forever.
func TestFireDueAdvancesTheLoop(t *testing.T) {
	now := at(time.March, 2, 9, 0)
	s := NewStore()
	l, err := s.Add("x", every{d: time.Minute}, 0, now)
	require.NoError(t, err)

	r := newRecorder()
	sched, clk := newTestScheduler(t, s, r, now)

	clk.advance(2 * time.Minute)
	sched.fireDue(t.Context())
	sched.fireDue(t.Context())

	assert.Len(t, r.prompts(), 1, "a second tick at the same time must not refire")

	got, err := s.Get(l.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Fires)
}

// The shell refuses while a turn is streaming. The loop keeps its schedule and
// records why, because a silent failure looks like nothing to report.
func TestFireDueKeepsGoingWhenTheShellRefuses(t *testing.T) {
	now := at(time.March, 2, 9, 0)
	s := NewStore()
	l, err := s.Add("x", every{d: time.Minute}, 0, now)
	require.NoError(t, err)

	r := newRecorder()
	r.setErr(errors.New("busy streaming"))
	sched, clk := newTestScheduler(t, s, r, now)

	clk.advance(2 * time.Minute)
	sched.fireDue(t.Context())

	got, err := s.Get(l.ID)
	require.NoError(t, err)
	assert.Equal(t, StateActive, got.State, "a refused wake must not kill the loop")
	assert.Contains(t, got.LastError, "busy streaming")
}

func TestFireDueStopsOnCancel(t *testing.T) {
	now := at(time.March, 2, 9, 0)
	s := NewStore()
	for _, p := range []string{"a", "b", "c"} {
		_, err := s.Add(p, every{d: time.Minute}, 0, now)
		require.NoError(t, err)
	}

	r := newRecorder()
	sched, clk := newTestScheduler(t, s, r, now)
	clk.advance(2 * time.Minute)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sched.fireDue(ctx)

	assert.Empty(t, r.prompts(), "a cancelled context must fire nothing")
}

// Start and Stop have to be safe to call more than once: a second load would
// otherwise double-fire every loop, and a Stop without a Start would panic.
func TestStartIsIdempotentAndStopIsSafe(t *testing.T) {
	s := NewStore()
	r := newRecorder()
	sched := NewScheduler(s, r.wake)

	sched.Stop() // never started

	ctx := t.Context()
	sched.Start(ctx)
	sched.Start(ctx)
	sched.Stop()
	sched.Stop()
}

// The background loop must actually fire without anyone calling fireDue.
func TestSchedulerFiresInTheBackground(t *testing.T) {
	now := at(time.March, 2, 9, 0)
	s := NewStore()
	_, err := s.Add("background work", every{d: time.Minute}, 0, now)
	require.NoError(t, err)

	r := newRecorder()
	sched, clk := newTestScheduler(t, s, r, now)
	sched.tickEvery = 5 * time.Millisecond
	clk.advance(2 * time.Minute)

	sched.Start(t.Context())
	defer sched.Stop()

	select {
	case <-r.notify:
	case <-time.After(2 * time.Second):
		t.Fatal("the scheduler never fired on its own")
	}
	assert.Equal(t, []string{"background work"}, r.prompts())
}
