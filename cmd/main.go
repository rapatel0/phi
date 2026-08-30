package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/app"
	"github.com/rapatel0/alpha/internal/project"
	"github.com/rapatel0/alpha/internal/tui/commands"
	"github.com/rapatel0/alpha/internal/tui/controller"
	"github.com/rapatel0/alpha/internal/tui/editor"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "run":
			os.Exit(runCmd(os.Args[2:]))
		case "sessions":
			os.Exit(sessionsCmd(os.Args[2:]))
		case "mcp":
			os.Exit(mcpCmd(os.Args[2:]))
		case "config":
			os.Exit(configCmd(os.Args[2:]))
		case "update":
			os.Exit(updateCmd(os.Args[2:]))
		case "login":
			os.Exit(loginCmd(os.Args[2:]))
		case "profile":
			os.Exit(profileCmd(os.Args[2:]))
		case "keys":
			os.Exit(keysCmd(os.Args[2:]))
		case "tui":
			os.Exit(runTUIExit(runTUI()))
		case "-h", "--help", "help":
			printMainUsage(os.Stdout)
			return
		default:
			fmt.Fprintf(os.Stderr, "alpha: unknown command %q (try 'alpha run --help' or 'alpha tui')\n", os.Args[1])
			os.Exit(ExitUsage)
		}
	}
	os.Exit(runTUIExit(runTUI()))
}

// runTUI starts the interactive terminal UI (default, unchanged behavior).
// It returns an error so main() can pick the process exit code.
func runTUI() error {
	proj := project.GetDefaultProject()
	if err := proj.LoadConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "alpha:", err)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Configure a model first, then restart:")
		fmt.Fprintln(os.Stderr, "  alpha config")
		fmt.Fprintln(os.Stderr, "or set ALPHA_MODEL and ALPHA_API_KEY.")
		return &exitError{code: ExitUsage, err: err}
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
		fmt.Fprintln(os.Stderr, "alpha: terminal UI:", err)
		return &exitError{code: ExitError, err: err}
	}
	shutdown := closeTerminal(vx)
	defer shutdown()
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigc
		shutdown()
	}()

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha: getwd:", err)
		return &exitError{code: ExitError, err: err}
	}
	th := components.DefaultTheme()
	models := proj.Config().AllModels()
	modelNames := make([]string, 0, len(models))
	for _, m := range models {
		modelNames = append(modelNames, m.Name)
	}

	application := app.NewApp(vx)
	application.Anim = true
	application.AfterQuery = func() { afterTerminalQuery(vx) }

	redraw := controller.NewRedrawRelay()
	bus := controller.NewBus(redraw.Fire)
	ctrl, err := controller.NewController(bus, proj, cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha:", err)
		return &exitError{code: ExitError, err: err}
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
		fmt.Fprintln(os.Stderr, "alpha:", err)
		return &exitError{code: ExitError, err: err}
	}
	return nil
}

// exitError carries a process exit code so helpers can fail without calling os.Exit.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func (e *exitError) Unwrap() error { return e.err }

// runTUIExit maps the error returned by runTUI to the process exit code.
func runTUIExit(err error) int {
	if err == nil {
		return ExitOK
	}
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code
	}
	return ExitError
}

func printMainUsage(w *os.File) {
	fmt.Fprintf(w, `usage: alpha [COMMAND]

  alpha                start the interactive TUI
  alpha tui            start the interactive TUI
  alpha config         open the HTML config editor (local web server)
  alpha update         install the latest release (see 'alpha update --help')
  alpha run -p "..."   run one agent loop headlessly (see 'alpha run --help')
  alpha login …        Claude Pro/Max or ChatGPT Codex OAuth (see 'alpha login --help')
  alpha profile …      switch between named credential sets (see 'alpha profile --help')
  alpha sessions list  list persisted sessions for this directory
  alpha mcp …          manage MCP servers (see 'alpha mcp --help')
  alpha keys           show how this terminal reports key presses
`)
}
