package cli

import (
	"bytes"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCmd builds a command with one flag of each kind and a Run that records
// the parsed values.
func testCmd(t *testing.T) (*Command, *options) {
	t.Helper()
	o := &options{}
	c := &Command{Name: "phi", Desc: "test tool"}
	String(c, "prompt", "p", "prompt to run (required)", &o.prompt)
	Bool(c, "jsonl", "", "emit JSONL events", &o.jsonl)
	Int(c, "max-rounds", "", "cap tool rounds", &o.maxRounds)
	Duration(c, "timeout", "", "stop after a duration", &o.timeout)
	c.Run = func(args []string) error {
		o.pos = args
		return nil
	}
	return c, o
}

type options struct {
	prompt    string
	jsonl     bool
	maxRounds int
	timeout   time.Duration
	pos       []string
}

func TestParseFlagsForms(t *testing.T) {
	c, o := testCmd(t)
	pos, err := c.Parse([]string{
		"-p", "do the thing",
		"--jsonl",
		"--max-rounds", "10",
		"--timeout=10m",
		"tail",
	})
	require.NoError(t, err)
	assert.Equal(t, "do the thing", o.prompt)
	assert.True(t, o.jsonl)
	assert.Equal(t, 10, o.maxRounds)
	assert.Equal(t, 10*time.Minute, o.timeout)
	assert.Equal(t, []string{"tail"}, pos)
}

func TestParseEqualsAndShortForms(t *testing.T) {
	c, o := testCmd(t)
	_, err := c.Parse([]string{"--prompt=hi", "-p=bye", "--max-rounds=5"})
	require.NoError(t, err)
	assert.Equal(t, "bye", o.prompt)
	assert.Equal(t, 5, o.maxRounds)
}

func TestParseDoubleDashStopsFlags(t *testing.T) {
	c, o := testCmd(t)
	pos, err := c.Parse([]string{"--jsonl", "--", "--prompt", "literal"})
	require.NoError(t, err)
	assert.True(t, o.jsonl)
	assert.Equal(t, []string{"--prompt", "literal"}, pos)
}

func TestParseNegativeNumberIsPositional(t *testing.T) {
	c, _ := testCmd(t)
	pos, err := c.Parse([]string{"-1s"})
	require.NoError(t, err)
	assert.Equal(t, []string{"-1s"}, pos)
}

func TestParseErrors(t *testing.T) {
	cases := [][]string{
		{"--prompt"},            // missing value
		{"--max-rounds", "abc"}, // non-integer
		{"--bogus", "x"},        // unknown flag
		{"-z"},                  // unknown short flag
	}
	for _, args := range cases {
		c, _ := testCmd(t)
		_, err := c.Parse(args)
		assert.Error(t, err, "args %v should error", args)
	}
}

func TestVarCustomParser(t *testing.T) {
	c := &Command{Name: "phi"}
	n := 0
	Var(c, "max-rounds", "", "N", "cap tool rounds", &n, func(s string) (int, error) {
		v, err := strconv.Atoi(s)
		if err != nil || v <= 0 {
			return 0, errors.New("must be a positive integer")
		}
		return v, nil
	})
	_, err := c.Parse([]string{"--max-rounds", "0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a positive integer")
}

func TestDispatchSubcommandsAndAliases(t *testing.T) {
	var ran []string
	root := &Command{Name: "phi"}
	run := &Command{Name: "run", Desc: "run headless"}
	run.Run = func(_ []string) error { ran = append(ran, "run"); return nil }
	list := &Command{Name: "list", Aliases: []string{"ls"}, Desc: "list things"}
	list.Run = func(_ []string) error { ran = append(ran, "list"); return nil }
	root.Add(run, list)

	require.NoError(t, root.Dispatch([]string{"run"}))
	require.NoError(t, root.Dispatch([]string{"ls"}))
	assert.Equal(t, []string{"run", "list"}, ran)
}

func TestDispatchBareRunsDefault(t *testing.T) {
	var ran bool
	root := &Command{Name: "phi", Run: func(_ []string) error { ran = true; return nil }}
	require.NoError(t, root.Dispatch(nil))
	assert.True(t, ran)
}

func TestDispatchUnknownCommand(t *testing.T) {
	var errBuf bytes.Buffer
	c := &Command{Name: "phi"}
	c.Err = &errBuf
	err := c.Dispatch([]string{"bogus"})
	require.Error(t, err)
	var ue *UsageError
	require.ErrorAs(t, err, &ue)
	assert.Contains(t, errBuf.String(), "phi: unknown command \"bogus\"")
	assert.Contains(t, errBuf.String(), "usage: phi")
}

func TestDispatchUnknownSubcommandOfGroup(t *testing.T) {
	var errBuf bytes.Buffer
	c := &Command{Name: "phi"}
	sub := &Command{Name: "mcp"}
	c.Add(sub)
	c.Err = &errBuf
	err := c.Dispatch([]string{"mcp", "nope"})
	require.Error(t, err)
	var ue *UsageError
	require.ErrorAs(t, err, &ue)
	assert.Contains(t, errBuf.String(), "phi mcp: unknown command \"nope\"")
}

func TestDispatchHelpToStdout(t *testing.T) {
	var out bytes.Buffer
	c := &Command{Name: "phi", Desc: "test tool"}
	String(c, "prompt", "p", "prompt to run (required)", nil)
	c.Out = &out
	require.NoError(t, c.Dispatch([]string{"--help"}))
	require.NoError(t, c.Dispatch([]string{"-h"}))
	require.NoError(t, c.Dispatch([]string{"help"}))
	for range 3 {
		assert.Contains(t, out.String(), "usage: phi")
		assert.Contains(t, out.String(), "--prompt")
		assert.Contains(t, out.String(), "-p, --prompt STRING")
	}
}

func TestRunUsageErrorPrintsHelp(t *testing.T) {
	var out, errBuf bytes.Buffer
	c := &Command{Name: "phi", Desc: "test tool"}
	c.Out = &out
	c.Err = &errBuf
	c.Run = func(_ []string) error { return c.usagef("prompt is required (-p \"...\")") }
	err := c.Dispatch(nil)
	require.Error(t, err)
	assert.Contains(t, errBuf.String(), "phi: prompt is required (-p \"...\")")
	assert.Contains(t, errBuf.String(), "usage: phi")
}

func TestRunErrorWrapsWithCommandPath(t *testing.T) {
	var errBuf bytes.Buffer
	c := &Command{Name: "phi"}
	sub := &Command{Name: "mcp"}
	sub.Run = func(_ []string) error { return errors.New("boom") }
	c.Add(sub)
	c.Err = &errBuf
	err := c.Dispatch([]string{"mcp"})
	require.Error(t, err)
	var re *RunError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "phi mcp", re.Cmd.fullName())
	assert.Equal(t, "boom", err.Error())
}

func TestHelpShowsDefault(t *testing.T) {
	var out bytes.Buffer
	c := &Command{Name: "phi"}
	n := 64
	Int(c, "max-rounds", "", "cap tool rounds", &n)
	c.Out = &out
	require.NoError(t, c.Dispatch([]string{"--help"}))
	assert.Contains(t, out.String(), "(default 64)")
}

// silentErr is an error that has already been reported to the user; the
// framework must not wrap it or print it again.
type silentErr struct{ error }

func (silentErr) silent() bool { return true }

func TestSilentErrorPassesThrough(t *testing.T) {
	c := &Command{Name: "phi", Run: func(_ []string) error { return silentErr{errors.New("already printed")} }}
	err := c.Dispatch(nil)
	require.Error(t, err)
	var re *RunError
	require.NotErrorAs(t, err, &re, "silent errors must not be wrapped")
}

func TestNegativeNumberValueFlag(t *testing.T) {
	c, o := testCmd(t)
	_, err := c.Parse([]string{"--timeout", "-1s"})
	require.NoError(t, err) // -1s is consumed as the flag value, not a flag
	assert.Equal(t, time.Duration(-1)*time.Second, o.timeout)
}
