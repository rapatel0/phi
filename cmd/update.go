package main

import (
	"context"
	"os"
	"time"

	"github.com/pulseaiclub/phi/internal/cli"
	"github.com/pulseaiclub/phi/internal/util/update"
	"github.com/pulseaiclub/phi/internal/version"
)

// updateCommand installs the latest GitHub release, or checks for one with
// --check. Environment notes live in the Long help block.
func updateCommand() *cli.Command {
	c := &cli.Command{
		Name: "update",
		Desc: "install the latest release",
		Long: "Environment:\n" +
			"  PHI_SKIP_VERSION_CHECK  skip startup version checks in the TUI\n" +
			"  PHI_OFFLINE             same as PHI_SKIP_VERSION_CHECK\n" +
			"  GITHUB_TOKEN            optional; raises GitHub API rate limits",
	}
	check := cli.Bool(c, "check", "", "query the latest release without installing", new(bool))
	c.Run = func(args []string) error {
		if len(args) > 0 {
			return c.Usagef("unexpected argument %q", args[0])
		}
		if *check {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return update.CheckOnly(ctx, version.Version)
		}
		ctx, cancel := context.WithTimeout(context.Background(), update.DefaultInstallTimeout)
		defer cancel()
		return update.Install(ctx, update.InstallOptions{
			Current: version.Version,
			Stdout:  os.Stdout,
		})
	}
	return c
}
