package websearchtool

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/llm"
)

// useTestClient routes native search calls at an httptest server.
func useTestClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := httpClient
	httpClient = srv.Client()
	t.Cleanup(func() { httpClient = prev })
}

// failDDG makes the DuckDuckGo fallback fail loudly if it is reached.
func failDDG(t *testing.T) {
	t.Helper()
	prev := fetchRaw
	fetchRaw = func(context.Context, string, string) ([]byte, error) {
		t.Error("ddg fallback should not run")
		return nil, errors.New("unexpected ddg call")
	}
	t.Cleanup(func() { fetchRaw = prev })
}

func TestClientDefaultsWhenUnset(t *testing.T) {
	prev := httpClient
	httpClient = nil
	t.Cleanup(func() { httpClient = prev })

	assert.NotNil(t, client())
}

func TestNativeSearchUnknownBackend(t *testing.T) {
	_, err := nativeSearch(t.Context(), "carrier-pigeon", "q", llm.ModelConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown backend")
}

func TestGeminiSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "gemini-2.5-flash:generateContent")
		assert.Equal(t, "gem-k", r.URL.Query().Get("key"))
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "google_search")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"candidates":[{
				"content":{"parts":[{"text":"Gemini says hi"}]},
				"groundingMetadata":{"groundingChunks":[
					{"web":{"title":"Doc","uri":"https://g.example/doc"}}
				]}
			}]
		}`)
	}))
	t.Cleanup(srv.Close)
	useTestClient(t, srv)

	got, err := geminiSearch(t.Context(), "q", llm.ModelConfig{
		Name: "gemini-2.5-flash", APIKey: "gem-k", BaseURL: srv.URL,
	})
	require.NoError(t, err)
	assert.Contains(t, got, "Gemini says hi")
	assert.Contains(t, got, "https://g.example/doc")
}

func TestResponsesSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/responses", r.URL.Path)
		assert.Equal(t, "Bearer sk-x", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), `"web_search"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"output":[{"type":"message","content":[{
				"text":"Answer",
				"annotations":[{"type":"url_citation","title":"Src","url":"https://o.example"}]
			}]}]
		}`)
	}))
	t.Cleanup(srv.Close)
	useTestClient(t, srv)

	got, err := responsesSearch(t.Context(), "q", llm.ModelConfig{
		Name: "gpt-4o", APIKey: "sk-x", BaseURL: srv.URL,
	}, openaiDefault)
	require.NoError(t, err)
	assert.Contains(t, got, "Answer")
	assert.Contains(t, got, "https://o.example")
}

func TestRunWebSearchNativeXAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/responses", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":[{"type":"message","content":[{"text":"Grok answer"}]}]}`)
	}))
	t.Cleanup(srv.Close)
	useTestClient(t, srv)
	failDDG(t)

	got, err := doSearch(t.Context(), "q", llm.ModelConfig{
		Name: "grok-4", APIKey: "xai-k", BaseURL: srv.URL,
	})
	require.NoError(t, err)
	assert.Contains(t, got, "Grok answer")
}

func TestDoSearchBothBackendsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	useTestClient(t, srv)
	prev := fetchRaw
	fetchRaw = func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("ddg offline")
	}
	t.Cleanup(func() { fetchRaw = prev })

	_, err := doSearch(t.Context(), "q", llm.ModelConfig{
		Name: "claude-sonnet-5", APIKey: "sk-ant-api03-x", BaseURL: srv.URL,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ddg offline")
	assert.Contains(t, err.Error(), "anthropic")
}

func TestAnthropicMessagesURL(t *testing.T) {
	cases := map[string]string{
		"":                                "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com":       "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/":      "https://api.anthropic.com/v1/messages",
		"https://proxy.local/v1":          "https://proxy.local/v1/messages",
		"https://proxy.local/v1/messages": "https://proxy.local/v1/messages",
	}
	for in, want := range cases {
		assert.Equal(t, want, anthropicMessagesURL(in), in)
	}
}

func TestGeminiGenerateURL(t *testing.T) {
	got := geminiGenerateURL("", "gemini-2.5-pro", "k+y")
	assert.Contains(t, got, geminiDefault)
	assert.Contains(t, got, "/models/gemini-2.5-pro:generateContent")
	assert.Contains(t, got, "key=k%2By")

	got = geminiGenerateURL("https://proxy.local/v1beta/", "m", "k")
	assert.Equal(t, "https://proxy.local/v1beta/models/m:generateContent?key=k", got)
}

func TestResponsesURL(t *testing.T) {
	assert.Equal(t, openaiDefault+"/responses", responsesURL("", openaiDefault))
	assert.Equal(t, xaiDefault+"/responses", responsesURL("", xaiDefault))
	assert.Equal(t, "https://p.local/responses", responsesURL("https://p.local/", openaiDefault))
	assert.Equal(t, "https://p.local/responses", responsesURL("https://p.local/responses", openaiDefault))
}

func TestPostJSONTruncatesErrorSnippet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, strings.Repeat("x", 900))
	}))
	t.Cleanup(srv.Close)
	useTestClient(t, srv)

	_, err := postJSON(t.Context(), srv.URL, nil, map[string]string{"a": "b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http 502")
	assert.Less(t, len(err.Error()), 300)
}

func TestPostJSONUnmarshalablePayload(t *testing.T) {
	_, err := postJSON(t.Context(), "https://example.com", nil, make(chan int))
	require.Error(t, err)
}

func TestParseAnthropicSearchNoResults(t *testing.T) {
	got, err := parseAnthropicSearch([]byte(`{"content":[]}`))
	require.NoError(t, err)
	assert.Equal(t, "No results found.", got)
}

func TestParseAnthropicSearchDedupesSources(t *testing.T) {
	got, err := parseAnthropicSearch([]byte(`{"content":[
		{"type":"text","text":"Body","citations":[{"title":"A","url":"https://a.example"}]},
		{"type":"web_search_tool_result","content":[
			{"type":"web_search_result","title":"A again","url":"https://a.example"},
			{"type":"web_search_result","title":"B","url":"https://b.example"},
			{"type":"web_search_result","title":"blank","url":"  "}
		]}
	]}`))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(got, "https://a.example"))
	assert.Contains(t, got, "https://b.example")
}

func TestParseSearchInvalidJSON(t *testing.T) {
	_, err := parseAnthropicSearch([]byte(`{`))
	require.Error(t, err)
	_, err = parseGeminiSearch([]byte(`{`))
	require.Error(t, err)
	_, err = parseResponsesSearch([]byte(`{`))
	require.Error(t, err)
}

func TestParseGeminiSearchNoCandidates(t *testing.T) {
	got, err := parseGeminiSearch([]byte(`{"candidates":[]}`))
	require.NoError(t, err)
	assert.Equal(t, "No results found.", got)
}

func TestParseResponsesSearchSkipsNonMessage(t *testing.T) {
	got, err := parseResponsesSearch([]byte(`{
		"output":[
			{"type":"web_search_call","content":[{"text":"ignored"}]},
			{"type":"message","content":[{"text":"kept"}]}
		]
	}`))
	require.NoError(t, err)
	assert.Contains(t, got, "kept")
	assert.NotContains(t, got, "ignored")
}

func TestParseResponsesSearchEmpty(t *testing.T) {
	got, err := parseResponsesSearch([]byte(`{"output":[]}`))
	require.NoError(t, err)
	assert.Equal(t, "No results found.", got)
}

func TestFormatSourcesCapsAndFallsBackToURL(t *testing.T) {
	many := make([]src, 0, maxResults+3)
	for i := range maxResults + 3 {
		many = append(many, src{URL: "https://e.example/" + string(rune('a'+i))})
	}
	got := formatSources(many)
	assert.Equal(t, maxResults, strings.Count(got, "\n- "))
	// Untitled sources use the URL as the link text, so each line repeats it.
	assert.Contains(t, got, "[https://e.example/a](https://e.example/a)")
	assert.NotContains(t, got, "https://e.example/i")
}
