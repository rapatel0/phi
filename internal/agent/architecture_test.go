package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests enforce architecture invariants that depguard cannot express.
// depguard reasons about imports; these reason about call order and call
// sites inside the agent loop. See doc/ext-api.md.

// callsInOrder returns the names called inside fn, in source order, keeping
// only names in want. Selector calls such as e.checkPermission report the
// selector ("checkPermission").
func callsInOrder(fn *ast.FuncDecl, want map[string]bool) []string {
	var got []string
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch f := call.Fun.(type) {
		case *ast.SelectorExpr:
			name = f.Sel.Name
		case *ast.Ident:
			name = f.Name
		}
		if want[name] {
			got = append(got, name)
		}
		return true
	})
	return got
}

func findFunc(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("function %q not found", name)
	return nil
}

func parseExecutor(t *testing.T) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "executor.go", nil, parser.ParseComments)
	require.NoError(t, err, "parse executor.go")
	return fset, file
}

// The tool loop is PreHooks, then the permission gate, then Run, then
// PostHooks. A tool must never execute before the gate decides.
func TestToolLoopOrderIsPreHookGateRun(t *testing.T) {
	_, file := parseExecutor(t)
	fn := findFunc(t, file, "runOne")

	want := map[string]bool{
		"PreTool":         true,
		"checkPermission": true,
		"Run":             true,
	}
	got := callsInOrder(fn, want)

	// Keep the first occurrence of each name; later repeats do not matter.
	var order []string
	seen := map[string]bool{}
	for _, name := range got {
		if !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
	}

	require.Equal(t, []string{"PreTool", "checkPermission", "Run"}, order,
		"tool loop order changed: a tool must not run before the permission gate")
}

// The gate must have exactly one call site. A second one is how a bypass
// gets introduced without anyone noticing.
func TestPermissionGateHasSingleCallSite(t *testing.T) {
	src, err := os.ReadFile("executor.go")
	require.NoError(t, err)

	n := strings.Count(string(src), "e.gate.Check(")
	assert.Equal(t, 1, n,
		"the permission gate must be called from exactly one place in executor.go")
}

// checkPermission must handle every permission decision. A missing case
// would fall through and allow the tool to run.
func TestCheckPermissionHandlesEveryDecision(t *testing.T) {
	src, err := os.ReadFile("executor.go")
	require.NoError(t, err)
	body := string(src)

	for _, decision := range []string{
		"permission.Allow",
		"permission.Deny",
		"permission.Ask",
	} {
		assert.Contains(t, body, "case "+decision+":",
			"checkPermission must handle %s", decision)
	}
}

// Tests belong next to the code they cover: foo_test.go tests foo.go. A test
// file with no parent splits a package's tests across names that no longer say
// what they cover, which is how engine_lifecycle_test.go started.
//
// A file declaring package x_test is exempt: an external test cannot merge
// into an internal test file. The rest are listed in the baseline, which
// ratchets like scripts/deadcode-baseline.txt. New orphans fail; the baseline
// only shrinks.
func TestTestFilesAreColocated(t *testing.T) {
	const baselinePath = "../../scripts/colocation-baseline.txt"

	raw, err := os.ReadFile(baselinePath)
	require.NoError(t, err, "baseline missing: %s", baselinePath)
	baseline := map[string]bool{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			baseline[line] = true
		}
	}

	var found []string
	roots := []string{"../../internal", "../../cmd"}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			parent := strings.TrimSuffix(d.Name(), "_test.go") + ".go"
			if _, statErr := os.Stat(filepath.Join(filepath.Dir(path), parent)); statErr == nil {
				return nil
			}
			// An external test package cannot live in an internal test file.
			if isExternalTestPackage(t, path) {
				return nil
			}
			rel := filepath.ToSlash(strings.TrimPrefix(path, "../../"))
			found = append(found, rel)
			return nil
		})
		require.NoError(t, err)
	}

	for _, rel := range found {
		if !baseline[rel] {
			t.Errorf("%s has no matching source file: put these tests in the file "+
				"they cover, or add the path to %s with a reason",
				rel, strings.TrimPrefix(baselinePath, "../../"))
		}
	}

	// A stale entry means the file was fixed or removed. Drop it, so the
	// baseline keeps shrinking.
	got := map[string]bool{}
	for _, rel := range found {
		got[rel] = true
	}
	for rel := range baseline {
		if !got[rel] {
			t.Errorf("%s is in the colocation baseline but is no longer an orphan: remove the entry", rel)
		}
	}
}

// isExternalTestPackage reports whether the file declares package x_test.
func isExternalTestPackage(t *testing.T, path string) bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
	require.NoError(t, err)
	return strings.HasSuffix(f.Name.Name, "_test")
}
