package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm/skills"
)

func writeSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	sub := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(sub, 0o755))
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(sub, skills.SkillFileName), []byte(body), 0o644))
}

func names(list []*skills.Skill) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.Name)
	}
	return out
}

func TestLoadDirsMergesInOrder(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeSkill(t, a, "alpha", "from a")
	writeSkill(t, b, "beta", "from b")

	got := skills.LoadDirs([]string{a, b})
	assert.ElementsMatch(t, []string{"alpha", "beta"}, names(got))
}

// The first directory wins, so a project skill can override a user one.
func TestLoadDirsFirstDirectoryWins(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeSkill(t, first, "review", "winner")
	writeSkill(t, second, "review", "loser")

	got := skills.LoadDirs([]string{first, second})
	require.Len(t, got, 1, "a duplicate name must not appear twice")
	assert.Equal(t, "winner", got[0].Description)
}

// A missing or empty entry must not empty the catalog.
func TestLoadDirsSkipsBadEntries(t *testing.T) {
	good := t.TempDir()
	writeSkill(t, good, "alpha", "d")

	got := skills.LoadDirs([]string{"", filepath.Join(t.TempDir(), "nope"), good})
	assert.Equal(t, []string{"alpha"}, names(got))
}

func TestLoadDirsEmpty(t *testing.T) {
	assert.Empty(t, skills.LoadDirs(nil))
	assert.Empty(t, skills.LoadDirs([]string{t.TempDir()}))
}

func TestFilter(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"review", "code-review", "build"} {
		writeSkill(t, dir, n, "d")
	}
	list := skills.LoadDirs([]string{dir})

	assert.Equal(t, []string{"build", "code-review", "review"}, names(skills.Filter(list, "")),
		"an empty query returns everything, sorted by name")

	// A prefix match outranks a substring match.
	assert.Equal(t, []string{"review", "code-review"}, names(skills.Filter(list, "review")))

	assert.Equal(t, []string{"build"}, names(skills.Filter(list, "BUI")), "match is case-insensitive")
	assert.Empty(t, skills.Filter(list, "nope"))
}

// The picker refilters on every keystroke, so order must be deterministic.
func TestFilterOrderIsStable(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"rev-b", "rev-a", "rev-c"} {
		writeSkill(t, dir, n, "d")
	}
	list := skills.LoadDirs([]string{dir})

	want := []string{"rev-a", "rev-b", "rev-c"}
	for range 5 {
		assert.Equal(t, want, names(skills.Filter(list, "rev")))
	}
}
