package composer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/llm/skills"
)

// newSkillPane returns a pane whose $ picker reads from a temp skill dir.
// SKILL_PATH is cleared so a developer's real skills cannot change results.
func newSkillPane(t *testing.T, names ...string) *ComposerPane {
	t.Helper()
	t.Setenv("ALPHA_SKILL_PATH", "")
	t.Setenv("PHI_SKILL_PATH", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := t.TempDir()
	for _, n := range names {
		sub := filepath.Join(dir, n)
		require.NoError(t, os.MkdirAll(sub, 0o755))
		body := "---\nname: " + n + "\ndescription: does " + n + "\n---\n\nbody\n"
		require.NoError(t, os.WriteFile(filepath.Join(sub, skills.SkillFileName), []byte(body), 0o644))
	}

	c := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())
	c.SetSkillPath(dir)
	c.skill.OnAccept = c.acceptSkill
	return c
}

func TestSkillPickerOpensOnDollar(t *testing.T) {
	c := newSkillPane(t, "review", "build")

	c.onSkillChange(true, "")
	assert.True(t, c.skill.Open, "$ must open the picker")
	assert.True(t, c.Chat.SkillOpen, "the chat must defer nav keys")
	assert.Len(t, c.skill.Items, 2)
}

func TestSkillPickerFilters(t *testing.T) {
	c := newSkillPane(t, "review", "build")

	c.onSkillChange(true, "rev")
	require.Len(t, c.skill.Items, 1)
	assert.Equal(t, "review", c.skill.Items[0].Path)
	assert.Equal(t, "does review", c.skill.Items[0].Description, "the row shows the description")
}

func TestSkillPickerReportsNoMatch(t *testing.T) {
	c := newSkillPane(t, "review")

	c.onSkillChange(true, "zzz")
	assert.Empty(t, c.skill.Items)
	assert.Equal(t, "No matching skills", c.skill.Status, "an empty list must say why")
}

func TestSkillPickerClosesWhenInactive(t *testing.T) {
	c := newSkillPane(t, "review")

	c.onSkillChange(true, "")
	require.True(t, c.skill.Open)

	c.onSkillChange(false, "")
	assert.False(t, c.skill.Open)
	assert.False(t, c.Chat.SkillOpen)
}

// Accept inserts the literal token, so the model sees exactly what the user
// sees. It must not submit.
func TestAcceptSkillInsertsToken(t *testing.T) {
	c := newSkillPane(t, "code-review")
	c.Chat.Value = "please $cod"
	c.Chat.Cursor = len(c.Chat.Value)

	c.onSkillChange(true, "cod")
	require.Len(t, c.skill.Items, 1)
	c.skill.OnAccept(c.skill.Items[0])

	assert.Equal(t, "please $code-review ", c.Chat.Value,
		"accept replaces the token and adds a trailing space")
	assert.False(t, c.skill.Open)
	assert.False(t, c.Chat.SkillOpen)
}

// A token mid-sentence must be replaced in place, not appended.
func TestAcceptSkillReplacesInPlace(t *testing.T) {
	c := newSkillPane(t, "review")
	c.Chat.Value = "$rev the auth package"
	c.Chat.Cursor = 4 // just after "$rev"

	c.onSkillChange(true, "rev")
	require.Len(t, c.skill.Items, 1)
	c.skill.OnAccept(c.skill.Items[0])

	assert.Equal(t, "$review  the auth package", c.Chat.Value)
}

// Only one completer may be open, or two popups fight for the same anchor.
func TestSkillPickerYieldsToOtherCompleters(t *testing.T) {
	c := newSkillPane(t, "review")

	c.mention.Show()
	c.Chat.MentionOpen = true
	c.onSkillChange(true, "")
	assert.False(t, c.skill.Open, "the @ picker keeps priority")

	c.mention.Hide()
	c.Chat.MentionOpen = false
	c.slash.Show()
	c.Chat.SlashOpen = true
	c.onSkillChange(true, "")
	assert.False(t, c.skill.Open, "the / picker keeps priority")
}

// Opening another completer must close this one.
func TestOtherCompletersCloseSkillPicker(t *testing.T) {
	c := newSkillPane(t, "review")

	c.onSkillChange(true, "")
	require.True(t, c.skill.Open)
	c.onSlashChange(true, "")
	assert.False(t, c.skill.Open, "/ must close the $ picker")

	c.onSlashChange(false, "")
	c.onSkillChange(true, "")
	require.True(t, c.skill.Open)
	c.onMentionChange(true, "x")
	assert.False(t, c.skill.Open, "@ must close the $ picker")
}

func TestCloseMentionSlashClosesSkillPicker(t *testing.T) {
	c := newSkillPane(t, "review")

	c.onSkillChange(true, "")
	require.True(t, c.skill.Open)

	c.CloseMentionSlash()
	assert.False(t, c.skill.Open)
	assert.False(t, c.Chat.SkillOpen)
}

// An empty skill path is valid; the other search directories still apply.
func TestSkillPickerEmptyPath(t *testing.T) {
	t.Setenv("ALPHA_SKILL_PATH", "")
	t.Setenv("PHI_SKILL_PATH", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	c := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())
	c.onSkillChange(true, "")
	assert.True(t, c.skill.Open, "the picker still opens")
	assert.Empty(t, c.skill.Items)
	assert.Equal(t, "No matching skills", c.skill.Status)
}

func TestSkillPickerPrefix(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	assert.Equal(t, "$", c.skill.Prefix, "rows must render with a $")
}

// End-to-end through ChatInput: typing '$' must reach the picker via the
// notifier chain, not just through a direct handler call.
func TestTypingDollarOpensPickerThroughChatInput(t *testing.T) {
	c := newSkillPane(t, "review")
	c.Chat.OnSkillChange = c.onSkillChange

	c.Chat.ReplaceRange(0, 0, "$rev")

	assert.True(t, c.skill.Open, "the notifier chain must open the picker")
	require.Len(t, c.skill.Items, 1)
	assert.Equal(t, "review", c.skill.Items[0].Path)
}

// Typing past the token must close it again.
func TestSpaceClosesPickerThroughChatInput(t *testing.T) {
	c := newSkillPane(t, "review")
	c.Chat.OnSkillChange = c.onSkillChange

	c.Chat.ReplaceRange(0, 0, "$rev")
	require.True(t, c.skill.Open)

	c.Chat.ReplaceRange(c.Chat.Cursor, c.Chat.Cursor, " ")
	assert.False(t, c.skill.Open, "a space ends the token")
}
