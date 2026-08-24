package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rapatel0/alpha/internal/util/update"
	"github.com/rapatel0/alpha/internal/version"
)

func updateCmd(args []string) int {
	checkOnly := false
	for _, a := range args {
		switch a {
		case "--check":
			checkOnly = true
		case "-h", "--help", "help":
			printUpdateUsage(os.Stdout)
			return ExitOK
		default:
			fmt.Fprintf(os.Stderr, "alpha update: unknown flag %q\n", a)
			printUpdateUsage(os.Stderr)
			return ExitUsage
		}
	}

	if checkOnly {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := update.CheckOnly(ctx, version.Version); err != nil {
			fmt.Fprintln(os.Stderr, "alpha update:", err)
			return ExitError
		}
		return ExitOK
	}

	ctx, cancel := context.WithTimeout(context.Background(), update.DefaultInstallTimeout)
	defer cancel()
	if err := update.Install(ctx, update.InstallOptions{
		Current: version.Version,
		Stdout:  os.Stdout,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "alpha update:", err)
		return ExitError
	}
	return ExitOK
}

func printUpdateUsage(w *os.File) {
	fmt.Fprintf(w, `usage: alpha update [--check]

  alpha update         download and install the latest GitHub release
  alpha update --check query the latest release without installing

Environment:
  ALPHA_SKIP_VERSION_CHECK  skip startup version checks in the TUI
  ALPHA_OFFLINE             same as ALPHA_SKIP_VERSION_CHECK
  GITHUB_TOKEN            optional; raises GitHub API rate limits
`)
}
