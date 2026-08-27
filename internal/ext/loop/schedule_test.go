package loop

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// at builds a time in UTC, which keeps the cron cases readable.
func at(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, time.UTC)
}

func TestParseInterval(t *testing.T) {
	s, err := ParseSchedule("30m")
	require.NoError(t, err)

	now := at(time.March, 2, 9, 0)
	assert.Equal(t, at(time.March, 2, 9, 30), s.Next(now))
}

// A loop that fires faster than a turn completes starts the next one before
// the last finished, and spends the budget on nothing.
func TestParseIntervalRejectsATooShortOne(t *testing.T) {
	_, err := ParseSchedule("1s")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minimum")
}

func TestParseScheduleRejectsGarbage(t *testing.T) {
	for _, spec := range []string{"", "   ", "soon", "5", "5x"} {
		_, err := ParseSchedule(spec)
		assert.Error(t, err, "spec %q must be refused", spec)
	}
}

// A daily 9am check is the reason cron is accepted at all: the same wall-clock
// moment every day, with no drift from the runtime of each fire.
func TestCronFiresAtTheSameWallClockTime(t *testing.T) {
	s, err := ParseSchedule("0 9 * * *")
	require.NoError(t, err)

	first := s.Next(at(time.March, 2, 8, 59))
	assert.Equal(t, at(time.March, 2, 9, 0), first)

	// The fire itself takes time. The next one must still land at 9:00.
	assert.Equal(t, at(time.March, 3, 9, 0), s.Next(first.Add(7*time.Minute)))
}

// Next is strictly after now, or a loop fires twice on the same minute.
func TestCronSkipsTheCurrentMinute(t *testing.T) {
	s, err := ParseSchedule("0 9 * * *")
	require.NoError(t, err)

	assert.Equal(t, at(time.March, 3, 9, 0), s.Next(at(time.March, 2, 9, 0)))
}

func TestCronWeekdayRange(t *testing.T) {
	s, err := ParseSchedule("0 9 * * 1-5")
	require.NoError(t, err)

	// Saturday 7 March 2026 -> the next fire is Monday the 9th.
	next := s.Next(at(time.March, 7, 10, 0))
	assert.Equal(t, time.Monday, next.Weekday())
	assert.Equal(t, at(time.March, 9, 9, 0), next)
}

func TestCronStepAndList(t *testing.T) {
	step, err := ParseSchedule("*/15 * * * *")
	require.NoError(t, err)
	assert.Equal(t, at(time.March, 2, 9, 15), step.Next(at(time.March, 2, 9, 7)))

	list, err := ParseSchedule("0,30 * * * *")
	require.NoError(t, err)
	assert.Equal(t, at(time.March, 2, 9, 30), list.Next(at(time.March, 2, 9, 5)))
}

// Cron ORs day-of-month against day-of-week when both are restricted, so
// "1 * 1" means the first of the month and every Monday.
func TestCronOrsTheTwoDayFields(t *testing.T) {
	s, err := ParseSchedule("0 9 1 * 1")
	require.NoError(t, err)

	// Thursday 2 April 2026 is neither, so the next fire is Monday the 6th.
	assert.Equal(t, at(time.April, 6, 9, 0), s.Next(at(time.April, 2, 10, 0)))

	// 1 May 2026 is a Friday: matched by day-of-month alone.
	assert.Equal(t, at(time.May, 1, 9, 0), s.Next(at(time.April, 30, 10, 0)))
}

// A schedule that can never match must report that rather than scan forever.
func TestCronThatNeverMatches(t *testing.T) {
	s, err := ParseSchedule("0 9 30 2 *")
	require.NoError(t, err)

	assert.True(t, s.Next(at(time.March, 2, 9, 0)).IsZero(),
		"February 30th never comes; want the zero time")
}

func TestCronRejectsBadFields(t *testing.T) {
	cases := map[string]string{
		"too few fields":      "0 9 * *",
		"too many fields":     "0 9 * * * *",
		"minute out of range": "60 9 * * *",
		"hour out of range":   "0 24 * * *",
		"reversed range":      "0 5-1 * * *",
		"zero step":           "*/0 * * * *",
		"not a number":        "x 9 * * *",
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSchedule(spec)
			assert.Error(t, err)
		})
	}
}

// The error names the field, because "invalid cron" does not say which of the
// five to fix.
func TestCronErrorNamesTheField(t *testing.T) {
	_, err := ParseSchedule("0 24 * * *")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hour")
}
