package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/llm"
)

// storeWith writes credentials to a temporary auth file.
func storeWith(t *testing.T, creds ...auth.Credential) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	st := auth.OpenStore(path)
	for _, c := range creds {
		require.NoError(t, st.Put(c))
	}
	return path
}

// noAntigravityEnv removes the antigravity OAuth client credentials, which is
// the state of a machine that never configured that provider.
func noAntigravityEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ALPHA_ANTIGRAVITY_CLIENT_ID", "")
	t.Setenv("ALPHA_ANTIGRAVITY_CLIENT_SECRET", "")
}

// A provider the user never configured must not take down the ones they did.
//
// An expired antigravity credential used to abort the whole config load, so
// alpha refused to start and named a provider the user was not trying to use.
func TestOneUnusableProviderDoesNotBreakTheOthers(t *testing.T) {
	noAntigravityEnv(t)
	authFile := storeWith(t,
		auth.Credential{
			Provider: auth.ProviderAnthropic, AccessToken: "good",
			ExpiresAt: time.Now().Add(time.Hour),
		},
		// Expired with a refresh token, so it tries to refresh and fails for
		// want of the client credentials.
		auth.Credential{
			Provider: auth.ProviderAntigravity, AccessToken: "old", RefreshToken: "r1",
			ExpiresAt: time.Now().Add(-time.Hour),
		},
	)

	cfg := &Config{Models: []llm.ModelConfig{
		{Name: "claude-sonnet-4-6"},
		{Name: "antigravity-gemini-3-pro"},
	}}
	applyOAuthKeys(cfg, authFile)

	assert.Equal(t, "good", cfg.Models[0].APIKey, "the working provider must keep its key")
	assert.Empty(t, cfg.Models[1].APIKey, "the unusable one gets no key, and that is all")
}

// The failure belongs at the model that cannot run, not at startup.
func TestAnUnusableProviderStillLoadsTheConfig(t *testing.T) {
	noAntigravityEnv(t)
	p := discoverInTempHome(t)
	writeModelConfig(t, p.Global().ConfigFile())

	st := auth.OpenStore(p.Global().AuthFile())
	require.NoError(t, st.Put(auth.Credential{
		Provider: auth.ProviderAntigravity, AccessToken: "old", RefreshToken: "r1",
		ExpiresAt: time.Now().Add(-time.Hour),
	}))

	require.NoError(t, p.LoadConfig(), "an unusable provider must not stop startup")
	require.NotNil(t, p.Config())
	assert.NotEmpty(t, p.Config().Models)
}

// A working credential must still reach its model. The fix must not turn every
// OAuth failure into silence.
func TestAStoredCredentialReachesItsModel(t *testing.T) {
	authFile := storeWith(t, auth.Credential{
		Provider: auth.ProviderAnthropic, AccessToken: "sk-ant-live",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	cfg := &Config{Models: []llm.ModelConfig{{Name: "claude-sonnet-4-6"}}}
	applyOAuthKeys(cfg, authFile)

	assert.Equal(t, "sk-ant-live", cfg.Models[0].APIKey)
}

// A key written in config.yaml wins, or a stale stored credential would
// silently replace what the user typed.
func TestAnExplicitKeyIsNotOverwritten(t *testing.T) {
	authFile := storeWith(t, auth.Credential{
		Provider: auth.ProviderAnthropic, AccessToken: "from-store",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	cfg := &Config{Models: []llm.ModelConfig{{Name: "claude-sonnet-4-6", APIKey: "from-config"}}}
	applyOAuthKeys(cfg, authFile)

	assert.Equal(t, "from-config", cfg.Models[0].APIKey)
}

// Catalog injection lists providers alphabetically. antigravity sorts before
// codex, so an expired antigravity login used to become the implicit default
// and block a working Codex login.
func TestALaterProviderStillStartsWhenAntigravityIsUnusable(t *testing.T) {
	noAntigravityEnv(t)
	p := discoverInTempHome(t)
	st := auth.OpenStore(p.Global().AuthFile())
	require.NoError(t, st.Put(auth.Credential{
		Provider: auth.ProviderCodex, AccessToken: "codex-live",
		ExpiresAt: time.Now().Add(time.Hour),
	}))
	require.NoError(t, st.Put(auth.Credential{
		Provider: auth.ProviderAntigravity, AccessToken: "old", RefreshToken: "r1",
		ExpiresAt: time.Now().Add(-time.Hour),
	}))

	require.NoError(t, p.LoadConfig(), "a working Codex login must start")
	got := p.Config().Model()
	assert.Equal(t, "codex-live", got.APIKey)
	assert.NotContains(t, got.Name, "antigravity")
}

// An explicit default that cannot run must still fail. Falling back would hide
// that the selected model has no credential.
func TestAnExplicitUnusableDefaultStillFails(t *testing.T) {
	noAntigravityEnv(t)
	p := discoverInTempHome(t)
	writeConfigFile(t, p.Global().ConfigFile(),
		"models:\n  - name: antigravity-gemini-3.1-pro\n    default: true\n  - name: claude-sonnet-4-6\n")
	st := auth.OpenStore(p.Global().AuthFile())
	require.NoError(t, st.Put(auth.Credential{
		Provider: auth.ProviderAnthropic, AccessToken: "good",
		ExpiresAt: time.Now().Add(time.Hour),
	}))
	require.NoError(t, st.Put(auth.Credential{
		Provider: auth.ProviderAntigravity, AccessToken: "old", RefreshToken: "r1",
		ExpiresAt: time.Now().Add(-time.Hour),
	}))

	err := p.LoadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key")
}

// Selecting a model that has no key must fail with that model's name. The
// error moved from startup to here, and it has to still exist.
func TestAModelWithNoKeyIsReported(t *testing.T) {
	noAntigravityEnv(t)
	p := discoverInTempHome(t)
	writeConfigFile(t, p.Global().ConfigFile(),
		"models:\n  - name: antigravity-gemini-3-pro\n")

	err := p.LoadConfig()
	require.Error(t, err, "a default model with no key cannot run")
	assert.Contains(t, err.Error(), "api_key")
}

// writeConfigFile writes a config.yaml body.
func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}
