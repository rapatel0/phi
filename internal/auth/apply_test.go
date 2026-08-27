package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
)

func TestApplyFillsAnthropicKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	st := OpenStore(path)
	require.NoError(t, st.Put(Credential{Provider: ProviderAnthropic, AccessToken: "sk-ant-oat-x"}))

	cfg := llm.ModelConfig{Name: "claude-sonnet-4-5"}
	require.NoError(t, Apply(t.Context(), &cfg, path))
	require.Equal(t, "sk-ant-oat-x", cfg.APIKey)
	require.Equal(t, "https://api.anthropic.com", cfg.BaseURL)
}

func TestApplyLeavesExplicitKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	st := OpenStore(path)
	require.NoError(t, st.Put(Credential{Provider: ProviderAnthropic, AccessToken: "sk-ant-oat-x"}))

	cfg := llm.ModelConfig{Name: "claude-sonnet-4-5", APIKey: "sk-ant-api03-keep"}
	require.NoError(t, Apply(t.Context(), &cfg, path))
	require.Equal(t, "sk-ant-api03-keep", cfg.APIKey)
}

// apply stores whatever a refresh returns, so a provider that reports success
// without a token would replace a working credential with a broken one.
//
// Every provider today rejects that itself, which is why this asserts the
// backstop directly: the guard exists so a provider added later cannot log the
// user out by returning an empty credential and a nil error.
// A provider that reports success with no token would otherwise have its empty
// credential stored over the working one, which reads to the user as a logout.
//
// Every provider compiled in today makes this check itself, so a stand-in is
// needed to reach the guard. It is there for the next provider that forgets.
func TestRefreshRejectsAnEmptyTokenFromAnyProvider(t *testing.T) {
	refreshHook = func(context.Context, string, string) (Credential, error) {
		return Credential{RefreshToken: "r2"}, nil
	}
	t.Cleanup(func() { refreshHook = nil })

	got, err := refresh(t.Context(), ProviderXAI, "r1")
	require.Error(t, err, "success with no token must not reach the caller")
	require.Empty(t, got.AccessToken)
}

// An unknown provider has nothing to refresh, so the guard must not turn that
// into an error and break a config alpha does not manage.
func TestRefreshAllowsAnEmptyTokenForNoProvider(t *testing.T) {
	got, err := refresh(t.Context(), "", "r1")
	require.NoError(t, err)
	require.Empty(t, got.AccessToken)
}

func TestRefreshRejectsAnEmptyToken(t *testing.T) {
	// Anthropic is used because its refresh reports success without its own
	// empty-token check. That is what makes this exercise the guard in
	// refresh rather than a provider's own.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"","refresh_token":"r2","expires_in":3600}`)
	}))
	t.Cleanup(srv.Close)

	old := anthropicTokenURL
	anthropicTokenURL = srv.URL
	t.Cleanup(func() { anthropicTokenURL = old })

	got, err := refresh(t.Context(), ProviderAnthropic, "r1")
	require.Error(t, err, "an empty token must never read as success")
	require.Empty(t, got.AccessToken)
}

// Every provider must refuse an empty token, whichever check catches it. The
// caller stores what comes back, so success with no token replaces a working
// credential with a broken one.
func TestRefreshRejectsAnEmptyTokenForEveryProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"","refresh_token":"r2","expires_in":3600}`)
	}))
	t.Cleanup(srv.Close)

	for _, p := range []struct {
		provider string
		url      *string
	}{
		{ProviderAnthropic, &anthropicTokenURL},
		{ProviderXAI, &xaiTokenURL},
		{ProviderCodex, &codexTokenURL},
	} {
		t.Run(p.provider, func(t *testing.T) {
			old := *p.url
			*p.url = srv.URL
			t.Cleanup(func() { *p.url = old })

			_, err := refresh(t.Context(), p.provider, "r1")
			assert.Error(t, err, "%s must refuse an empty token", p.provider)
		})
	}
}

// The stored credential must survive a refresh that fails, whatever the
// reason: a failed refresh is a temporary condition, and discarding the token
// turns it into a logout.
func TestApplyKeepsTheTokenWhenRefreshFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	old := xaiTokenURL
	xaiTokenURL = srv.URL
	t.Cleanup(func() { xaiTokenURL = old })

	path := filepath.Join(t.TempDir(), "auth.json")
	st := OpenStore(path)
	require.NoError(t, st.Put(Credential{
		Provider:     ProviderXAI,
		AccessToken:  "still-good",
		RefreshToken: "r1",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}))

	cfg := &llm.ModelConfig{Name: "grok-4"}
	require.Error(t, Apply(t.Context(), cfg, path))

	got, ok := OpenStore(path).Get(ProviderXAI)
	require.True(t, ok, "the credential must not be deleted")
	require.Equal(t, "still-good", got.AccessToken)
}

// An unknown provider has nothing to refresh, and must not be reported as a
// provider that failed.
func TestRefreshIgnoresAnUnknownProvider(t *testing.T) {
	cred, err := refresh(t.Context(), "", "r1")
	require.NoError(t, err)
	require.Empty(t, cred.AccessToken)
}
