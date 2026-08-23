package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pulseaiclub/phi/internal/cli"
	"github.com/pulseaiclub/phi/internal/mcp"
	"github.com/pulseaiclub/phi/internal/project"
)

// mcpCommand manages MCP server configuration. Subcommands return errors
// prefixed by the framework ("phi mcp add: ...").
func mcpCommand() *cli.Command {
	c := &cli.Command{
		Name: "mcp",
		Desc: "manage MCP servers",
		Long: "Set PHI_MCP=off to disable MCP meta-tools in the agent.\nSee doc/mcp.md.",
	}
	list := &cli.Command{Name: "list", Desc: "list configured servers"}
	list.Run = func(args []string) error {
		if len(args) > 0 {
			return list.Usagef("unexpected argument %q", args[0])
		}
		return mcpList()
	}
	add := &cli.Command{Name: "add", Desc: "add a stdio server to ~/.phi/mcp.json", ArgsUse: "<name> -- <cmd> [args…]"}
	add.Run = func(args []string) error {
		if len(args) == 0 {
			return add.Usagef("missing server name")
		}
		name := args[0]
		rest := args[1:]
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return add.Usagef("missing command after --")
		}
		return mcpAddServer(name, rest)
	}
	remove := &cli.Command{
		Name:    "remove",
		Aliases: []string{"rm"},
		Desc:    "remove a server from user config",
		ArgsUse: "<name>",
	}
	remove.Run = func(args []string) error {
		if len(args) != 1 {
			return remove.Usagef("expected exactly one server name")
		}
		return mcpRemoveServer(args[0])
	}
	call := &cli.Command{
		Name:    "call",
		Desc:    "call a tool (optional JSON args object)",
		ArgsUse: "<server> <tool> [json]",
	}
	call.Run = func(args []string) error {
		if len(args) < 2 {
			return call.Usagef("expected <server> <tool> [json-args]")
		}
		argMap := map[string]any{}
		if len(args) >= 3 {
			raw := strings.Join(args[2:], " ")
			if err := json.Unmarshal([]byte(raw), &argMap); err != nil {
				return call.Usagef("args must be a JSON object: %v", err)
			}
		}
		return mcpCallTool(args[0], args[1], argMap)
	}
	doctor := &cli.Command{Name: "doctor", Desc: "check config + connectivity"}
	doctor.Run = func(args []string) error {
		if len(args) > 0 {
			return doctor.Usagef("unexpected argument %q", args[0])
		}
		return mcpDoctor()
	}
	c.Add(list, add, remove, call, doctor)
	return c
}

func mcpList() error {
	servers, err := mcp.Load(project.GetDefaultProject().MCPConfigFile())
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		fmt.Println("(no servers — try: phi mcp add fetch -- npx -y @modelcontextprotocol/server-fetch)")
		return nil
	}
	for _, name := range sortedKeys(servers) {
		cfg := servers[name]
		cmd, _ := cfg.CmdLine()
		fmt.Printf("%s\t%s\n", name, strings.Join(cmd, " "))
	}
	return nil
}

func sortedKeys(m map[string]mcp.ServerConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mcpAddServer(name string, rest []string) error {
	cfg := mcp.ServerConfig{
		Transport: "stdio",
		Command:   rest[:1],
		Args:      rest[1:],
	}
	if err := mcp.AddServer(name, cfg); err != nil {
		return err
	}
	fmt.Printf("added %s\n", name)
	return nil
}

func mcpRemoveServer(name string) error {
	ok, err := mcp.RemoveServer(name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%q not in user config", name)
	}
	fmt.Printf("removed %s\n", name)
	return nil
}

func mcpCallTool(server, tool string, argMap map[string]any) error {
	pool, err := mcp.LoadPool(project.GetDefaultProject().MCPConfigFile())
	if err != nil {
		return err
	}
	if pool == nil {
		return errors.New("MCP disabled (PHI_MCP=off)")
	}
	defer func() { _ = pool.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := pool.Call(ctx, server, tool, argMap)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func mcpDoctor() error {
	if mcp.Disabled() {
		fmt.Println("PHI_MCP=off — MCP disabled")
		return nil
	}
	pool, err := mcp.LoadPool(project.GetDefaultProject().MCPConfigFile())
	if err != nil {
		return err
	}
	if pool == nil {
		return errors.New("no pool")
	}
	defer func() { _ = pool.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results := pool.Doctor(ctx)
	fail := 0
	for _, r := range results {
		status := "ok"
		if !r.OK {
			status = "FAIL"
			fail++
		}
		fmt.Printf("%s\t%s\t%s\n", status, r.Name, r.Detail)
	}
	if fail > 0 {
		return fmt.Errorf("%d server(s) failed", fail)
	}
	return nil
}
