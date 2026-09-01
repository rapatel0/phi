package wasmhost

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rapatel0/alpha/internal/ext"
)

func TestLoadHelloRegistersCommand(t *testing.T) {
	src := filepath.Join("testdata", "hello.wasm")
	bin, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.wasm"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	h := ext.NewHost()
	if err := Load(context.Background(), h, []string{dir}); err != nil {
		t.Fatal(err)
	}
	var found ext.Command
	for _, c := range h.Commands() {
		if c.Name == "hello" {
			found = c
			break
		}
	}
	if found.Run == nil {
		t.Fatalf("commands=%v", names(h))
	}
	res, err := found.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Toast != "hello from wasm" {
		t.Fatalf("toast=%q", res.Toast)
	}
}

func names(h *ext.Host) []string {
	var out []string
	for _, c := range h.Commands() {
		out = append(out, c.Name)
	}
	return out
}
