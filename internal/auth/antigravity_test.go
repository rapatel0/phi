package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
)

// Google issues a refresh token only when offline access is asked for and
// consent is forced. Without both, the login lasts an hour.
func TestAntigravityAuthURLAsksForOfflineAccess(t *testing.T) {
	u, err := url.Parse(AntigravityAuthURL("challenge", "state"))
	require.NoError(t, err)
	q := u.Query()

	assert.Equal(t, "accounts.google.com", u.Host)
	assert.Equal(t, "offline", q.Get("access_type"))
	assert.Equal(t, "consent", q.Get("prompt"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, "challenge", q.Get("code_challenge"))
	assert.Equal(t, "state", q.Get("state"))
	assert.Equal(t, antigravityRedirectURI, q.Get("redirect_uri"))
}

// Asking for fewer scopes gets a token the endpoint rejects.
func TestAntigravityAuthURLRequestsEveryScope(t *testing.T) {
	u, err := url.Parse(AntigravityAuthURL("c", "s"))
	require.NoError(t, err)
	scopes := strings.Fields(u.Query().Get("scope"))
	assert.ElementsMatch(t, antigravityScopes, scopes)
	assert.Contains(t, scopes, "https://www.googleapis.com/auth/cloud-platform")
	assert.Contains(t, scopes, "https://www.googleapis.com/auth/cclog")
}

// A user who copies the address bar and one who copies the code both expect it
// to work.
func TestParseAntigravityRedirectAcceptsBothForms(t *testing.T) {
	code, state, err := parseAntigravityRedirect(
		"http://localhost:51121/oauth-callback?code=abc123&state=xyz&scope=email")
	require.NoError(t, err)
	assert.Equal(t, "abc123", code)
	assert.Equal(t, "xyz", state)

	code, state, err = parseAntigravityRedirect("  bare-code-value  ")
	require.NoError(t, err)
	assert.Equal(t, "bare-code-value", code)
	assert.Empty(t, state, "a bare code carries no state to check")
}

func TestParseAntigravityRedirectReportsRefusal(t *testing.T) {
	_, _, err := parseAntigravityRedirect("http://localhost:51121/oauth-callback?error=access_denied")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_denied")
}

func TestParseAntigravityRedirectRejectsEmptyAndCodeless(t *testing.T) {
	_, _, err := parseAntigravityRedirect("   ")
	assert.Error(t, err)

	_, _, err = parseAntigravityRedirect("http://localhost:51121/oauth-callback?state=xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no code")
}

// Antigravity serves Claude and Gemini models, so it must be recognized before
// the rules for those providers or the wrong token is sent.
func TestProviderForRecognizesAntigravityFirst(t *testing.T) {
	cases := []struct{ name, base string }{
		{"antigravity-claude-opus-4-6-thinking", ""},
		{"antigravity-gemini-3.1-pro", ""},
		{"anything", antigravityBase},
		// The cases that matter: a bare model name on the antigravity
		// endpoint. Checked in the wrong order these match the
		// Anthropic and Gemini rules, and that provider's token is
		// sent to Google.
		{"claude-opus-4-6-thinking", antigravityBase},
		{"gemini-3.1-pro", antigravityBase},
	}
	for _, c := range cases {
		got := ProviderFor(llm.ModelConfig{Name: c.name, BaseURL: c.base})
		assert.Equal(t, ProviderAntigravity, got, "name=%q base=%q", c.name, c.base)
	}
}

// The ordinary providers must be unaffected by that new rule.
func TestProviderForKeepsTheOtherProviders(t *testing.T) {
	assert.Equal(t, ProviderAnthropic, ProviderFor(llm.ModelConfig{Name: "claude-sonnet-5"}))
	assert.Equal(t, ProviderGemini, ProviderFor(llm.ModelConfig{Name: "gemini-2.5-pro"}))
	assert.Equal(t, ProviderXAI, ProviderFor(llm.ModelConfig{Name: "grok-4.6"}))
	assert.Equal(t, ProviderCodex, ProviderFor(llm.ModelConfig{Name: "gpt-5.5"}))
}

// A login with no models to select would be useless.
func TestAntigravityCatalogIsNotEmpty(t *testing.T) {
	got := Catalog(ProviderAntigravity)
	require.NotEmpty(t, got)
	for _, m := range got {
		assert.True(t, strings.HasPrefix(m.Name, "antigravity-"),
			"%q must be prefixed so it cannot collide with a public model", m.Name)
		assert.Equal(t, antigravityBase, m.BaseURL)
		assert.Positive(t, m.ContextWindow)
	}
}

// Every catalog entry must route to the antigravity transport, or selecting
// one sends an Antigravity token to the public Gemini API.
func TestAntigravityCatalogRoutesToItsOwnProvider(t *testing.T) {
	for _, m := range Catalog(ProviderAntigravity) {
		assert.Equal(t, ProviderAntigravity, ProviderFor(m), "%q", m.Name)
	}
}

// The embedded application credentials must decode to what Antigravity ships,
// or every request is refused.
func TestAntigravityClientCredentialsDecode(t *testing.T) {
	assert.Equal(t,
		"test-client-id",
		antigravityClientID)
	assert.True(t, strings.HasPrefix(antigravityClientSecret, "GOCSPX-"),
		"the secret must decode to a Google client secret")
}

// A refresh response usually omits the refresh token. Dropping it would log
// the user out at the next expiry.
func TestRefreshAntigravityKeepsTheRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		assert.Equal(t, "refresh_token", form.Get("grant_type"))
		assert.Equal(t, "old-refresh", form.Get("refresh_token"))
		w.Header().Set("Content-Type", "application/json")
		// No refresh_token in the reply, which is the usual case.
		_, _ = io.WriteString(w, `{"access_token":"fresh","expires_in":3600}`)
	}))
	defer srv.Close()

	old := antigravityTokenURL
	antigravityTokenURL = srv.URL
	t.Cleanup(func() { antigravityTokenURL = old })

	cred, err := RefreshAntigravity(t.Context(), "old-refresh")
	require.NoError(t, err)
	assert.Equal(t, "fresh", cred.AccessToken)
	assert.Equal(t, "old-refresh", cred.RefreshToken, "the refresh token must be carried over")
	assert.Equal(t, ProviderAntigravity, cred.Provider)
	assert.False(t, cred.Expired(), "a one-hour token must not read as expired")
}

// This is the failure this provider is expected to reach: Google retires the
// borrowed application. Telling the user to log in again would not help, so it
// must be reported as its own condition.
func TestRefreshAntigravityReportsWithdrawal(t *testing.T) {
	for _, code := range []string{"invalid_client", "unauthorized_client"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"`+code+`"}`)
		}))

		old := antigravityTokenURL
		antigravityTokenURL = srv.URL

		_, err := RefreshAntigravity(t.Context(), "r")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnavailable, "%s must report withdrawal", code)

		antigravityTokenURL = old
		srv.Close()
	}
}

// An expired grant is an ordinary failure: logging in again fixes it, so it
// must not be reported as the provider being gone.
func TestRefreshAntigravityKeepsOrdinaryErrorsDistinct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
	}))
	defer srv.Close()

	old := antigravityTokenURL
	antigravityTokenURL = srv.URL
	t.Cleanup(func() { antigravityTokenURL = old })

	_, err := RefreshAntigravity(t.Context(), "r")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrUnavailable)
	assert.Contains(t, err.Error(), "expired or revoked")
}
