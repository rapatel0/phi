package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/session"
)

func TestSessionPickerProjectsMarksCurrent(t *testing.T) {
	now := time.Now()
	groups := []session.ProjectSessions{
		{
			Dir:   "/home/me/.alpha/session/--Users-me-alpha--",
			Name:  "--Users-me-alpha--",
			Label: "alpha",
			Cwd:   "/Users/me/alpha",
			Sessions: []session.SessionMeta{
				{ID: "aaa11111", File: "/s/a1.jsonl", Preview: "fix oauth", Mtime: now},
			},
		},
		{
			Dir:   "/home/me/.alpha/session/--Users-me-repos--",
			Name:  "--Users-me-repos--",
			Label: "repos",
			Cwd:   "/Users/me/repos",
			Sessions: []session.SessionMeta{
				{ID: "bbb22222", File: "/s/r1.jsonl", Preview: "notes", Mtime: now},
			},
		},
	}

	got := SessionPickerProjects(groups, "/home/me/.alpha/session/--Users-me-alpha--")
	require.Len(t, got, 2)

	assert.True(t, got[0].Current, "the running project must be marked")
	assert.False(t, got[1].Current)

	assert.Equal(t, "alpha", got[0].Label)
	assert.Equal(t, "/Users/me/alpha", got[0].Cwd)
	require.Len(t, got[0].Sessions, 1)
	assert.Equal(t, "aaa11111", got[0].Sessions[0].ID)
	// The file path is what Resume needs for a cross-project session.
	assert.Equal(t, "/s/a1.jsonl", got[0].Sessions[0].File)
	assert.Equal(t, "fix oauth", got[0].Sessions[0].Preview)
}

func TestSessionPickerProjectsNoCurrentDir(t *testing.T) {
	groups := []session.ProjectSessions{
		{Dir: "/d", Label: "one", Sessions: []session.SessionMeta{{ID: "a"}}},
	}
	got := SessionPickerProjects(groups, "")
	require.Len(t, got, 1)
	assert.False(t, got[0].Current, "an empty current dir must not match any project")
}

func TestSessionPickerProjectsEmpty(t *testing.T) {
	assert.Empty(t, SessionPickerProjects(nil, "/d"))
}
