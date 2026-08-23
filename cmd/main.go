package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/pulseaiclub/xui"

	"github.com/pulseaiclub/phi/internal/cli"
	"github.com/pulseaiclub/phi/internal/components"
	"github.com/pulseaiclub/phi/internal/components/app"
	"github.com/pulseaiclub/phi/internal/project"
	"github.com/pulseaiclub/phi/internal/tui/commands"
	"github.com/pulseaiclub/phi/internal/tui/controller"
	"github.com/pulseaiclub/phi/internal/tui/editor"
)

func main() {
	if err := rootCommand().Dispatch(os.Args[1:]); err != nil {
		printCLIError(err)
		os.Exit(exitCode(err))
	}
}

// rootCommand builds the phi command tree. Bare `phi` (or `phi tui`) starts
// the interactive TUI; subcommands cover the headless entrypoints.
func rootCommand() *cli.Command {
	root := &cli.Command{
		Name: "phi",
		Desc: "minimal Go terminal coding agent",
		Long: "Bare `phi` starts the interactive TUI.",
	}
	root.Add(
		newRunCommand(&runOptions{}),
		sessionsCommand(),
		mcpCommand(),
		configCommand(),
		updateCommand(),
		tuiCommand(),
	)
	root.Run = func(args []string) error {
		if len(args) > 0 {
			return root.Usagef("unknown command %q (try 'phi run --help' or 'phi tui')", args[0])
		}
		return runTUI()
	}
	return root
}

func tuiCommand() *cli.Command {
	return &cli.Command{
		Name: "tui",
		Desc: "start the interactive TUI",
		Run: func(args []string) error {
			if len(args) > 0 {
				return errors.New("unexpected argument")
			}
			return runTUI()
		},
	}
}

// runTUI starts the interactive terminal UI (default, unchanged behavior).
// It returns an error so main() can pick the process exit code. All
// diagnostics are printed here, hence the silent exitError.
func runTUI() error {
	proj := project.GetDefaultProject()
	if err := proj.LoadConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "phi:", err)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Configure a model first, then restart:")
		fmt.Fprintln(os.Stderr, "  phi config")
		fmt.Fprintln(os.Stderr, "or set PHI_MODEL and PHI_API_KEY.")
		return &exitError{code: ExitUsage, err: err, silent: true}
	}
	cfg := proj.Config().Model()

	// Download fd/rg in the background so a cold install does not block the
	// first TUI frame. Failures stay non-fatal (tools fall back to PATH).
	go func() {
		if err := EnsureSearchTools(context.Background(), proj); err != nil {
			fmt.Fprintln(os.Stderr, "warning: could not install search tools:", err)
		}
	}()

	vx, err := xui.New(xui.Options{Mouse: true, BracketedPaste: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "phi: terminal UI:", err)
		return &exitError{code: ExitError, err: err, silent: true}
	}
	defer func(vx *xui.XUI) {
		if err := vx.Close(); err != nil {
			panic(err)
		}
	}(vx)

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "phi: getwd:", err)
		return &exitError{code: ExitError, err: err, silent: true}
	}
	th := components.DefaultTheme()
	models := proj.Config().AllModels()
	modelNames := make([]string, 0, len(models))
	for _, m := range models {
		modelNames = append(modelNames, m.Name)
	}

	application := app.NewApp(vx)
	application.Anim = true

	redraw := controller.NewRedrawRelay()
	bus := controller.NewBus(redraw.Fire)
	ctrl, err := controller.NewController(bus, proj, cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "phi:", err)
		return &exitError{code: ExitError, err: err, silent: true}
	}
	cmds := commands.NewBuiltinRegistry()
	ui := editor.NewEditor(
		application,
		bus,
		ctrl,
		cmds,
		vx,
		th,
		cwd,
		cfg.Name,
		cfg.SkillPath,
		cfg.ContextWindow,
		modelNames,
	)
	redraw.Bind(ui.RequestRedraw)
	ui.StartUpdateCheck(proj.Global().Root())
	ui.StartBranchWatch()
	if err := application.Run(ui); err != nil {
		fmt.Fprintln(os.Stderr, "phi:", err)
		return &exitError{code: ExitError, err: err, silent: true}
	}
	return nil
}
