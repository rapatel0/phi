package webfetchtool

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebFetchToolSchema(t *testing.T) {
	tl := WebFetchTool()
	assert.Equal(t, "webfetch", tl.Definition.Name)
	assert.True(t, tl.Definition.Readable)
}

func TestWebFetchRejectsHTTP(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"url": "http://example.com/"})
	_, err := runWebFetch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
}

func TestWebFetchRejectsLoopback(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"url": "https://127.0.0.1/"})
	_, err := runWebFetch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private")
}

func TestWebFetchHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><script>alert(1)</script><p>Hello <b>world</b></p></html>`)
	}))
	t.Cleanup(srv.Close)

	host, _, _ := strings.Cut(strings.TrimPrefix(srv.URL, "https://"), ":")
	prevHosts, prevClient := allowPrivateHosts, fetchClientOverride
	allowPrivateHosts = []string{host, "127.0.0.1", "::1"}
	fetchClientOverride = srv.Client()
	t.Cleanup(func() {
		allowPrivateHosts = prevHosts
		fetchClientOverride = prevClient
	})

	raw, err := json.Marshal(map[string]string{"url": srv.URL + "/doc"})
	require.NoError(t, err)
	out, err := runWebFetch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Hello world")
	assert.NotContains(t, out.Content, "alert")
	assert.Equal(t, srv.URL+"/doc", out.Detail)
}

func TestHtmlToTextStripsTags(t *testing.T) {
	got := htmlToText(`<style>x{}</style><script>y()</script><h1>Title</h1> &amp; more`)
	assert.Equal(t, "Title & more", got)
}
