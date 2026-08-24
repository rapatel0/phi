package skilltool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

func TestSkillToolSchema(t *testing.T) {
	tl := SkillTool()
	assert.Equal(t, "skill", tl.Definition.Name)
	assert.True(t, tl.Definition.Readable)
}

func TestRunSkillLoadsBody(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "review")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	body := "---\nname: review\ndescription: Review a change\n---\nRead the diff first.\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644))

	raw, err := json.Marshal(map[string]string{"name": "review"})
	require.NoError(t, err)
	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{SkillPath: dir})
	out, err := runSkill(ctx, raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Read the diff first.")
	assert.Contains(t, out.Content, "# Skill: review")
	assert.Equal(t, "review", out.Detail)
}

func TestRunSkillNotFound(t *testing.T) {
	dir := t.TempDir()
	raw, _ := json.Marshal(map[string]string{"name": "missing"})
	ctx := tooldef.WithModel(t.Context(), llm.ModelConfig{SkillPath: dir})
	_, err := runSkill(ctx, raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunSkillEmptyName(t *testing.T) {
	_, err := runSkill(t.Context(), json.RawMessage(`{"name":" "}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty name")
}
