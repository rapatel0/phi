package webfetchtool

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allowTestServer points the SSRF allowlist and the client at a loopback server.
func allowTestServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	host, _, _ := strings.Cut(strings.TrimPrefix(srv.URL, "https://"), ":")
	prevHosts, prevClient := allowPrivateHosts, fetchClientOverride
	allowPrivateHosts = []string{host, "127.0.0.1", "::1"}
	fetchClientOverride = srv.Client()
	t.Cleanup(func() {
		allowPrivateHosts = prevHosts
		fetchClientOverride = prevClient
	})
}

func TestWebFetchToolSchemaFields(t *testing.T) {
	tl := WebFetchTool()
	require.NotNil(t, tl.Definition.Params)
	assert.Equal(t, []string{"url"}, tl.Definition.Params.Required)
	assert.Contains(t, tl.Definition.Params.Properties, "url")
	require.NotNil(t, tl.DetailFromArgs)
	assert.Equal(t, "https://example.com/a", tl.DetailFromArgs(json.RawMessage(`{"url":" https://example.com/a "}`)))
	assert.Empty(t, tl.DetailFromArgs(json.RawMessage(`not json`)))
}

func TestRunWebFetchBadArgs(t *testing.T) {
	_, err := runWebFetch(t.Context(), json.RawMessage(`{`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webfetch args")
}

func TestFetchRawRejectsEmptyHost(t *testing.T) {
	_, err := FetchRaw(t.Context(), "https://")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no host")
}

func TestFetchTextStripsHTML(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<html><body><p>Doc &amp; text</p></body></html>`)
	}))
	t.Cleanup(srv.Close)
	allowTestServer(t, srv)

	got, err := FetchText(t.Context(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "Doc & text", got)
}

func TestFetchTextTruncatesLongBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("ab ", maxTextRunes))
	}))
	t.Cleanup(srv.Close)
	allowTestServer(t, srv)

	got, err := FetchText(t.Context(), srv.URL)
	require.NoError(t, err)
	assert.Contains(t, got, "[truncated]")
	assert.Less(t, len([]rune(got)), maxTextRunes+32)
}

func TestFetchRawCapsBodySize(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxFetchBytes+4096))
	}))
	t.Cleanup(srv.Close)
	allowTestServer(t, srv)

	body, err := FetchRaw(t.Context(), srv.URL)
	require.NoError(t, err)
	assert.Len(t, body, maxFetchBytes)
}

func TestFetchRawNonSuccessStatus(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	allowTestServer(t, srv)

	_, err := FetchRaw(t.Context(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http 404")
}

func TestFetchRawWithUADefaultsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	allowTestServer(t, srv)

	_, err := FetchRawWithUA(t.Context(), srv.URL, "   ")
	require.NoError(t, err)
	assert.Equal(t, fetchUserAgent, gotUA)
}

func TestFetchRawWithUACustomUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	allowTestServer(t, srv)

	_, err := FetchRawWithUA(t.Context(), srv.URL, "custom-agent/9")
	require.NoError(t, err)
	assert.Equal(t, "custom-agent/9", gotUA)
}

func TestFetchRawUnresolvableHost(t *testing.T) {
	_, err := FetchRaw(t.Context(), "https://nonexistent.invalid/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve")
}

func TestRejectPrivateHost(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		wantErr string
	}{
		{"empty", "", "no host"},
		{"loopback ip", "127.0.0.1", "private or local"},
		{"private ip", "10.0.0.5", "private or local"},
		{"link local", "169.254.1.1", "private or local"},
		{"unspecified", "0.0.0.0", "private or local"},
		{"ipv6 loopback", "::1", "private or local"},
		{"public ip", "8.8.8.8", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := rejectPrivateHost(t.Context(), c.host)
			if c.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantErr)
		})
	}
}

func TestRejectPrivateHostAllowlisted(t *testing.T) {
	prev := allowPrivateHosts
	allowPrivateHosts = []string{"127.0.0.1"}
	t.Cleanup(func() { allowPrivateHosts = prev })

	require.NoError(t, rejectPrivateHost(t.Context(), "127.0.0.1"))
}

func TestRejectPrivateIPNil(t *testing.T) {
	err := rejectPrivateIP(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil ip")
}

func TestIsAllowedPrivateHost(t *testing.T) {
	prev := allowPrivateHosts
	allowPrivateHosts = []string{"", "10.1.2.3"}
	t.Cleanup(func() { allowPrivateHosts = prev })

	assert.True(t, isAllowedPrivateHost(net.ParseIP("10.1.2.3")))
	assert.False(t, isAllowedPrivateHost(net.ParseIP("10.1.2.4")))
}

func TestCheckRedirect(t *testing.T) {
	policy := checkRedirect(t.Context())

	mustReq := func(rawURL string) *http.Request {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, http.NoBody)
		require.NoError(t, err)
		return req
	}

	t.Run("allows public https", func(t *testing.T) {
		require.NoError(t, policy(mustReq("https://8.8.8.8/next"), nil))
	})

	t.Run("rejects plain http", func(t *testing.T) {
		err := policy(mustReq("http://example.com/next"), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not https")
	})

	t.Run("rejects private host", func(t *testing.T) {
		err := policy(mustReq("https://127.0.0.1/next"), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "private")
	})

	t.Run("rejects redirect chains", func(t *testing.T) {
		via := make([]*http.Request, 5)
		err := policy(mustReq("https://8.8.8.8/next"), via)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too many redirects")
	})
}

func TestFetchRawRefusesRedirectOffHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/leak", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	host, _, _ := strings.Cut(strings.TrimPrefix(srv.URL, "https://"), ":")
	prevHosts, prevClient := allowPrivateHosts, fetchClientOverride
	allowPrivateHosts = []string{host, "127.0.0.1", "::1"}
	// Same client as production, so the real redirect policy is exercised.
	c := *srv.Client()
	c.CheckRedirect = checkRedirect(t.Context())
	fetchClientOverride = &c
	t.Cleanup(func() {
		allowPrivateHosts = prevHosts
		fetchClientOverride = prevClient
	})

	_, err := FetchRaw(t.Context(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not https")
}

func TestHtmlToTextCollapsesWhitespace(t *testing.T) {
	got := htmlToText("<p>one</p>\n\n\t<p>two</p>   <p>three</p>")
	assert.Equal(t, "one two three", got)
}
