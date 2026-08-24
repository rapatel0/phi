package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSessionFile creates a minimal session jsonl with a header and one user
// message, then stamps its mtime so ordering is deterministic.
func writeSessionFile(t *testing.T, dir, id, cwd, userText string, mtime time.Time) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "2026-08-24T10-00-00_"+id+".jsonl")
	body := `{"type":"EntrySession","id":"` + id + `","timestamp":"2026-08-24T10-00-00","cwd":"` + cwd + `"}` + "\n" +
		`{"type":"EntryMessage","id":"m1","parentID":null,"message":{"role":"user","content":"` + userText + `"}}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
	return path
}

func TestBrowseSessionsGroupsByProject(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	writeSessionFile(t, filepath.Join(base, "--Users-me-old--"), "aaa11111",
		"/Users/me/old", "older work", now.Add(-48*time.Hour))
	writeSessionFile(t, filepath.Join(base, "--Users-me-proj--"), "bbb22222",
		"/Users/me/proj", "newest work", now)
	writeSessionFile(t, filepath.Join(base, "--Users-me-proj--"), "ccc33333",
		"/Users/me/proj", "middle work", now.Add(-2*time.Hour))

	got, err := BrowseSessions(base)
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Newest project first.
	assert.Equal(t, "proj", got[0].Label)
	assert.Equal(t, "/Users/me/proj", got[0].Cwd)
	require.Len(t, got[0].Sessions, 2)
	// Newest session first within a project.
	assert.Equal(t, "bbb22222", got[0].Sessions[0].ID)
	assert.Equal(t, "ccc33333", got[0].Sessions[1].ID)

	assert.Equal(t, "old", got[1].Label)
	require.Len(t, got[1].Sessions, 1)
	assert.Equal(t, "aaa11111", got[1].Sessions[0].ID)
}

func TestBrowseSessionsSkipsEmptyDirs(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "--empty--"), 0o755))
	// A stray non-session file must not create a project entry.
	require.NoError(t, os.WriteFile(filepath.Join(base, "--empty--", "notes.txt"), []byte("x"), 0o600))
	writeSessionFile(t, filepath.Join(base, "--Users-me-proj--"), "bbb22222",
		"/Users/me/proj", "work", time.Now())

	got, err := BrowseSessions(base)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "proj", got[0].Label)
}

func TestBrowseSessionsIgnoresLooseFiles(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "stray.jsonl"), []byte("{}"), 0o600))

	got, err := BrowseSessions(base)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestBrowseSessionsMissingBase(t *testing.T) {
	got, err := BrowseSessions(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestBrowseSessionsCarriesPreviewAndFile(t *testing.T) {
	base := t.TempDir()
	path := writeSessionFile(t, filepath.Join(base, "--Users-me-proj--"), "bbb22222",
		"/Users/me/proj", "fix the oauth tool names", time.Now())

	got, err := BrowseSessions(base)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Len(t, got[0].Sessions, 1)
	assert.Equal(t, "fix the oauth tool names", got[0].Sessions[0].Preview)
	assert.Equal(t, path, got[0].Sessions[0].File)
}

func TestProjectLabel(t *testing.T) {
	cases := map[string]string{
		"--Users-me-proj--": "proj",
		"--Users-me-a-b--":  "b",
		"--proj--":          "proj",
		"--unknown--":       "unknown",
		"":                  "",
		"--":                "--",
	}
	for in, want := range cases {
		assert.Equal(t, want, projectLabel(in), in)
	}
}

func TestFirstCwd(t *testing.T) {
	assert.Empty(t, firstCwd(nil))
	assert.Empty(t, firstCwd([]SessionMeta{{Cwd: "  "}}))
	assert.Equal(t, "/b", firstCwd([]SessionMeta{{Cwd: ""}, {Cwd: "/b"}}))
}
