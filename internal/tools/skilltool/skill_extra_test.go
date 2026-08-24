package skilltool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/tools/tooldef"
)

// writeSkill creates <dir>/<name>/SKILL.md with front matter and a body.
func writeSkill(t *testing.T, dir, name, description, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))
}

func TestSkillToolSchemaFields(t *testing.T) {
	tl := SkillTool()
	require.NotNil(t, tl.Definition.Params)
	assert.Equal(t, []string{"name"}, tl.Definition.Params.Required)
	assert.Contains(t, tl.Definition.Params.Properties, "name")
	require.NotNil(t, tl.DetailFromArgs)
	assert.Equal(t, "review", tl.DetailFromArgs(json.RawMessage(`{"name":"  review  "}`)))
	assert.Empty(t, tl.DetailFromArgs(json.RawMessage(`nope`)))
}

func TestRunSkillBadArgs(t *testing.T) {
	_, err := runSkill(t.Context(), json.RawMessage(`{`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skill args")
}

func TestRunSkillNotFoundListsKnownNames(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "review", "Review a change", "Read the diff.")
	writeSkill(t, dir, "deploy", "Ship it", "Push the button.")

	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{SkillPath: dir})
	_, err := runSkill(ctx, json.RawMessage(`{"name":"nope"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "review")
	assert.Contains(t, err.Error(), "deploy")
}

func TestRunSkillFallsBackToDescription(t *testing.T) {
	dir := t.TempDir()
	// Empty body, so the description is returned instead.
	writeSkill(t, dir, "terse", "Only a description", "")

	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{SkillPath: dir})
	out, err := runSkill(ctx, json.RawMessage(`{"name":"terse"}`))
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Only a description")
	assert.Contains(t, out.Content, "Location: ")
	assert.Equal(t, out.Content, out.Output)
}

func TestRunSkillIsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "review", "Review a change", "Read the diff.")

	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{SkillPath: dir})
	out, err := runSkill(ctx, json.RawMessage(`{"name":"REVIEW"}`))
	require.NoError(t, err)
	assert.Equal(t, "review", out.Detail)
}

func TestLoadAllDedupesAcrossDirs(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeSkill(t, first, "shared", "From the model path", "First wins.")
	writeSkill(t, second, "shared", "From the env path", "Second loses.")
	writeSkill(t, second, "extra", "Only in env path", "Extra body.")
	t.Setenv("PHI_SKILL_PATH", second)

	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{SkillPath: first})
	list, err := loadAll(ctx)
	require.NoError(t, err)

	bodies := map[string]string{}
	for _, s := range list {
		bodies[s.Name] = s.Body
	}
	assert.Equal(t, "First wins.", bodies["shared"], "earlier dir must win")
	assert.Equal(t, "Extra body.", bodies["extra"])
}

func TestSkillDirsOrderAndSources(t *testing.T) {
	modelDir, envDir, cwd := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("PHI_SKILL_PATH", envDir)

	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{SkillPath: modelDir})
	ctx = tooldef.WithCwd(ctx, cwd)
	dirs := skillDirs(ctx)

	require.GreaterOrEqual(t, len(dirs), 3)
	assert.Equal(t, modelDir, dirs[0], "model skill_path comes first")
	assert.Equal(t, envDir, dirs[1], "PHI_SKILL_PATH comes second")
	assert.Contains(t, dirs, filepath.Join(cwd, ".phi", "skills"))
}

func TestSkillDirsSkipsBlankPaths(t *testing.T) {
	t.Setenv("PHI_SKILL_PATH", "")
	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{SkillPath: "   "})
	for _, d := range skillDirs(ctx) {
		assert.NotEmpty(t, d)
	}
}
