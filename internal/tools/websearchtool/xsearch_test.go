package websearchtool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

func TestXSearchToolSchema(t *testing.T) {
	tl := XSearchTool()
	assert.Equal(t, "x_search", tl.Definition.Name)
	assert.True(t, tl.Definition.Readable)
	require.NotNil(t, tl.DetailFromArgs)
	assert.Equal(t, "release notes", tl.DetailFromArgs(json.RawMessage(`{"query":"release notes"}`)))
}

func TestRunXSearchReturnsPostsAndSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/responses", r.URL.Path)
		assert.Equal(t, "Bearer k", r.Header.Get("Authorization"))

		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Contains(t, payload["input"], "Only posts from these handles: a, b")

		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{map[string]any{
				"type": "message",
				"content": []any{map[string]any{
					"text": "grok 4.6 shipped with a longer context.",
					"annotations": []any{map[string]any{
						"type":  "url_citation",
						"title": "xAI",
						"url":   "https://x.com/xai",
					}},
				}},
			}},
		})
	}))
	defer srv.Close()

	prev := httpClient
	httpClient = srv.Client()
	defer func() { httpClient = prev }()

	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{
		Name:    "grok-4.6",
		APIKey:  "k",
		BaseURL: srv.URL,
	})
	res, err := XSearchTool().Run(ctx, json.RawMessage(`{"query":"release notes","handles":["a","@b"]}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "grok 4.6 shipped")
	assert.Contains(t, res.Content, "[xAI](https://x.com/xai)")
	assert.Equal(t, "release notes", res.Detail)
}

func TestXSearchNeedsAKey(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")

	_, err := XSearchTool().Run(t.Context(), []byte(`{"query":"grok 4.6 release notes"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "XAI_API_KEY")
}

func TestRunXSearchRejectsEmptyQuery(t *testing.T) {
	_, err := XSearchTool().Run(t.Context(), json.RawMessage(`{"query":"  "}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

func TestXAIConfigFallsBackToEnvKey(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-key")
	cfg := xaiConfig(llm.ModelConfig{})
	assert.Equal(t, "env-key", cfg.APIKey)
	assert.Equal(t, xaiSearchModel, cfg.Name)
	assert.Equal(t, xaiDefault, cfg.BaseURL)
}

func TestXAIConfigKeepsActiveXAISession(t *testing.T) {
	active := llm.ModelConfig{Name: "grok-4.5", APIKey: "k", BaseURL: "https://api.x.ai/v1"}
	assert.Equal(t, active, xaiConfig(active))
}
