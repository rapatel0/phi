package profile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustCreate makes a profile and fails the test if it cannot.
func mustCreate(t *testing.T, root, name string) {
	t.Helper()
	_, err := Create(root, name)
	require.NoError(t, err)
}

// An install that never uses profiles must keep its credentials where they
// were, or upgrading looks like being logged out.
func TestDefaultProfileKeepsTheOriginalPaths(t *testing.T) {
	root := t.TempDir()
	assert.Equal(t, root, Dir(root, Default))
	assert.Equal(t, root, Dir(root, ""))
	assert.Equal(t, filepath.Join(root, "auth.json"), AuthFile(root, Default))
}

func TestNamedProfileGetsItsOwnDirectory(t *testing.T) {
	root := t.TempDir()
	assert.Equal(t, filepath.Join(root, "profiles", "work"), Dir(root, "work"))
	assert.Equal(t, filepath.Join(root, "profiles", "work", "auth.json"), AuthFile(root, "work"))
}

// A profile name becomes a path. A name containing a separator would write
// credentials outside the profiles directory.
func TestValidateNameRejectsPathEscapes(t *testing.T) {
	for _, bad := range []string{
		"..", "../evil", "a/b", `a\b`, "/abs", ".hidden", "", "   ",
		"a..b",
	} {
		assert.Error(t, ValidateName(bad), "must reject %q", bad)
	}
}

func TestValidateNameAcceptsOrdinaryNames(t *testing.T) {
	for _, ok := range []string{"work", "personal", "client-a", "team_1", "v2.0", "a"} {
		assert.NoError(t, ValidateName(ok), "must accept %q", ok)
	}
}

func TestValidateNameRejectsOverlongNames(t *testing.T) {
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	assert.Error(t, ValidateName(string(long)))
}

// The environment must win, or a per-project .envrc cannot override the
// machine-wide choice.
func TestResolvePrefersTheEnvironment(t *testing.T) {
	root := t.TempDir()
	mustCreate(t, root, "pointed")
	require.NoError(t, Use(root, "pointed"))

	t.Setenv(EnvVar, "fromenv")
	got, source := Resolve(root)
	assert.Equal(t, "fromenv", got)
	assert.Equal(t, EnvVar, source)
}

func TestResolveFallsBackToThePointer(t *testing.T) {
	root := t.TempDir()
	mustCreate(t, root, "work")
	require.NoError(t, Use(root, "work"))

	t.Setenv(EnvVar, "")
	got, source := Resolve(root)
	assert.Equal(t, "work", got)
	assert.Equal(t, "alpha profile use", source)
}

func TestResolveDefaultsWhenNothingIsSet(t *testing.T) {
	t.Setenv(EnvVar, "")
	got, source := Resolve(t.TempDir())
	assert.Equal(t, Default, got)
	assert.Equal(t, "default", source)
}

// An unusable name must not silently select another profile's credentials.
func TestResolveRejectsAnInvalidEnvironmentName(t *testing.T) {
	t.Setenv(EnvVar, "../escape")
	got, source := Resolve(t.TempDir())
	assert.Equal(t, Default, got)
	assert.Equal(t, "fallback", source)
}

// A pointer file holding a bad name must not escape either.
func TestResolveIgnoresACorruptPointer(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, pointerFile), []byte("../escape\n"), 0o600))

	t.Setenv(EnvVar, "")
	got, _ := Resolve(root)
	assert.Equal(t, Default, got)
}

func TestCreateAndList(t *testing.T) {
	root := t.TempDir()
	assert.Equal(t, []string{Default}, List(root))

	mustCreate(t, root, "work")
	mustCreate(t, root, "personal")
	assert.Equal(t, []string{Default, "personal", "work"}, List(root))
}

// Creating twice must not fail, so the command is safe to repeat.
func TestCreateIsRepeatable(t *testing.T) {
	root := t.TempDir()
	mustCreate(t, root, "work")
	mustCreate(t, root, "work")
}

func TestCreateRejectsBadNames(t *testing.T) {
	_, err := Create(t.TempDir(), "../escape")
	assert.Error(t, err)
}

func TestExists(t *testing.T) {
	root := t.TempDir()
	assert.True(t, Exists(root, Default), "the default is the root, which always exists")
	assert.False(t, Exists(root, "work"))
	mustCreate(t, root, "work")
	assert.True(t, Exists(root, "work"))
}

// Pointing at a profile that was never created would read an empty credential
// store, which looks like being logged out.
func TestUseRequiresAnExistingProfile(t *testing.T) {
	err := Use(t.TempDir(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

// Returning to the default must remove the pointer, not write "default" into
// it: the file exists only while a named profile is chosen.
func TestUseDefaultClearsThePointer(t *testing.T) {
	root := t.TempDir()
	mustCreate(t, root, "work")
	require.NoError(t, Use(root, "work"))
	require.FileExists(t, filepath.Join(root, pointerFile))

	require.NoError(t, Use(root, Default))
	assert.NoFileExists(t, filepath.Join(root, pointerFile))
}

// Clearing an already-clear pointer must succeed, so the command is repeatable.
func TestUseDefaultIsRepeatable(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Use(root, Default))
	require.NoError(t, Use(root, Default))
}

func TestDeleteRemovesTheCredentials(t *testing.T) {
	root := t.TempDir()
	mustCreate(t, root, "work")
	require.NoError(t, os.WriteFile(AuthFile(root, "work"), []byte(`{"credentials":{}}`), 0o600))

	require.NoError(t, Delete(root, "work"))
	assert.NoFileExists(t, AuthFile(root, "work"))
	assert.False(t, Exists(root, "work"))
}

// A pointer to a deleted profile would resolve to a directory that is gone.
func TestDeleteClearsAPointerToItself(t *testing.T) {
	root := t.TempDir()
	mustCreate(t, root, "work")
	require.NoError(t, Use(root, "work"))

	require.NoError(t, Delete(root, "work"))

	t.Setenv(EnvVar, "")
	got, _ := Resolve(root)
	assert.Equal(t, Default, got)
}

// Deleting one profile must not disturb a pointer aimed at another.
func TestDeleteKeepsAnUnrelatedPointer(t *testing.T) {
	root := t.TempDir()
	mustCreate(t, root, "work")
	mustCreate(t, root, "personal")
	require.NoError(t, Use(root, "work"))

	require.NoError(t, Delete(root, "personal"))

	t.Setenv(EnvVar, "")
	got, _ := Resolve(root)
	assert.Equal(t, "work", got)
}

// The default profile is the global root. Deleting it would delete every
// session, skill, and hook alongside the credentials.
func TestDeleteRefusesTheDefault(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "auth.json"), []byte("{}"), 0o600))

	err := Delete(root, Default)
	require.Error(t, err)
	assert.FileExists(t, filepath.Join(root, "auth.json"), "the root must survive")
}

func TestDeleteReportsAMissingProfile(t *testing.T) {
	err := Delete(t.TempDir(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

// A file in the profiles directory is not a profile.
func TestListIgnoresNonDirectories(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "profiles", "stray.txt"), []byte("x"), 0o600))

	assert.Equal(t, []string{Default}, List(root))
}

// Repeating the command is safe, but the caller has to be able to say
// "already exists" instead of "created", which reads as having replaced the
// credentials that were there.
func TestCreateReportsWhetherItWasNew(t *testing.T) {
	root := t.TempDir()

	created, err := Create(root, "work")
	require.NoError(t, err)
	assert.True(t, created, "the first create must report a new profile")

	created, err = Create(root, "work")
	require.NoError(t, err)
	assert.False(t, created, "the second create must report that it existed")
}

// The default profile is the global directory, which always exists.
func TestCreateDefaultIsNeverNew(t *testing.T) {
	created, err := Create(t.TempDir(), Default)
	require.NoError(t, err)
	assert.False(t, created)
}

// Credentials must survive a repeated create, or the command destroys the
// thing it claims to make.
func TestCreateKeepsExistingCredentials(t *testing.T) {
	root := t.TempDir()
	mustCreate(t, root, "work")

	path := AuthFile(root, "work")
	require.NoError(t, os.WriteFile(path, []byte(`{"credentials":{}}`), 0o600))

	_, err := Create(root, "work")
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.NoError(t, err, "the credential file must survive")
}
