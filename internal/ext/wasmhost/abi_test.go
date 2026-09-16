package wasmhost

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	got := statePath("/home/u", "todo", "state.json")
	assert.Equal(t, "/home/u/.alpha/plugins/todo/state.json", got)
}
