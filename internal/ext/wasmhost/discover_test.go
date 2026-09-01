package wasmhost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesLaterDirWins(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	if err := os.WriteFile(filepath.Join(a, "hello.wasm"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "hello.wasm"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := Files([]string{a, b})
	if len(files) != 1 {
		t.Fatalf("files=%d", len(files))
	}
	got, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "B" {
		t.Fatalf("got %q", got)
	}
}

func TestDirsIncludeAgentsPlugins(t *testing.T) {
	dirs := Dirs("/repo")
	joined := filepath.Join(dirs...)
	if !containsPath(dirs, "plugins") {
		t.Fatalf("dirs=%v joined=%s", dirs, joined)
	}
}

func containsPath(dirs []string, elem string) bool {
	for _, d := range dirs {
		if filepath.Base(d) == elem {
			return true
		}
	}
	return false
}
