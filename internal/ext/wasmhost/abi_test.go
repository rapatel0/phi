package wasmhost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/llm"
)

func TestModelJSONOmitsAPIKey(t *testing.T) {
	raw := modelJSON(llm.ModelConfig{
		Name:          "grok-4.6",
		APIKey:        "secret",
		BaseURL:       "https://api.x.ai/v1",
		ContextWindow: 500_000,
	})

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "grok-4.6", got["name"])
	assert.Equal(t, "https://api.x.ai/v1", got["base_url"])
	assert.NotContains(t, string(raw), "secret")
}

func TestScopeNamesAcceptsBothForms(t *testing.T) {
	assert.Equal(t, []string{"read", "grep"}, scopeNames(`["read","grep"]`))
	assert.Equal(t, []string{"read", "grep"}, scopeNames("read, grep"))
	assert.Nil(t, scopeNames("  "))
}

func TestAssetPathStaysInsideModuleDir(t *testing.T) {
	assert.Equal(t, "dir/prompts/style.md", assetPath("dir", "prompts/style.md"))
	assert.Equal(t, "dir/style.md", assetPath("dir", "../style.md"))
}

func TestStatePathIsNamespacedPerPlugin(t *testing.T) {
	assert.Equal(t, "/home/u/.alpha/plugins/todo/state.json", statePath("/home/u", "todo", "state.json"))
}

// TestGetterGuestUsesHostABI loads a hand-written module that calls each getter
// added for plugins, so the reply offset and the returned lengths are checked
// against a real guest rather than only the Go helpers.
func TestGetterGuestUsesHostABI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "getter.wasm"), readTestdata(t, "getter.wasm"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("asset for the guest"), 0o644))

	h := ext.NewHost()
	var scoped []string
	h.SetToolScope(func(names []string) { scoped = names })
	h.SetToolNames(func() []string { return []string{"read", "grep"} })
	h.SetModelInfo(llm.ModelConfig{
		Name:          "grok-4.6",
		APIKey:        "secret",
		BaseURL:       "https://api.x.ai/v1",
		ContextWindow: 500_000,
	})
	require.NoError(t, loadOne(t.Context(), h, filepath.Join(dir, "getter.wasm")))

	assert.Equal(t, []string{"read"}, scoped, "set_active_tools must reach the engine")

	var found ext.Command
	for _, c := range h.Commands() {
		if c.Name == "getter" {
			found = c
		}
	}
	require.NotNil(t, found.Run)

	res, err := found.Run(t.Context(), nil)
	require.NoError(t, err)

	// model_info reaches the toast, without the credential.
	assert.Contains(t, res.Toast, "grok-4.6")
	assert.NotContains(t, res.Toast, "secret")
	// read_asset reaches the submit text, read from the module directory.
	assert.Equal(t, "asset for the guest", res.Submit)
	// active_tools reaches the footer status as a JSON list.
	assert.True(t, res.StatusSet)
	assert.Equal(t, `["read","grep"]`, res.Status)
}

func readTestdata(t *testing.T, file string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", file))
	require.NoError(t, err)
	return raw
}
