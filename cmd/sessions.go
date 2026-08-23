package main

import (
	"fmt"
	"os"

	"github.com/pulseaiclub/phi/internal/cli"
	"github.com/pulseaiclub/phi/internal/project"
	"github.com/pulseaiclub/phi/internal/session"
)

// sessionsCommand lists persisted sessions for the current directory.
// Bare `phi sessions` behaves like `phi sessions list`.
func sessionsCommand() *cli.Command {
	c := &cli.Command{
		Name: "sessions",
		Desc: "list persisted sessions for this directory",
	}
	run := func(args []string) error {
		if len(args) > 0 {
			return c.Usagef("unexpected argument %q", args[0])
		}
		proj := project.GetDefaultProject()
		dir := proj.SessionDir()
		list, err := session.ListSessions(dir)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Fprintf(os.Stderr, "no sessions in %s\n", dir)
			return nil
		}
		for _, s := range list {
			fmt.Printf("%s  %s  %s\n", s.ID, s.Mtime.Format("2006-01-02 15:04:05"), s.Preview)
		}
		return nil
	}
	c.Run = run
	c.Add(&cli.Command{Name: "list", Desc: "list persisted sessions for this directory", Run: run})
	return c
}
