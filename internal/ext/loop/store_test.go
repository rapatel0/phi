package loop

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// every builds a schedule that fires d apart, skipping the parser bounds so a
// test does not have to wait ten seconds per fire.
type every struct{ d time.Duration }

func (e every) Next(now time.Time) time.Time { return now.Add(e.d) }
func (e every) String() string               { return e.d.String() }

// never fires once and then stops, which is how a real cron behaves when it
// runs out of matching dates.
type never struct{ first time.Time }

func (n never) Next(now time.Time) time.Time {
	if now.Before(n.first) {
		return n.first
	}
	return time.Time{}
}
func (never) String() string { return "once" }

func addLoop(t *testing.T, s *Store, maxFires int, now time.Time) Loop {
	t.Helper()
	l, err := s.Add("check the build", every{d: time.Minute}, maxFires, now)
	require.NoError(t, err)
	return l
}

func TestAddAndList(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)

	l := addLoop(t, s, 0, now)
	assert.Equal(t, StateActive, l.State)
	assert.Equal(t, at(time.March, 2, 9, 1), l.NextAt)
	assert.Len(t, s.List(), 1)
}

// A loop with no prompt has nothing to say when it fires, so creating one is a
// mistake worth reporting at creation rather than at 3am.
func TestAddRejectsAnEmptyPrompt(t *testing.T) {
	_, err := NewStore().Add("   ", every{d: time.Minute}, 0, time.Now())
	assert.Error(t, err)
}

func TestAddRejectsAScheduleThatNeverFires(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)
	_, err := s.Add("x", never{first: now.Add(-time.Hour)}, 0, now)
	assert.Error(t, err, "a loop that can never fire must not be created")
}

func TestListIsInCreationOrder(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)
	first := addLoop(t, s, 0, now)
	second := addLoop(t, s, 0, now)

	got := s.List()
	require.Len(t, got, 2)
	assert.Equal(t, first.ID, got[0].ID)
	assert.Equal(t, second.ID, got[1].ID)
}

// Only loops that are both active and due may fire.
func TestDueIgnoresPausedAndFutureLoops(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)

	due := addLoop(t, s, 0, now)
	paused := addLoop(t, s, 0, now)
	_, err := s.SetState(paused.ID, StatePaused, now)
	require.NoError(t, err)

	assert.Empty(t, s.Due(now), "nothing is due yet")

	later := now.Add(2 * time.Minute)
	got := s.Due(later)
	require.Len(t, got, 1, "the paused loop must not fire")
	assert.Equal(t, due.ID, got[0].ID)
}

// A backlog drains oldest first, or a loop that fell behind starves.
func TestDueIsOldestFirst(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)

	slow, err := s.Add("slow", every{d: 10 * time.Minute}, 0, now)
	require.NoError(t, err)
	fast, err := s.Add("fast", every{d: time.Minute}, 0, now)
	require.NoError(t, err)

	got := s.Due(now.Add(time.Hour))
	require.Len(t, got, 2)
	assert.Equal(t, fast.ID, got[0].ID, "the earlier deadline comes first")
	assert.Equal(t, slow.ID, got[1].ID)
}

func TestRecordFireAdvancesTheSchedule(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)
	l := addLoop(t, s, 0, now)

	fired := now.Add(time.Minute)
	got, err := s.RecordFire(l.ID, fired, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, got.Fires)
	assert.Equal(t, fired, got.LastAt)
	assert.Equal(t, fired.Add(time.Minute), got.NextAt)
	assert.Equal(t, StateActive, got.State)
}

// A bounded loop has to stop on its own. Without this a "check five times"
// loop runs until the session ends.
func TestRecordFireEndsABoundedLoop(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)
	l := addLoop(t, s, 2, now)

	got, err := s.RecordFire(l.ID, now, nil)
	require.NoError(t, err)
	assert.Equal(t, StateActive, got.State)

	got, err = s.RecordFire(l.ID, now, nil)
	require.NoError(t, err)
	assert.Equal(t, StateDone, got.State, "the budget ran out")
	assert.True(t, got.NextAt.IsZero(), "a finished loop has no next fire")
	assert.Empty(t, s.Due(now.Add(time.Hour)), "a finished loop is never due")
}

func TestRecordFireEndsALoopWhoseScheduleRanOut(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)
	l, err := s.Add("once", never{first: now.Add(time.Minute)}, 0, now)
	require.NoError(t, err)

	got, err := s.RecordFire(l.ID, now.Add(time.Minute), nil)
	require.NoError(t, err)
	assert.Equal(t, StateDone, got.State)
}

// A loop that cannot reach the agent looks exactly like one with nothing to
// report, so the reason has to be kept.
func TestRecordFireKeepsTheFailureReason(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)
	l := addLoop(t, s, 0, now)

	got, err := s.RecordFire(l.ID, now, errors.New("the shell is busy"))
	require.NoError(t, err)
	assert.Contains(t, got.LastError, "busy")
	assert.Equal(t, StateActive, got.State, "a failed wake must not kill the loop")

	got, err = s.RecordFire(l.ID, now.Add(time.Minute), nil)
	require.NoError(t, err)
	assert.Empty(t, got.LastError, "a later success must clear the reason")
}

// Resuming recomputes from now. Otherwise a loop paused over a weekend fires
// immediately and then again for every slot it missed.
func TestResumeRecomputesFromNow(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)
	l := addLoop(t, s, 0, now)

	_, err := s.SetState(l.ID, StatePaused, now)
	require.NoError(t, err)

	resumed := now.Add(48 * time.Hour)
	got, err := s.SetState(l.ID, StateActive, resumed)
	require.NoError(t, err)

	assert.Equal(t, resumed.Add(time.Minute), got.NextAt)
	assert.Empty(t, s.Due(resumed), "it must not fire the moment it resumes")
}

func TestFinishedLoopCannotBeRestarted(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)
	l := addLoop(t, s, 1, now)
	_, err := s.RecordFire(l.ID, now, nil)
	require.NoError(t, err)

	_, err = s.SetState(l.ID, StateActive, now)
	assert.Error(t, err, "a finished loop must not silently come back")
}

func TestRemove(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)
	l := addLoop(t, s, 0, now)

	require.NoError(t, s.Remove(l.ID))
	assert.Empty(t, s.List())
	assert.Error(t, s.Remove(l.ID), "removing it twice must report the second")
}

func TestUnknownIDIsReported(t *testing.T) {
	s := NewStore()
	_, err := s.Get("loop-99")
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = s.SetState("loop-99", StatePaused, time.Now())
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = s.RecordFire("loop-99", time.Now(), nil)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRemaining(t *testing.T) {
	s := NewStore()
	now := at(time.March, 2, 9, 0)

	unbounded := addLoop(t, s, 0, now)
	_, bounded := unbounded.Remaining()
	assert.False(t, bounded, "zero maxFires means unbounded")

	l := addLoop(t, s, 3, now)
	got, ok := l.Remaining()
	assert.True(t, ok)
	assert.Equal(t, 3, got)
}
