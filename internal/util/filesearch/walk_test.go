package filesearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestResolveFD(t *testing.T) {
	ResetResolveFDForTest()
	bin, err := ResolveFD()
	if err != nil {
		t.Skip("fd not installed:", err)
	}
	if bin == "" {
		t.Fatal("empty fd path")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("fd path %q not usable: %v", bin, err)
	}
}

func TestSearch(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("go.mod", "module x\n")
	mustWrite("internal/session/manager.go", "package session\n")
	mustWrite("internal/session/manager_test.go", "package session\n")
	mustWrite("README.md", "# x\n")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	all, truncated, err := Search(ctx, dir, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 4 {
		t.Fatalf("expected >=4 files, got %v", all)
	}
	if truncated {
		t.Fatalf("4 files under a limit of 20 must not report truncation: %v", all)
	}

	hits, _, err := Search(ctx, dir, "manager", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected manager hits")
	}
	for _, h := range hits {
		if !strings.Contains(h, "manager") {
			t.Fatalf("unexpected hit %q", h)
		}
		if strings.Contains(h, "\\") {
			t.Fatalf("path should use slashes: %q", h)
		}
	}

	// 4 files, limit 2: the list is cut and the caller must be told.
	limited, truncated, err := Search(ctx, dir, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 {
		t.Fatalf("want exactly 2 paths, got %v", limited)
	}
	if !truncated {
		t.Fatal("more files exist beyond the limit, so truncated must be true")
	}

	// Exactly at the limit is not truncation: there is nothing more to show.
	exact, truncated, err := Search(ctx, dir, "manager", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 2 {
		t.Fatalf("want the 2 manager files, got %v", exact)
	}
	if truncated {
		t.Fatal("a full page with no further matches must not report truncation")
	}

	none, truncated, err := Search(ctx, dir, "zzz-no-such-file-xyz", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("expected empty, got %v", none)
	}
	if truncated {
		t.Fatal("an empty result cannot be truncated")
	}
}

// A deadline must surface as ErrTimeout, not as the child's "signal: killed".
// The picker shows this text, so it has to be about the search, not the process.
func TestSearchTimeoutIsTyped(t *testing.T) {
	// Already past the deadline, so fd is killed immediately.
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	_, _, err := Search(ctx, t.TempDir(), "anything", 10)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if strings.Contains(err.Error(), "signal:") {
		t.Fatalf("child-process detail leaked to the caller: %v", err)
	}
}

// A cancelled search reports the cancellation, not a process failure.
func TestSearchCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := Search(ctx, t.TempDir(), "anything", 10)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// The walk runs in process, so a missing fd binary must not stop it. This is
// the property that makes the picker work on a machine without fd.
func TestSearchWorksWithoutFD(t *testing.T) {
	ResetResolveFDForTest()
	t.Cleanup(ResetResolveFDForTest)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Empty PATH, so fd cannot be resolved even if it is installed.
	t.Setenv("PATH", "")

	got, _, err := Search(t.Context(), dir, "", 5)
	if err != nil {
		t.Fatalf("search must not need an external binary: %v", err)
	}
	if len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("want [a.go], got %v", got)
	}
}

// An empty query must list files, and results must stay relative to cwd
// rather than leaking the absolute root.
func TestSearchEmptyQueryListsRelativePaths(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"a.go", "sub/b.go"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, _, err := Search(t.Context(), dir, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want both files, got %v", got)
	}
	for _, p := range got {
		if filepath.IsAbs(p) || strings.HasPrefix(p, dir) {
			t.Fatalf("path must be relative to cwd, got %q", p)
		}
	}
}

// A query must match against the path, not just the file name, and the result
// must still be relative.
func TestSearchMatchesDirectorySegment(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "internal", "session", "manager.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, _, err := Search(t.Context(), dir, "session", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "internal/session/manager.go" {
		t.Fatalf("want internal/session/manager.go, got %v", got)
	}
}

// The picker must never offer a file git ignores. This covers the cases that
// make a hand-rolled matcher risky: a directory rule, a glob, a negation, and
// a nested .gitignore that applies only below its own directory.
func TestSearchRespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "node_modules/\n*.log\n!keep.log\n")
	write("src/app.go", "x")
	write("node_modules/lib/index.js", "x")
	write("debug.log", "x")
	write("keep.log", "x")
	write("nested/.gitignore", "secret*\n")
	write("nested/secret.txt", "x")
	write("nested/ok.txt", "x")

	got, _, err := Search(t.Context(), dir, "", 100)
	if err != nil {
		t.Fatal(err)
	}

	found := make(map[string]bool, len(got))
	for _, p := range got {
		found[p] = true
	}
	for _, want := range []string{"src/app.go", "nested/ok.txt", "keep.log"} {
		if !found[want] {
			t.Errorf("%q must be listed, got %v", want, got)
		}
	}
	for _, deny := range []string{"node_modules/lib/index.js", "debug.log", "nested/secret.txt"} {
		if found[deny] {
			t.Errorf("%q is ignored by git and must not be listed", deny)
		}
	}
}

// The walk stops once enough matches exist, so a huge tree costs no more than
// a small one when matches are common.
func TestSearchStopsEarly(t *testing.T) {
	dir := t.TempDir()
	for i := range 500 {
		p := filepath.Join(dir, "f"+strconv.Itoa(i)+".go")
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, truncated, err := Search(t.Context(), dir, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Fatalf("want 20 paths, got %d", len(got))
	}
	if !truncated {
		t.Fatal("480 more files exist, so truncated must be true")
	}
}
