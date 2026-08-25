package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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
