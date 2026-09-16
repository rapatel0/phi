package lens

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

const fixture = `package probe

func Helper() string {
	return "x"
}

func Use() string {
	return Helper()
}
`

func TestToolsRegisterOnHost(t *testing.T) {
	h := ext.NewHost()
	require.NoError(t, (&Plugin{}).Register(h))

	var names []string
	for _, tl := range h.Tools() {
		names = append(names, tl.Definition.Name)
	}
	assert.Contains(t, names, "lsp_diagnostics")
	assert.Contains(t, names, "lsp_navigation")
}

func TestNavigationFindsDefinition(t *testing.T) {
	root, _ := goModule(t, fixture)
	ctx := tooldef.WithCwd(t.Context(), root)

	res, err := NavigationTool().Run(ctx, json.RawMessage(`{"symbol":"Helper"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "sample.go")
	assert.Contains(t, res.Content, "func Helper() string")
	assert.Equal(t, "Helper", NavigationTool().DetailFromArgs(json.RawMessage(`{"symbol":"Helper"}`)))
}

func TestNavigationReferencesExcludeDefinition(t *testing.T) {
	root, _ := goModule(t, fixture)
	ctx := tooldef.WithCwd(t.Context(), root)

	res, err := NavigationTool().Run(ctx, json.RawMessage(`{"symbol":"Helper","operation":"references"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "return Helper()")
	assert.NotContains(t, res.Content, "func Helper() string")
}

func TestNavigationHoverReturnsDeclaration(t *testing.T) {
	root, _ := goModule(t, fixture)
	ctx := tooldef.WithCwd(t.Context(), root)

	res, err := NavigationTool().Run(ctx, json.RawMessage(`{"symbol":"Helper","operation":"hover"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "sample.go")
	assert.Contains(t, res.Content, "func Helper() string")
}

func TestNavigationRequiresSymbol(t *testing.T) {
	root, _ := goModule(t, fixture)
	ctx := tooldef.WithCwd(t.Context(), root)

	_, err := NavigationTool().Run(ctx, json.RawMessage(`{"symbol":" "}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbol is required")
}

func TestDiagnosticsReportsGoTypeErrors(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() {\n\tvar x int = \"nope\"\n\t_ = x\n}\n")
	ctx := tooldef.WithCwd(t.Context(), root)

	args, err := json.Marshal(map[string]any{"paths": []string{file}})
	require.NoError(t, err)
	res, err := DiagnosticsTool().Run(ctx, args)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "cannot use")
	assert.Contains(t, res.Detail, "problem")
}

func TestDiagnosticsOnCleanFile(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() string { return \"ok\" }\n")
	ctx := tooldef.WithCwd(t.Context(), root)

	args, err := json.Marshal(map[string]any{"paths": []string{file}})
	require.NoError(t, err)
	res, err := DiagnosticsTool().Run(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, "clean", res.Detail)
}
