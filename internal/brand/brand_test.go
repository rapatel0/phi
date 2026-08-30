package brand

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvPrefersCurrentName(t *testing.T) {
	t.Setenv("ALPHA_MODEL", "new")
	t.Setenv("PHI_MODEL", "old")
	assert.Equal(t, "new", Env("MODEL"))
}

// unsetEnv clears a variable for the test and restores it afterwards.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	// Setenv registers the cleanup that restores the original value.
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

func TestEnvFallsBackToLegacyName(t *testing.T) {
	unsetEnv(t, "ALPHA_MODEL")
	t.Setenv("PHI_MODEL", "old")
	assert.Equal(t, "old", Env("MODEL"))
}

func TestEnvEmptyCurrentNameWins(t *testing.T) {
	// An explicitly empty ALPHA_ value must not fall through to PHI_.
	t.Setenv("ALPHA_MODEL", "")
	t.Setenv("PHI_MODEL", "old")
	assert.Empty(t, Env("MODEL"))
}

func TestEnvUnset(t *testing.T) {
	unsetEnv(t, "ALPHA_MODEL")
	unsetEnv(t, "PHI_MODEL")
	assert.Empty(t, Env("MODEL"))
}

func TestEnvAcceptsFullNames(t *testing.T) {
	t.Setenv("ALPHA_MODEL", "new")
	assert.Equal(t, "new", Env("ALPHA_MODEL"))
	assert.Equal(t, "new", Env("PHI_MODEL"))
	assert.Equal(t, "new", Env("MODEL"))
}

func TestEnvLookup(t *testing.T) {
	unsetEnv(t, "ALPHA_DEBUG")
	unsetEnv(t, "PHI_DEBUG")
	_, ok := EnvLookup("DEBUG")
	assert.False(t, ok)

	t.Setenv("PHI_DEBUG", "")
	v, ok := EnvLookup("DEBUG")
	assert.True(t, ok, "an empty legacy value is still set")
	assert.Empty(t, v)
}

func TestEnvName(t *testing.T) {
	assert.Equal(t, "ALPHA_API_KEY", EnvName("API_KEY"))
	assert.Equal(t, "ALPHA_API_KEY", EnvName("PHI_API_KEY"))
	assert.Equal(t, "ALPHA_API_KEY", EnvName("ALPHA_API_KEY"))
}

func TestIsEnvKey(t *testing.T) {
	assert.True(t, IsEnvKey("ALPHA_API_KEY", "API_KEY"))
	assert.True(t, IsEnvKey("PHI_API_KEY", "API_KEY"))
	assert.True(t, IsEnvKey("ALPHA_API_KEY=sk-secret", "API_KEY"))
	assert.True(t, IsEnvKey("PHI_API_KEY=sk-secret", "API_KEY"))
	assert.False(t, IsEnvKey("ALPHA_MODEL", "API_KEY"))
	assert.False(t, IsEnvKey("HOME", "API_KEY"))
}

func TestHasEnvPrefix(t *testing.T) {
	assert.True(t, HasEnvPrefix("ALPHA_HOOK_EVENT"))
	assert.True(t, HasEnvPrefix("PHI_HOOK_EVENT"))
	assert.True(t, HasEnvPrefix("ALPHA_HOOK_EVENT=x"))
	assert.False(t, HasEnvPrefix("PATH"))
}

func TestDirHelpers(t *testing.T) {
	assert.Equal(t, filepath.Join("/home/u", ".alpha"), HomeDir("/home/u"))
	assert.Equal(t, filepath.Join("/home/u", ".phi"), LegacyHomeDir("/home/u"))
	assert.Equal(t, filepath.Join("/home/u", ".agents"), AgentsHome("/home/u"))
	assert.Equal(t, filepath.Join("/repo", ".alpha"), ProjectDir("/repo"))
	assert.Equal(t, filepath.Join("/repo", ".phi"), LegacyProjectDir("/repo"))
	assert.Equal(t, filepath.Join("/repo", ".agents"), AgentsProject("/repo"))
	assert.Equal(t, []string{
		filepath.Join("/home/u", ".claude"),
		filepath.Join("/home/u", ".codex"),
		filepath.Join("/home/u", ".grok"),
	}, PeerHomes("/home/u"))
	assert.Equal(t, filepath.Join("/home/u", ".claude", "skills"), PeerJoin("/home/u", "skills")[0])
}

func TestMigrateMovesLegacyDir(t *testing.T) {
	home := t.TempDir()
	legacy := LegacyHomeDir(home)
	require.NoError(t, os.MkdirAll(filepath.Join(legacy, "session"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "auth.json"), []byte(`{"t":1}`), 0o600))

	res, err := Migrate(home)
	require.NoError(t, err)
	assert.True(t, res.Moved)

	body, err := os.ReadFile(filepath.Join(HomeDir(home), "auth.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"t":1}`, string(body))
	assert.DirExists(t, filepath.Join(HomeDir(home), "session"))
	assert.NoDirExists(t, legacy)
}

func TestMigrateNoLegacyDir(t *testing.T) {
	home := t.TempDir()
	res, err := Migrate(home)
	require.NoError(t, err)
	assert.False(t, res.Moved)
	assert.NoDirExists(t, HomeDir(home))
}

func TestMigrateKeepsExistingCurrentDir(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(LegacyHomeDir(home), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(LegacyHomeDir(home), "auth.json"), []byte("old"), 0o600))
	require.NoError(t, os.MkdirAll(HomeDir(home), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(HomeDir(home), "auth.json"), []byte("new"), 0o600))

	res, err := Migrate(home)
	require.NoError(t, err)
	assert.False(t, res.Moved, "must not merge over existing state")

	body, err := os.ReadFile(filepath.Join(HomeDir(home), "auth.json"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(body))
	assert.DirExists(t, LegacyHomeDir(home), "legacy dir stays for manual recovery")
}

func TestMigrateIgnoresSymlinkedLegacyDir(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	require.NoError(t, os.Symlink(target, LegacyHomeDir(home)))

	res, err := Migrate(home)
	require.NoError(t, err)
	assert.False(t, res.Moved)
	assert.NoDirExists(t, HomeDir(home))

	fi, err := os.Lstat(LegacyHomeDir(home))
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "symlink must survive untouched")
}

func TestMigrateIgnoresLegacyFile(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.WriteFile(LegacyHomeDir(home), []byte("not a dir"), 0o600))

	res, err := Migrate(home)
	require.NoError(t, err)
	assert.False(t, res.Moved)
}

func TestMigrateIsIdempotent(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(LegacyHomeDir(home), 0o700))

	first, err := Migrate(home)
	require.NoError(t, err)
	assert.True(t, first.Moved)

	second, err := Migrate(home)
	require.NoError(t, err)
	assert.False(t, second.Moved)
	assert.DirExists(t, HomeDir(home))
}
