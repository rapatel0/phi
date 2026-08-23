package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/pulseaiclub/phi/internal/cli"
)

// exitError carries a process exit code so helpers can fail without calling
// os.Exit. silent marks errors that were already reported to the user (e.g.
// runLoop prints its own diagnostics) so main does not print them twice.
type exitError struct {
	code   int
	err    error
	silent bool
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// exitCode maps any dispatch error to the phi exit-code contract:
// usage errors are 3, command-specific codes win, everything else is 1.
func exitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	var ue *cli.UsageError
	if errors.As(err, &ue) {
		return ExitUsage
	}
	return ExitError
}

// printCLIError writes the failure to stderr. Usage errors were already
// printed with help by the framework; silent errors were already printed by
// the command itself; everything else gets the command path as prefix.
func printCLIError(err error) {
	var re *cli.RunError
	if errors.As(err, &re) {
		fmt.Fprintf(os.Stderr, "%s: %s\n", re.Cmd.FullName(), re.Err)
		return
	}
	var ue *cli.UsageError
	if errors.As(err, &ue) {
		return
	}
	var ee *exitError
	if errors.As(err, &ee) {
		if !ee.silent {
			fmt.Fprintln(os.Stderr, "phi:", ee.err)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "phi:", err)
}
