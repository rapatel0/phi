package skills_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rapatel0/alpha/internal/llm/skills"
)

func TestSearchDirsIncludesAgentsPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ALPHA_SKILL_PATH", "")
	t.Setenv("PHI_SKILL_PATH", "")

	dirs := skills.SearchDirs("", "/repo")

	assert.Contains(t, dirs, filepath.Join(home, ".agents", "skills"))
	assert.Contains(t, dirs, filepath.Join(home, ".alpha", "skills"))
	assert.Contains(t, dirs, filepath.Join(home, ".claude", "skills"))
	assert.Contains(t, dirs, filepath.Join(home, ".codex", "skills"))
	assert.Contains(t, dirs, filepath.Join(home, ".grok", "skills"))
	assert.Contains(t, dirs, filepath.Join("/repo", ".agents", "skills"))
	assert.Contains(t, dirs, filepath.Join("/repo", ".alpha", "skills"))
	assert.Contains(t, dirs, filepath.Join("/repo", ".claude", "skills"))
}

func TestSearchDirsConfiguredPathComesFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ALPHA_SKILL_PATH", "")
	t.Setenv("PHI_SKILL_PATH", "")

	dirs := skills.SearchDirs("/custom/skills", "")
	assert.Equal(t, "/custom/skills", dirs[0])
	assert.NotContains(t, dirs, "")
}
