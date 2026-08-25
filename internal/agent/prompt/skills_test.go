package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSkillDir(t *testing.T, dir, name, desc string) {
	t.Helper()
	sub := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(sub, 0o755))
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(body), 0o644))
}

// isolateSkills points every search directory at temp dirs, so a developer's
// real skills cannot change the result.
func isolateSkills(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ALPHA_SKILL_PATH", "")
	t.Setenv("PHI_SKILL_PATH", "")
}

// The model only honors a $name token if the prompt explains it.
func TestSkillsBlockDocumentsDollarConvention(t *testing.T) {
	isolateSkills(t)
	dir := t.TempDir()
	writeSkillDir(t, dir, "code-review", "reviews code")

	got := Build(dir, false, nil)

	assert.Contains(t, got, "code-review", "the catalog must list the skill")
	assert.Contains(t, got, "$name", "the prompt must explain the token")
	assert.Contains(t, got, "not a skill reference", "and must exclude shell text")
}

// The catalog must cover every directory the skill tool can load. A narrower
// list would advertise fewer skills than the tool accepts.
func TestSkillsBlockCoversEnvPath(t *testing.T) {
	isolateSkills(t)
	envDir := t.TempDir()
	writeSkillDir(t, envDir, "from-env", "loaded via the env path")
	t.Setenv("ALPHA_SKILL_PATH", envDir)

	got := Build("", false, nil)

	assert.Contains(t, got, "from-env",
		"a skill outside the configured path must still be listed")
}

// No skills means no block, rather than an empty heading.
func TestSkillsBlockOmittedWhenEmpty(t *testing.T) {
	isolateSkills(t)

	got := Build(t.TempDir(), false, nil)

	assert.NotContains(t, got, "## Available skills")
	assert.NotContains(t, got, "$name")
}
