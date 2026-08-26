package profile_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/profile"
)

// The point of profiles: two logins for the same provider coexist instead of
// overwriting each other.
func TestCredentialsAreIsolatedPerProfile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, profile.Create(root, "work"))

	def := auth.OpenStore(profile.AuthFile(root, profile.Default))
	work := auth.OpenStore(profile.AuthFile(root, "work"))

	require.NoError(t, def.Put(auth.Credential{Provider: "gemini", AccessToken: "personal-key"}))
	require.NoError(t, work.Put(auth.Credential{Provider: "gemini", AccessToken: "work-key"}))

	got, ok := def.Get("gemini")
	require.True(t, ok)
	assert.Equal(t, "personal-key", got.AccessToken)

	got, ok = work.Get("gemini")
	require.True(t, ok)
	assert.Equal(t, "work-key", got.AccessToken, "the work login must not be overwritten")
}

// A profile with no login must not fall back to another profile's credentials.
func TestAnEmptyProfileIsNotLoggedIn(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, profile.Create(root, "empty"))

	require.NoError(t, auth.OpenStore(profile.AuthFile(root, profile.Default)).
		Put(auth.Credential{Provider: "gemini", AccessToken: "k"}))

	_, ok := auth.OpenStore(profile.AuthFile(root, "empty")).Get("gemini")
	assert.False(t, ok)
}

// Deleting a profile must take its credentials with it, or a deleted work
// account stays readable on disk.
func TestDeleteRemovesStoredTokens(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, profile.Create(root, "work"))
	require.NoError(t, auth.OpenStore(profile.AuthFile(root, "work")).
		Put(auth.Credential{Provider: "anthropic", AccessToken: "secret-token"}))

	require.NoError(t, profile.Delete(root, "work"))
	assert.NoFileExists(t, filepath.Join(root, "profiles", "work", "auth.json"))
}
