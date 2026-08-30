package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rapatel0/alpha/internal/mcp"
)

func TestConfigLoadSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := mcp.ServerConfig{
		Transport: "stdio",
		Command:   []string{"npx"},
		Args:      []string{"-y", "pkg"},
	}
	if err := mcp.AddServer("demo", cfg); err != nil {
		t.Fatal(err)
	}

	servers, err := mcp.Load(filepath.Join(t.TempDir(), ".alpha", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["demo"]; !ok {
		t.Fatal("expected demo server")
	}
	if got := servers["demo"].Command; len(got) != 1 || got[0] != "npx" {
		t.Fatalf("command = %v", got)
	}

	ok, err := mcp.RemoveServer("demo")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected remove ok")
	}
}

func TestLoadPrefersAgentsOverAlpha(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	write := func(path, name string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"servers":{"demo":{"command":["` + name + `"]}}}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".alpha", "mcp.json"), "legacy")
	write(filepath.Join(home, ".agents", "mcp.json"), "agents")
	proj := filepath.Join(t.TempDir(), ".agents", "mcp.json")
	write(filepath.Join(filepath.Dir(filepath.Dir(proj)), ".alpha", "mcp.json"), "proj-legacy")
	write(proj, "proj-agents")

	servers, err := mcp.Load(proj)
	if err != nil {
		t.Fatal(err)
	}
	got := servers["demo"].Command
	if len(got) != 1 || got[0] != "proj-agents" {
		t.Fatalf("project agents file must win, got %v", got)
	}
}

func TestCompactAndSlim(t *testing.T) {
	if got := mcp.CompactServerList([]string{"a", "b"}); got != "a b" {
		t.Fatalf("got %q", got)
	}
	tools := []mcp.ToolDef{
		{
			Name:        "echo",
			Description: "Echo back",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`,
			),
		},
	}
	if got := mcp.CompactToolNames(tools); got != "echo" {
		t.Fatalf("got %q", got)
	}
	slim := mcp.SlimTool(tools[0])
	if !strings.Contains(slim, "echo") || !strings.Contains(slim, "message:s*") {
		t.Fatalf("slim = %q", slim)
	}
}

func TestDisabled(t *testing.T) {
	t.Setenv("ALPHA_MCP", "off")
	if !mcp.Disabled() {
		t.Fatal("expected disabled")
	}
	pool, err := mcp.LoadPool(filepath.Join(t.TempDir(), ".alpha", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if pool != nil {
		t.Fatal("expected nil pool")
	}
}
