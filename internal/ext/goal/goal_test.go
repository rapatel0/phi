package goal

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartRequiresAnObjective(t *testing.T) {
	var s State
	_, err := s.Start("   ", 0)
	require.ErrorIs(t, err, ErrEmptyObjective)
	assert.Nil(t, s.Current())
}

func TestStartSetsDefaults(t *testing.T) {
	var s State
	g, err := s.Start("ship the port", 0)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, g.Status)
	assert.Equal(t, DefaultMaxTurns, g.MaxTurns, "an unset limit must fall back to the default")
	assert.True(t, g.Active())
}

// Current returns a copy, so a caller cannot mutate the session's goal.
func TestCurrentIsACopy(t *testing.T) {
	var s State
	_, err := s.Start("objective", 0)
	require.NoError(t, err)

	got := s.Current()
	got.Objective = "hijacked"
	assert.Equal(t, "objective", s.Current().Objective)
}

// The guard that matters: a turn started under an older goal must not be able
// to close the goal that replaced it.
func TestCloseRejectsStaleID(t *testing.T) {
	var s State
	first, err := s.Start("first", 0)
	require.NoError(t, err)
	second, err := s.Start("second", 0)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)

	_, err = s.Complete(first.ID, "done")
	require.ErrorIs(t, err, ErrStaleID)
	assert.Equal(t, StatusActive, s.Current().Status, "the current goal must be untouched")

	_, err = s.Complete(second.ID, "done")
	require.NoError(t, err)
}

func TestCloseWithoutAGoal(t *testing.T) {
	var s State
	_, err := s.Complete("goal-1", "done")
	require.ErrorIs(t, err, ErrNoGoal)
}

// Completing without saying what was verified defeats the point of the tool.
func TestCompleteRequiresASummary(t *testing.T) {
	var s State
	g, err := s.Start("objective", 0)
	require.NoError(t, err)

	_, err = s.Complete(g.ID, "  ")
	require.Error(t, err)
	assert.Equal(t, StatusActive, s.Current().Status)
}

func TestBlockAndWaitRequireAReason(t *testing.T) {
	var s State
	g, err := s.Start("objective", 0)
	require.NoError(t, err)

	_, err = s.Block(g.ID, "")
	require.Error(t, err)
	_, err = s.Wait(g.ID, "")
	require.Error(t, err)
	assert.Equal(t, StatusActive, s.Current().Status)
}

// Blocked is terminal; waiting is not, so waiting can resume.
func TestWaitingCanResumeButCompleteCannot(t *testing.T) {
	var s State
	g, err := s.Start("objective", 0)
	require.NoError(t, err)

	_, err = s.Wait(g.ID, "waiting for CI")
	require.NoError(t, err)
	assert.Equal(t, StatusWaiting, s.Current().Status)

	resumed, err := s.Resume()
	require.NoError(t, err)
	assert.Equal(t, StatusActive, resumed.Status)

	_, err = s.Complete(g.ID, "done")
	require.NoError(t, err)
	_, err = s.Resume()
	require.Error(t, err, "a completed goal must not be resumable")
}

func TestStatusDone(t *testing.T) {
	assert.False(t, StatusActive.Done())
	assert.False(t, StatusWaiting.Done(), "waiting expects to continue")
	assert.True(t, StatusComplete.Done())
	assert.True(t, StatusBlocked.Done())
	assert.True(t, StatusPaused.Done())
}

// The turn limit is a safety net against a goal that loops without finishing.
func TestRecordTurnPausesAtTheLimit(t *testing.T) {
	var s State
	_, err := s.Start("objective", 2)
	require.NoError(t, err)

	assert.Equal(t, StatusActive, s.RecordTurn().Status)
	paused := s.RecordTurn()
	assert.Equal(t, StatusPaused, paused.Status)
	assert.Contains(t, paused.Reason, "2 turns")

	// A paused goal must stop counting, or the reason keeps changing.
	assert.Equal(t, 2, s.RecordTurn().Turns)
}

func TestRecordTurnWithoutAGoal(t *testing.T) {
	var s State
	assert.Nil(t, s.RecordTurn())
}

// The reminder must carry the id, or the model cannot pass the stale-id guard.
func TestReminderCarriesTheID(t *testing.T) {
	var s State
	g, err := s.Start("ship the port", 5)
	require.NoError(t, err)

	got := Reminder(g)
	assert.Contains(t, got, "ship the port")
	assert.Contains(t, got, g.ID)
	assert.Contains(t, got, "goal_complete")
}

// An inactive goal must not keep injecting a reminder.
func TestReminderOnlyWhileActive(t *testing.T) {
	assert.Empty(t, Reminder(nil))
	assert.Empty(t, Reminder(&Goal{Status: StatusComplete}))
	assert.Empty(t, Reminder(&Goal{Status: StatusPaused}))
}

func TestDescribeExplainsAnEmptyState(t *testing.T) {
	assert.Contains(t, Describe(nil), "/goal <objective>")
}

func TestDescribeShowsReasonAndSummary(t *testing.T) {
	got := Describe(&Goal{
		ID: "goal-1", Objective: "obj", Status: StatusBlocked,
		Reason: "needs credentials", MaxTurns: 10,
	})
	assert.Contains(t, got, "goal-1")
	assert.Contains(t, got, "blocked")
	assert.Contains(t, got, "needs credentials")
}

func TestFooter(t *testing.T) {
	assert.Empty(t, Footer(nil))
	assert.Equal(t, "goal:2/10", Footer(&Goal{Status: StatusActive, Turns: 2, MaxTurns: 10}))
	assert.Equal(t, "goal:blocked", Footer(&Goal{Status: StatusBlocked}))
}

// Concurrent tool calls and turn hooks touch the same state.
func TestStateIsConcurrencySafe(t *testing.T) {
	var s State
	_, err := s.Start("objective", 0)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				s.RecordTurn()
				_ = Describe(s.Current())
			}
		})
	}
	wg.Wait()
	assert.NotNil(t, s.Current())
}

func TestStripReminderRestoresTheBase(t *testing.T) {
	const base = "BASE PROMPT"
	g := &Goal{ID: "goal-1", Objective: "obj", Status: StatusActive, MaxTurns: 5}
	withGoal := base + Reminder(g)

	assert.Equal(t, base, stripReminder(withGoal))
	assert.Equal(t, base, stripReminder(base), "no reminder is not an error")
	assert.Equal(t, 1, strings.Count(stripReminder(withGoal)+Reminder(g), reminderHeader),
		"re-applying must not stack reminders")
}
