package websearchtool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebSearchToolSchemaFields(t *testing.T) {
	tl := WebSearchTool()
	require.NotNil(t, tl.Definition.Params)
	assert.Equal(t, []string{"query"}, tl.Definition.Params.Required)
	assert.Contains(t, tl.Definition.Params.Properties, "query")
	require.NotNil(t, tl.DetailFromArgs)
	assert.Equal(t, "golang", tl.DetailFromArgs(json.RawMessage(`{"query":"  golang  "}`)))
	assert.Empty(t, tl.DetailFromArgs(json.RawMessage(`nope`)))
}

func TestRunWebSearchBadArgs(t *testing.T) {
	_, err := runWebSearch(t.Context(), json.RawMessage(`{`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "websearch args")
}

func TestRunWebSearchTruncatesLongQuery(t *testing.T) {
	var gotURL string
	prev := fetchRaw
	fetchRaw = func(_ context.Context, rawURL, _ string) ([]byte, error) {
		gotURL = rawURL
		return []byte(`<a class="result__a" href="https://go.dev">Go</a>`), nil
	}
	t.Cleanup(func() { fetchRaw = prev })

	long := strings.Repeat("z", maxQueryRun+50)
	raw, err := json.Marshal(map[string]string{"query": long})
	require.NoError(t, err)
	out, err := runWebSearch(t.Context(), raw)
	require.NoError(t, err)
	assert.Len(t, []rune(out.Detail), maxQueryRun)
	assert.NotContains(t, gotURL, strings.Repeat("z", maxQueryRun+1))
}

func TestDDGSearchFetchError(t *testing.T) {
	prev := fetchRaw
	fetchRaw = func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("network down")
	}
	t.Cleanup(func() { fetchRaw = prev })

	_, err := ddgSearch(t.Context(), "q")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network down")
}

func TestDDGSearchNoResults(t *testing.T) {
	prev := fetchRaw
	fetchRaw = func(context.Context, string, string) ([]byte, error) {
		return []byte(`<html><body>nothing here</body></html>`), nil
	}
	t.Cleanup(func() { fetchRaw = prev })

	got, err := ddgSearch(t.Context(), "obscure")
	require.NoError(t, err)
	assert.Equal(t, "No results for obscure", got)
}

func TestDDGSearchCapsResults(t *testing.T) {
	var page strings.Builder
	for i := range maxResults + 5 {
		page.WriteString(`<a class="result__a" href="https://e.example/`)
		page.WriteByte(byte('a' + i))
		page.WriteString(`">Hit</a>`)
	}
	prev := fetchRaw
	fetchRaw = func(context.Context, string, string) ([]byte, error) {
		return []byte(page.String()), nil
	}
	t.Cleanup(func() { fetchRaw = prev })

	got, err := ddgSearch(t.Context(), "many")
	require.NoError(t, err)
	assert.Contains(t, got, "Search: many")
	assert.Equal(t, maxResults, strings.Count(got, "https://e.example/"))
}

func TestDDGSearchIncludesSnippet(t *testing.T) {
	prev := fetchRaw
	fetchRaw = func(context.Context, string, string) ([]byte, error) {
		return []byte(`<a class="result__a" href="https://go.dev">Go</a>` +
			`<a class="result__snippet">Build fast things.</a>`), nil
	}
	t.Cleanup(func() { fetchRaw = prev })

	got, err := ddgSearch(t.Context(), "go")
	require.NoError(t, err)
	assert.Contains(t, got, "1. Go")
	assert.Contains(t, got, "Build fast things.")
}

func TestParsePlainLinksFallback(t *testing.T) {
	// No result__a anchors, so parseDDG falls back to bare URL scraping.
	page := `visit https://one.example/a and https://one.example/a again,
	plus https://duckduckgo.com/skip and https://two.example/b);`
	hits := parseDDG(page)
	require.Len(t, hits, 2)
	assert.Equal(t, "https://one.example/a", hits[0].link)
	assert.Equal(t, hits[0].link, hits[0].title)
	assert.Equal(t, "https://two.example/b", hits[1].link)
}

func TestParsePlainLinksCaps(t *testing.T) {
	var page strings.Builder
	for i := range maxResults + 6 {
		page.WriteString(" https://e.example/")
		page.WriteByte(byte('a' + i))
	}
	hits := parsePlainLinks(page.String())
	assert.Len(t, hits, maxResults)
}

func TestParseDDGSkipsUndecodableLink(t *testing.T) {
	page := `<a class="result__a" href="javascript:void(0)">Bad</a>` +
		`<a class="result__a" href="https://ok.example">Good</a>`
	hits := parseDDG(page)
	require.Len(t, hits, 1)
	assert.Equal(t, "https://ok.example", hits[0].link)
}

func TestDecodeDDGLink(t *testing.T) {
	cases := map[string]string{
		"//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev": "https://go.dev",
		"https://plain.example/x":                       "https://plain.example/x",
		"http://plain.example/x":                        "http://plain.example/x",
		"//cdn.example/x":                               "https://cdn.example/x",
		"javascript:void(0)":                            "",
		"/relative/path":                                "",
	}
	for in, want := range cases {
		assert.Equal(t, want, decodeDDGLink(in), in)
	}
}

func TestStripTags(t *testing.T) {
	assert.Equal(t, "bold text", stripTags("<b>bold</b> <i>text</i>"))
}

func TestTrimErr(t *testing.T) {
	assert.Empty(t, trimErr(nil, 10))
	assert.Equal(t, "a b", trimErr(errors.New(" a\nb "), 10))
	assert.Equal(t, "abcde", trimErr(errors.New("abcdefghij"), 5))
}
