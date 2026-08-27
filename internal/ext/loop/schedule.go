// Package loop runs work on a schedule and supervises background commands.
//
// It exists because the alternative is a shell `sleep` in a bash tool call,
// which blocks the tool loop and holds a turn open for the whole wait. A loop
// keeps the schedule outside the conversation and starts a turn when it fires.
package loop

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule decides when a loop fires next.
type Schedule interface {
	// Next returns the first firing strictly after now. The zero time means
	// the schedule never fires again.
	Next(now time.Time) time.Time
	String() string
}

// ParseSchedule reads an interval ("30s", "5m", "2h") or a five-field cron
// expression ("0 9 * * 1-5").
//
// Both forms are accepted because they answer different questions. An interval
// says how long to wait; cron says which wall-clock moments count. A daily
// 9am check written as an interval drifts by the runtime of every fire.
func ParseSchedule(spec string) (Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf(
			"loop: empty schedule: use an interval like 30m or a cron expression like %q",
			"0 9 * * *",
		)
	}
	if strings.Contains(spec, " ") {
		return parseCron(spec)
	}
	return parseInterval(spec)
}

// interval fires a fixed duration apart.
type interval struct {
	d time.Duration
}

func (i interval) Next(now time.Time) time.Time { return now.Add(i.d) }
func (i interval) String() string               { return i.d.String() }

// minInterval keeps a loop from firing faster than the agent can answer. A
// one-second loop would start a turn before the previous one finished and
// spend the budget on nothing.
const minInterval = 10 * time.Second

func parseInterval(spec string) (Schedule, error) {
	d, err := time.ParseDuration(spec)
	if err != nil {
		return nil, fmt.Errorf("loop: %q is not an interval (use 30s, 5m, 2h) or a cron expression", spec)
	}
	if d < minInterval {
		return nil, fmt.Errorf(
			"loop: interval %s is below the %s minimum: a loop that fires faster than a turn completes wastes the budget",
			d,
			minInterval,
		)
	}
	return interval{d: d}, nil
}

// cronExpr is minute, hour, day-of-month, month, day-of-week. Each field is
// "*", a number, a comma list, a range, or a step ("*/15").
type cronExpr struct {
	minute  fieldSet
	hour    fieldSet
	dom     fieldSet
	month   fieldSet
	dow     fieldSet
	literal string
}

// fieldSet is the set of values one cron field matches.
type fieldSet struct {
	any    bool
	values map[int]bool
}

func (f fieldSet) matches(v int) bool {
	if f.any {
		return true
	}
	return f.values[v]
}

func (c cronExpr) String() string { return c.literal }

// Next scans forward a minute at a time. A year is the bound: a schedule that
// matches nothing within a year (February 30th) never matches, and returning
// the zero time is better than scanning forever.
func (c cronExpr) Next(now time.Time) time.Time {
	t := now.Truncate(time.Minute).Add(time.Minute)
	limit := now.Add(366 * 24 * time.Hour)
	for !t.After(limit) {
		if c.matchesTime(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

// matchesTime reports whether t satisfies every field.
//
// Day-of-month and day-of-week are ORed when both are restricted, which is
// what cron does: "1 * 1" means the first of the month and every Monday, not
// Mondays that fall on the first.
func (c cronExpr) matchesTime(t time.Time) bool {
	if !c.minute.matches(t.Minute()) || !c.hour.matches(t.Hour()) || !c.month.matches(int(t.Month())) {
		return false
	}
	dom, dow := c.dom.matches(t.Day()), c.dow.matches(int(t.Weekday()))
	if c.dom.any || c.dow.any {
		return dom && dow
	}
	return dom || dow
}

// cronField bounds one field and names it for errors.
type cronField struct {
	name     string
	min, max int
}

var cronFields = []cronField{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day-of-month", 1, 31},
	{"month", 1, 12},
	{"day-of-week", 0, 6},
}

func parseCron(spec string) (Schedule, error) {
	parts := strings.Fields(spec)
	if len(parts) != len(cronFields) {
		return nil, fmt.Errorf(
			"loop: cron needs 5 fields (minute hour day month weekday), got %d in %q",
			len(parts),
			spec,
		)
	}
	sets := make([]fieldSet, len(parts))
	for i, part := range parts {
		set, err := parseField(part, cronFields[i])
		if err != nil {
			return nil, err
		}
		sets[i] = set
	}
	return cronExpr{
		minute: sets[0], hour: sets[1], dom: sets[2], month: sets[3], dow: sets[4],
		literal: strings.Join(parts, " "),
	}, nil
}

// parseField reads one field: "*", "*/15", "1-5", "0,30", or a number.
func parseField(part string, f cronField) (fieldSet, error) {
	if part == "*" {
		return fieldSet{any: true}, nil
	}
	out := fieldSet{values: map[int]bool{}}
	for chunk := range strings.SplitSeq(part, ",") {
		if err := addChunk(out.values, chunk, f); err != nil {
			return fieldSet{}, err
		}
	}
	if len(out.values) == 0 {
		return fieldSet{}, fmt.Errorf("loop: %s field %q matches nothing", f.name, part)
	}
	return out, nil
}

// addChunk adds one comma-separated piece: a step, a range, or a number.
func addChunk(into map[int]bool, chunk string, f cronField) error {
	step := 1
	if base, rawStep, ok := strings.Cut(chunk, "/"); ok {
		n, err := strconv.Atoi(rawStep)
		if err != nil || n <= 0 {
			return fmt.Errorf("loop: %s step %q must be a positive number", f.name, rawStep)
		}
		step, chunk = n, base
	}

	lo, hi := f.min, f.max
	if chunk != "*" {
		rawLo, rawHi, isRange := strings.Cut(chunk, "-")
		var err error
		if lo, err = strconv.Atoi(strings.TrimSpace(rawLo)); err != nil {
			return fmt.Errorf("loop: %s value %q is not a number", f.name, rawLo)
		}
		hi = lo
		if isRange {
			if hi, err = strconv.Atoi(strings.TrimSpace(rawHi)); err != nil {
				return fmt.Errorf("loop: %s value %q is not a number", f.name, rawHi)
			}
		}
	}
	if lo < f.min || hi > f.max || lo > hi {
		return fmt.Errorf("loop: %s %q is outside %d-%d", f.name, chunk, f.min, f.max)
	}
	for v := lo; v <= hi; v += step {
		into[v] = true
	}
	return nil
}
