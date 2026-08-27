package auth

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
)

const (
	testAntigravityClientID     = "test-client-id"
	testAntigravityClientSecret = "test-client-secret"
)

func TestMain(m *testing.M) {
	if err := os.Setenv(antigravityClientIDEnv, testAntigravityClientID); err != nil {
		panic(err)
	}
	if err := os.Setenv(antigravityClientSecretEnv, testAntigravityClientSecret); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

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
	assert.Equal(t, testAntigravityClientID, q.Get("client_id"))
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

func TestAntigravityClientCredentialsRequireVariables(t *testing.T) {
	t.Setenv(antigravityClientIDEnv, "")
	t.Setenv(antigravityClientSecretEnv, "")

	_, _, err := antigravityClientCredentials()
	require.Error(t, err)
	assert.ErrorContains(t, err, antigravityClientIDEnv)
	assert.ErrorContains(t, err, antigravityClientSecretEnv)
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

// stubTokenEndpoint points the code exchange at a local server and returns the
// form it received.
func stubTokenEndpoint(t *testing.T, reply string) *url.Values {
	t.Helper()
	got := &url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		*got = form
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)

	old := antigravityTokenURL
	antigravityTokenURL = srv.URL
	t.Cleanup(func() { antigravityTokenURL = old })
	return got
}

// login runs LoginAntigravity in the background on a free callback port.
//
// It hands back the consent URL's state and a function to await the result, so
// each test can drive the browser redirect or the paste without repeating the
// goroutine plumbing.
type loginRun struct {
	port  int
	state string
	wait  func() (Credential, error)
	paste chan string
}

func startLogin(t *testing.T) *loginRun {
	t.Helper()

	port := freePort(t)
	old := antigravityCallbackPort
	antigravityCallbackPort = port
	t.Cleanup(func() { antigravityCallbackPort = old })

	run := &loginRun{port: port, paste: make(chan string, 1)}
	urlCh := make(chan string, 1)
	type outcome struct {
		cred Credential
		err  error
	}
	done := make(chan outcome, 1)

	go func() {
		cred, err := LoginAntigravity(t.Context(), LoginOpts{
			OnURL: func(u string) { urlCh <- u },
			Paste: run.paste,
		})
		done <- outcome{cred, err}
	}()

	select {
	case u := <-urlCh:
		parsed, err := url.Parse(u)
		require.NoError(t, err)
		run.state = parsed.Query().Get("state")
		require.NotEmpty(t, run.state, "the consent URL must carry state")
	case <-time.After(5 * time.Second):
		t.Fatal("the login never produced a consent URL")
	}

	run.wait = func() (Credential, error) {
		select {
		case o := <-done:
			return o.cred, o.err
		case <-time.After(5 * time.Second):
			t.Fatal("the login never returned")
			return Credential{}, nil
		}
	}
	return run
}

// redirect is what the browser does after consent.
func (r *loginRun) redirect(t *testing.T, query string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d%s?%s", r.port, antigravityCallbackPath, query), http.NoBody)
	require.NoError(t, err)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

// The point of the callback server: a local browser finishes the login by
// itself, with nothing to copy.
func TestLoginAntigravityCompletesFromTheRedirect(t *testing.T) {
	form := stubTokenEndpoint(t, `{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
	run := startLogin(t)

	run.redirect(t, "code=the-code&state="+run.state)

	cred, err := run.wait()
	require.NoError(t, err)
	assert.Equal(t, "at", cred.AccessToken)
	assert.Equal(t, "rt", cred.RefreshToken)
	assert.Equal(t, "the-code", form.Get("code"))
	assert.Equal(t, antigravityRedirectURI, form.Get("redirect_uri"),
		"the exchange must repeat the registered redirect URI")
	assert.NotEmpty(t, form.Get("code_verifier"), "PKCE must be completed")
}

// State is what stops a redirect from another login being accepted.
func TestLoginAntigravityRejectsAForeignState(t *testing.T) {
	stubTokenEndpoint(t, `{"access_token":"at"}`)
	run := startLogin(t)

	run.redirect(t, "code=c&state=not-the-state")

	_, err := run.wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different login")
}

// Over SSH the browser runs elsewhere and its redirect to localhost cannot
// reach this process, so paste has to keep working.
func TestLoginAntigravityStillAcceptsAPaste(t *testing.T) {
	form := stubTokenEndpoint(t, `{"access_token":"at","expires_in":3600}`)
	run := startLogin(t)

	run.paste <- antigravityRedirectURI + "?code=pasted&state=" + run.state

	cred, err := run.wait()
	require.NoError(t, err)
	assert.Equal(t, "at", cred.AccessToken)
	assert.Equal(t, "pasted", form.Get("code"))
}

// A user who cancels at the consent screen must be told why, not left waiting.
func TestLoginAntigravityReportsARefusal(t *testing.T) {
	run := startLogin(t)

	run.redirect(t, "error=access_denied&error_description=denied+by+user")

	_, err := run.wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied by user")
}

// The callback port is fixed by the registered redirect URI, so another alpha
// holding it must not break the login: paste still finishes it.
func TestLoginAntigravityFallsBackWhenThePortIsTaken(t *testing.T) {
	form := stubTokenEndpoint(t, `{"access_token":"at"}`)

	port := freePort(t)
	old := antigravityCallbackPort
	antigravityCallbackPort = port
	t.Cleanup(func() { antigravityCallbackPort = old })

	var lc net.ListenConfig
	blocker, err := lc.Listen(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer func() { _ = blocker.Close() }()

	paste := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, lerr := LoginAntigravity(t.Context(), LoginOpts{
			OnURL: func(string) { paste <- "bare-code-abc" },
			Paste: paste,
		})
		done <- lerr
	}()

	select {
	case lerr := <-done:
		require.NoError(t, lerr, "a taken port must not fail the login")
	case <-time.After(5 * time.Second):
		t.Fatal("the login never returned")
	}
	assert.Equal(t, "bare-code-abc", form.Get("code"), "a bare code must be accepted")
}
