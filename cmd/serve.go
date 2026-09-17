package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/rapatel0/alpha/internal/project"
	"github.com/rapatel0/alpha/internal/serve"
)

func serveCmd(args []string) int {
	addr := ""
	tsnetMode := false
	tsnetHostname := serve.DefaultTSNetName
	tsnetStateDir := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			printServeUsage(os.Stdout)
			return ExitOK
		case a == "--tsnet":
			tsnetMode = true
		case a == "--tsnet-hostname" && i+1 < len(args):
			i++
			tsnetHostname = strings.TrimSpace(args[i])
		case strings.HasPrefix(a, "--tsnet-hostname="):
			tsnetHostname = strings.TrimSpace(strings.TrimPrefix(a, "--tsnet-hostname="))
		case a == "--tsnet-state-dir" && i+1 < len(args):
			i++
			tsnetStateDir = strings.TrimSpace(args[i])
		case strings.HasPrefix(a, "--tsnet-state-dir="):
			tsnetStateDir = strings.TrimSpace(strings.TrimPrefix(a, "--tsnet-state-dir="))
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

	if tsnetMode && tsnetStateDir == "" {
		tsnetStateDir = filepath.Join(proj.Global().Root(), "tsnet")
	}
	srv, err := serve.Listen(ctx, proj, serve.Options{
		Addr:          addr,
		Cwd:           cwd,
		TSNet:         tsnetMode,
		TSNetHostname: tsnetHostname,
		TSNetAuthKey:  firstEnv("TS_AUTHKEY", "TS_AUTH_KEY"),
		TSNetStateDir: tsnetStateDir,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha serve:", err)
		return ExitError
	}
	defer func() { _ = srv.Close() }()

	if tsnetMode {
		fmt.Fprintf(os.Stderr, "alpha serve: tsnet://%s\n", srv.Addr())
		fmt.Fprintln(os.Stderr, "warning: tailnet ACLs control access to the agent API")
	} else {
		fmt.Fprintf(os.Stderr, "alpha serve: http://%s  (loopback only)\n", srv.Addr())
	}
	fmt.Fprintln(os.Stderr, "GET  /health")
	fmt.Fprintln(os.Stderr, "GET  /v1/session")
	fmt.Fprintln(os.Stderr, "POST /v1/prompt   {\"text\":\"...\"}")
	fmt.Fprintln(os.Stderr, "GET  /v1/events   text/event-stream")
	<-ctx.Done()
	return ExitOK
}

func printServeUsage(w *os.File) {
	fmt.Fprintf(w, `usage: alpha serve [--addr 127.0.0.1:38765]
       alpha serve --tsnet [--tsnet-hostname alpha] [--tsnet-state-dir PATH]

Loopback HTTP control plane over the same Controller as the TUI.
Use --tsnet to listen only on the Tailscale network.
Set TS_AUTHKEY or TS_AUTH_KEY for a new embedded node.
Tailnet ACLs control access to the agent API.

  GET  /health
  GET  /v1/session
  POST /v1/prompt   JSON {"text":"..."}
  GET  /v1/events   SSE of bus messages
`)
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}
