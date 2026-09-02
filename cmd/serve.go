package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/rapatel0/alpha/internal/project"
	"github.com/rapatel0/alpha/internal/serve"
)

func serveCmd(args []string) int {
	addr := serve.DefaultAddr
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			printServeUsage(os.Stdout)
			return ExitOK
		case a == "--addr" && i+1 < len(args):
			i++
			addr = strings.TrimSpace(args[i])
		case strings.HasPrefix(a, "--addr="):
			addr = strings.TrimSpace(strings.TrimPrefix(a, "--addr="))
		default:
			fmt.Fprintf(os.Stderr, "alpha serve: unknown argument %q\n", a)
			printServeUsage(os.Stderr)
			return ExitUsage
		}
	}

	proj := project.GetDefaultProject()
	if err := proj.LoadConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "alpha serve:", err)
		return ExitUsage
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha serve:", err)
		return ExitError
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv, err := serve.Listen(ctx, proj, serve.Options{Addr: addr, Cwd: cwd})
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha serve:", err)
		return ExitError
	}
	defer func() { _ = srv.Close() }()

	fmt.Fprintf(os.Stderr, "alpha serve: http://%s  (loopback only)\n", srv.Addr())
	fmt.Fprintln(os.Stderr, "GET  /health")
	fmt.Fprintln(os.Stderr, "GET  /v1/session")
	fmt.Fprintln(os.Stderr, "POST /v1/prompt   {\"text\":\"...\"}")
	fmt.Fprintln(os.Stderr, "GET  /v1/events   text/event-stream")
	<-ctx.Done()
	return ExitOK
}

func printServeUsage(w *os.File) {
	fmt.Fprintf(w, `usage: alpha serve [--addr 127.0.0.1:38765]

Loopback HTTP control plane over the same Controller as the TUI.
Refuses non-loopback binds. Default TUI is unchanged.

  GET  /health
  GET  /v1/session
  POST /v1/prompt   JSON {"text":"..."}
  GET  /v1/events   SSE of bus messages
`)
}
