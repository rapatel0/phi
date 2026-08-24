package websearchtool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

func TestWebSearchToolSchema(t *testing.T) {
	tl := WebSearchTool()
	assert.Equal(t, "websearch", tl.Definition.Name)
	assert.True(t, tl.Definition.Readable)
}

func TestNativeBackend(t *testing.T) {
	cases := []struct {
		cfg  llm.ModelConfig
		want string
	}{
		{llm.ModelConfig{}, ""},
		{llm.ModelConfig{Name: "claude-sonnet-5", APIKey: "k", BaseURL: "https://api.anthropic.com"}, "anthropic"},
		{llm.ModelConfig{Name: "gemini-2.5-pro", APIKey: "k"}, "gemini"},
		{llm.ModelConfig{Name: "gpt-4o", APIKey: "sk-x", BaseURL: "https://api.openai.com/v1"}, "openai"},
		{llm.ModelConfig{Name: "grok-4", APIKey: "k", BaseURL: "https://api.x.ai/v1"}, "xai"},
		{llm.ModelConfig{Name: "gpt-5.5", APIKey: "k", BaseURL: "https://chatgpt.com/backend-api/codex"}, ""},
		{llm.ModelConfig{Name: "llama-3", APIKey: "k", BaseURL: "http://127.0.0.1:8080/v1"}, ""},
		{llm.ModelConfig{Name: "claude-sonnet-5"}, ""}, // no key
	}
	for _, c := range cases {
		assert.Equal(t, c.want, nativeBackend(c.cfg), "%+v", c.cfg)
	}
}

func TestDDGParseHTML(t *testing.T) {
	page := `
<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fblog">The Go Blog</a>
<a class="result__snippet">Official Go announcements.</a>
<a class="result__a" href="https://pkg.go.dev">pkg.go.dev</a>
<a class="result__snippet">Go packages.</a>
`
	hits := parseDDG(page)
	require.Len(t, hits, 2)
	assert.Equal(t, "The Go Blog", hits[0].title)
	assert.Equal(t, "https://go.dev/blog", hits[0].link)
	assert.Equal(t, "Official Go announcements.", hits[0].snippet)
	assert.Equal(t, "https://pkg.go.dev", hits[1].link)
}

func TestRunWebSearchDDG(t *testing.T) {
	prev := fetchRaw
	fetchRaw = func(_ context.Context, rawURL, _ string) ([]byte, error) {
		assert.Contains(t, rawURL, "duckduckgo.com")
		assert.Contains(t, rawURL, "golang")
		return []byte(`<a class="result__a" href="https://go.dev">Go</a>`), nil
	}
	t.Cleanup(func() { fetchRaw = prev })

	raw, err := json.Marshal(map[string]string{"query": "golang"})
	require.NoError(t, err)
	out, err := runWebSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "https://go.dev")
	assert.Equal(t, "golang", out.Detail)
}

func TestRunWebSearchNativeThenFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"nope"}`)
	}))
	t.Cleanup(srv.Close)
	prevClient, prevFetch := httpClient, fetchRaw
	httpClient = srv.Client()
	fetchRaw = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`<a class="result__a" href="https://example.com/hit">Hit</a>`), nil
	}
	t.Cleanup(func() {
		httpClient = prevClient
		fetchRaw = prevFetch
	})

	raw, err := json.Marshal(map[string]string{"query": "widgets"})
	require.NoError(t, err)
	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{
		Name:    "claude-sonnet-5",
		APIKey:  "sk-ant-api03-x",
		BaseURL: srv.URL,
	})
	out, err := runWebSearch(ctx, raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Native search failed")
	assert.Contains(t, out.Content, "https://example.com/hit")
}

func TestRunWebSearchNativeAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "sk-ant-api03-x", r.Header.Get("x-api-key"))
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "web_search_20250305")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"content": [
				{"type":"text","text":"Widgets are useful."},
				{"type":"web_search_tool_result","content":[
					{"type":"web_search_result","title":"Widgets","url":"https://example.com/w"}
				]}
			]
		}`)
	}))
	t.Cleanup(srv.Close)
	prevClient, prevFetch := httpClient, fetchRaw
	httpClient = srv.Client()
	fetchRaw = func(context.Context, string, string) ([]byte, error) {
		t.Fatal("ddg should not run")
		return nil, nil
	}
	t.Cleanup(func() {
		httpClient = prevClient
		fetchRaw = prevFetch
	})

	raw, err := json.Marshal(map[string]string{"query": "widgets"})
	require.NoError(t, err)
	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{
		Name:    "claude-sonnet-5",
		APIKey:  "sk-ant-api03-x",
		BaseURL: srv.URL,
	})
	out, err := runWebSearch(ctx, raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Widgets are useful.")
	assert.Contains(t, out.Content, "https://example.com/w")
	assert.NotContains(t, out.Content, "Native search failed")
}

func TestParseGeminiAndResponses(t *testing.T) {
	gemini, err := parseGeminiSearch([]byte(`{
		"candidates":[{
			"content":{"parts":[{"text":"Hello"}]},
			"groundingMetadata":{"groundingChunks":[{"web":{"title":"T","uri":"https://t.example"}}]}
		}]
	}`))
	require.NoError(t, err)
	assert.Contains(t, gemini, "Hello")
	assert.Contains(t, gemini, "https://t.example")

	resp, err := parseResponsesSearch([]byte(`{
		"output":[{"type":"message","content":[{
			"text":"Found it",
			"annotations":[{"type":"url_citation","title":"A","url":"https://a.example"}]
		}]}],
		"citations":["https://b.example"]
	}`))
	require.NoError(t, err)
	assert.Contains(t, resp, "Found it")
	assert.Contains(t, resp, "https://a.example")
	assert.Contains(t, resp, "https://b.example")
}

func TestRunWebSearchEmptyQuery(t *testing.T) {
	_, err := runWebSearch(t.Context(), json.RawMessage(`{"query":"  "}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty query")
}

func TestAnthropicOAuthHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer sk-ant-oat-"))
		assert.Empty(t, r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	t.Cleanup(srv.Close)
	prev := httpClient
	httpClient = srv.Client()
	t.Cleanup(func() { httpClient = prev })

	text, err := anthropicSearch(t.Context(), "q", llm.ModelConfig{
		Name:    "claude-sonnet-5",
		APIKey:  "sk-ant-oat-x",
		BaseURL: srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", text)
}
