// Package cli is a minimal generic command-line framework: a command tree
// with auto-generated help and generic flag binding. It has zero
// dependencies and deliberately stays small — no lifecycle, no middleware,
// no configuration files. A command is a struct, flags are typed variables
// bound in place, and dispatch is one method call.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Command is one node in a command tree. A command either runs (Run != nil),
// groups subcommands (Sub != nil), or both (root commands that default to a
// runnable action, e.g. `phi` with no arguments starting the TUI).
type Command struct {
	Name    string // shown in usage, e.g. "run"
	Desc    string // one-line summary shown in lists and help
	ArgsUse string // positional hint for the usage line, e.g. "<name> -- <cmd>"
	Long    string // extra help paragraphs appended after the flags
	Aliases []string
	Sub     []*Command
	Run     func(args []string) error

	// Out/Err receive help and error output. They default to stdout/stderr
	// and are also the place to inject a writer in tests.
	Out io.Writer
	Err io.Writer

	parent *Command
	flags  []*flagDef
}

type flagDef struct {
	name       string
	short      string
	meta       string
	usage      string
	def        string // default value shown in help; empty when not meaningful
	takesValue bool
	set        func(value string) error
}

// Add registers subcommands and wires their parent for usage paths.
func (c *Command) Add(sub ...*Command) {
	for _, s := range sub {
		s.parent = c
		c.Sub = append(c.Sub, s)
	}
}

// Var binds a generic flag of type T. dst is the variable the parsed value
// lands in (allocated when nil); parse converts the raw string and reports
// invalid values. The returned pointer is dst, so callers may read the value
// immediately after dispatch.
func Var[T any](c *Command, name, short, meta, usage string, dst *T, parse func(string) (T, error)) *T {
	if dst == nil {
		dst = new(T)
	}
	fd := &flagDef{
		name:       name,
		short:      short,
		meta:       meta,
		usage:      usage,
		def:        defaultString(*dst),
		takesValue: true,
		set: func(v string) error {
			t, err := parse(v)
			if err != nil {
				return fmt.Errorf("invalid value %q for --%s: %w", v, name, err)
			}
			*dst = t
			return nil
		},
	}
	c.flags = append(c.flags, fd)
	return dst
}

// Bool binds a presence flag; the value is set when the flag appears.
func Bool(c *Command, name, short, usage string, dst *bool) *bool {
	if dst == nil {
		dst = new(bool)
	}
	c.flags = append(c.flags, &flagDef{
		name:  name,
		short: short,
		usage: usage,
		def:   defaultString(*dst),
		set: func(string) error {
			*dst = true
			return nil
		},
	})
	return dst
}

// String binds a string flag.
func String(c *Command, name, short, usage string, dst *string) *string {
	return Var(c, name, short, "STRING", usage, dst, func(s string) (string, error) { return s, nil })
}

// Int binds an integer flag.
func Int(c *Command, name, short, usage string, dst *int) *int {
	return Var(c, name, short, "N", usage, dst, strconv.Atoi)
}

// Duration binds a time.Duration flag parsed with time.ParseDuration.
func Duration(c *Command, name, short, usage string, dst *time.Duration) *time.Duration {
	return Var(c, name, short, "DURATION", usage, dst, time.ParseDuration)
}

// Parse consumes flags from args and returns the remaining positional
// arguments. It supports --name, --name=value, -n and -n=value, value flags
// taking the next argument, "--" as end-of-flags, and single-character
// shorthand. A usage error is returned for unknown flags and missing values.
func (c *Command) Parse(args []string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		fd, val, hasVal, err := c.matchFlag(arg)
		if err != nil {
			return nil, err
		}
		if fd == nil {
			pos = append(pos, arg)
			continue
		}
		if !fd.takesValue {
			if err := fd.set(""); err != nil {
				return nil, err
			}
			continue
		}
		if !hasVal {
			i++
			if i >= len(args) {
				return nil, c.usagef("flag --%s requires a value", fd.name)
			}
			val = args[i] //nolint:gosec // G602: guarded by i >= len(args) above
		}
		if err := fd.set(val); err != nil {
			return nil, err
		}
	}
	return pos, nil
}

// matchFlag resolves a single argument against the registered flags.
// fd==nil, err==nil means the argument is positional (including things like
// "-1s" that look like negative numbers rather than flags).
func (c *Command) matchFlag(arg string) (fd *flagDef, val string, hasVal bool, err error) {
	if arg == "--" || arg == "" || arg == "-" || isNegativeNumber(arg) {
		return nil, "", false, nil
	}
	long := strings.HasPrefix(arg, "--")
	if !long && !strings.HasPrefix(arg, "-") {
		return nil, "", false, nil
	}
	body := strings.TrimPrefix(strings.TrimPrefix(arg, "--"), "-")
	name := body
	if n, v, ok := strings.Cut(body, "="); ok {
		name, val, hasVal = n, v, true
	}
	if fd := c.findFlag(name, long); fd != nil {
		return fd, val, hasVal, nil
	}
	return nil, "", false, c.usagef("unknown flag %q", arg)
}

func (c *Command) findFlag(name string, long bool) *flagDef {
	for _, fd := range c.flags {
		if (long && fd.name == name) || (!long && fd.short == name) {
			return fd
		}
	}
	return nil
}

func isNegativeNumber(s string) bool {
	return len(s) > 1 && s[0] == '-' && s[1] >= '0' && s[1] <= '9'
}

// Dispatch runs the command tree against args. It selects a subcommand when
// the first argument names one, parses the command's own flags, and invokes
// Run with the remaining positionals. Help ("-h", "--help", "help") prints
// to Out and succeeds; usage errors print the message and help to Err.
func (c *Command) Dispatch(args []string) error {
	c.Out = c.effOut()
	c.Err = c.effErr()
	if len(args) == 0 {
		if c.Run != nil {
			return c.run(c.Run, nil)
		}
		fmt.Fprint(c.Out, c.help())
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(c.Out, c.help())
		return nil
	}
	if sub := c.matchSub(args[0]); sub != nil {
		return sub.Dispatch(args[1:])
	}
	if !strings.HasPrefix(args[0], "-") && c.Run == nil {
		return c.report(c.usagef("unknown command %q", args[0]))
	}
	pos, err := c.Parse(args)
	if err != nil {
		return c.report(err)
	}
	if c.Run == nil {
		fmt.Fprint(c.Out, c.help())
		return nil
	}
	return c.run(c.Run, pos)
}

func (c *Command) run(fn func([]string) error, args []string) error {
	if err := fn(args); err != nil {
		var ue *UsageError
		if errors.As(err, &ue) {
			return c.report(ue)
		}
		var silent interface{ silent() bool }
		if errors.As(err, &silent) && silent.silent() {
			return err
		}
		return &RunError{Cmd: c, Err: err}
	}
	return nil
}

func (c *Command) matchSub(name string) *Command {
	for _, s := range c.Sub {
		if s.Name == name {
			return s
		}
		if slices.Contains(s.Aliases, name) {
			return s
		}
	}
	return nil
}

// UsageError is a user-facing invocation mistake: unknown flag, missing
// value, unknown subcommand, bad positional. Dispatch reports it with help.
type UsageError struct {
	Cmd *Command
	Msg string
}

func (e *UsageError) Error() string { return e.Msg }

// RunError wraps a non-usage error from a command's Run so callers can print
// the failing command path ("phi mcp add: ...").
type RunError struct {
	Cmd *Command
	Err error
}

func (e *RunError) Error() string { return e.Err.Error() }
func (e *RunError) Unwrap() error { return e.Err }

func (c *Command) usagef(format string, a ...any) error {
	return &UsageError{Cmd: c, Msg: fmt.Sprintf(format, a...)}
}

// report prints a usage error (message + help) to Err and returns it.
func (c *Command) report(err error) error {
	var ue *UsageError
	if !errors.As(err, &ue) {
		return err
	}
	fmt.Fprintf(c.Err, "%s: %s\n\n", ue.Cmd.fullName(), ue.Msg)
	fmt.Fprint(c.Err, ue.Cmd.help())
	return ue
}

// effOut/effErr resolve the output writers, falling back to the parent's so
// subcommands inherit injected writers and tests can capture nested output.
func (c *Command) effOut() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	if c.parent != nil {
		return c.parent.effOut()
	}
	return os.Stdout
}

func (c *Command) effErr() io.Writer {
	if c.Err != nil {
		return c.Err
	}
	if c.parent != nil {
		return c.parent.effErr()
	}
	return os.Stderr
}

func (c *Command) fullName() string {
	if c.parent == nil {
		return c.Name
	}
	return c.parent.fullName() + " " + c.Name
}

// FullName is the full command path, e.g. "phi mcp add", for error prefixes.
func (c *Command) FullName() string { return c.fullName() }

// Usagef builds a usage error scoped to c; Dispatch prints it with help.
func (c *Command) Usagef(format string, a ...any) error { return c.usagef(format, a...) }

// help renders the usage block: invocation line, description, subcommands,
// flags, and the Long tail.
func (c *Command) help() string {
	var b strings.Builder
	fmt.Fprintf(&b, "usage: %s", c.fullName())
	if c.ArgsUse != "" {
		b.WriteString(" " + c.ArgsUse)
	}
	if len(c.flags) > 0 {
		b.WriteString(" [flags]")
	}
	b.WriteString("\n")
	if c.Desc != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Desc)
	}
	if len(c.Sub) > 0 {
		b.WriteString("\ncommands:\n")
		for _, s := range c.Sub {
			names := s.Name
			if len(s.Aliases) > 0 {
				names += " (" + strings.Join(s.Aliases, ", ") + ")"
			}
			fmt.Fprintf(&b, "  %-22s %s\n", names, s.Desc)
		}
	}
	if len(c.flags) > 0 {
		b.WriteString("\nflags:\n")
		width := 0
		for _, fd := range c.flags {
			if w := len(flagLabel(fd)); w > width {
				width = w
			}
		}
		for _, fd := range c.flags {
			line := "  " + flagLabel(fd)
			if d := fd.def; d != "" {
				fmt.Fprintf(&b, "%-*s  %s (default %s)\n", width+2, line, fd.usage, d)
			} else {
				fmt.Fprintf(&b, "%-*s  %s\n", width+2, line, fd.usage)
			}
		}
	}
	if c.Long != "" {
		b.WriteString("\n" + strings.TrimSpace(c.Long) + "\n")
	}
	return b.String()
}

func flagLabel(fd *flagDef) string {
	var b strings.Builder
	b.WriteString("    ")
	if fd.short != "" {
		b.WriteString("-" + fd.short + ", ")
	} else {
		b.WriteString("    ")
	}
	b.WriteString("--" + fd.name)
	if fd.takesValue {
		b.WriteString(" " + fd.meta)
	}
	return b.String()
}

func defaultString(v any) string {
	switch v := v.(type) {
	case bool:
		if v {
			return "true"
		}
		return ""
	case string:
		return v
	case time.Duration:
		if v == 0 {
			return ""
		}
		return v.String()
	case int:
		if v == 0 {
			return ""
		}
		return strconv.Itoa(v)
	default:
		return fmt.Sprint(v)
	}
}
