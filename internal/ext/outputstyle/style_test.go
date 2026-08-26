package outputstyle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter(t *testing.T) {
	s := Parse("---\nname: concise\ndescription: Few words\n---\nAnswer briefly.\n", "file")
	assert.Equal(t, "concise", s.Name)
	assert.Equal(t, "Few words", s.Description)
	assert.Equal(t, "Answer briefly.", s.Body)
}

// Without frontmatter the file name names the style, so a plain markdown file
// is a valid style.
func TestParseWithoutFrontmatter(t *testing.T) {
	s := Parse("Just a body.\n", "teacher")
	assert.Equal(t, "teacher", s.Name)
	assert.Empty(t, s.Description)
	assert.Equal(t, "Just a body.", s.Body)
}

// A quoted description keeps its inner text, not the quotes.
func TestParseStripsQuotes(t *testing.T) {
	s := Parse("---\nname: x\ndescription: \"a: colon\"\n---\nbody", "f")
	assert.Equal(t, "a: colon", s.Description)
}

// An unterminated frontmatter block is not frontmatter; treating it as a body
// is better than losing the text.
func TestParseUnterminatedFrontmatter(t *testing.T) {
	s := Parse("---\nname: x\nbody with no close", "fallback")
	assert.Equal(t, "fallback", s.Name)
	assert.Contains(t, s.Body, "body with no close")
}

func TestApplyAppendsStyle(t *testing.T) {
	got := Apply("BASE PROMPT", Style{Name: "concise", Body: "Be brief."})
	assert.Contains(t, got, "BASE PROMPT")
	assert.Contains(t, got, "Be brief.")
	assert.Contains(t, got, marker+"concise")
}

// Switching styles must replace, not stack: two applies leave one block.
func TestApplyReplacesPreviousStyle(t *testing.T) {
	once := Apply("BASE", Style{Name: "a", Body: "Style A."})
	twice := Apply(once, Style{Name: "b", Body: "Style B."})

	assert.Equal(t, 1, strings.Count(twice, marker), "exactly one style block must remain")
	assert.NotContains(t, twice, "Style A.")
	assert.Contains(t, twice, "Style B.")
	assert.Contains(t, twice, "BASE")
}

// Applying the same style repeatedly must be stable, since it runs every turn.
func TestApplyIsIdempotent(t *testing.T) {
	s := Style{Name: "concise", Body: "Be brief."}
	once := Apply("BASE", s)
	assert.Equal(t, once, Apply(once, s))
}

// Clearing a style must restore the original prompt exactly, or the base drifts
// as styles are switched.
func TestStripAppliedRestoresBase(t *testing.T) {
	const base = "BASE PROMPT"
	assert.Equal(t, base, stripApplied(Apply(base, Style{Name: "a", Body: "A."})))
	assert.Equal(t, base, stripApplied(base), "no style is not an error")
}

// An empty body would append a heading with nothing under it.
func TestApplyEmptyBodyChangesNothing(t *testing.T) {
	assert.Equal(t, "BASE", Apply("BASE", Style{Name: "empty", Body: "   \n"}))
}

func writeStyle(t *testing.T, dir, file, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600))
}

func TestDiscoverReadsMarkdownOnly(t *testing.T) {
	dir := t.TempDir()
	writeStyle(t, dir, "concise.md", "---\nname: concise\n---\nBrief.")
	writeStyle(t, dir, "notes.txt", "ignored")

	styles := Discover([]string{dir})
	require.Len(t, styles, 1)
	assert.Contains(t, styles, "concise")
}

// A project style must shadow a user style of the same name, so a repo can set
// its own voice without editing the user directory.
func TestDiscoverEarlierDirWins(t *testing.T) {
	project, user := t.TempDir(), t.TempDir()
	writeStyle(t, project, "concise.md", "---\nname: concise\ndescription: project\n---\nP.")
	writeStyle(t, user, "concise.md", "---\nname: concise\ndescription: user\n---\nU.")

	styles := Discover([]string{project, user})
	require.Len(t, styles, 1)
	assert.Equal(t, "project", styles["concise"].Description)
}

// A missing directory is the normal case before any style is created.
func TestDiscoverMissingDirIsNotAnError(t *testing.T) {
	assert.Empty(t, Discover([]string{filepath.Join(t.TempDir(), "nope")}))
}

func TestNamesAreSorted(t *testing.T) {
	got := Names(map[string]Style{"b": {}, "a": {}, "c": {}})
	assert.Equal(t, []string{"a", "b", "c"}, got)
}

// resolve must report false for a style that was deleted after selection, so a
// stale name cannot silently keep applying.
func TestResolveMissingStyle(t *testing.T) {
	dir := t.TempDir()
	s := &store{dirs: []string{dir}}
	s.set("gone")
	_, ok := s.resolve()
	assert.False(t, ok)
}

func TestResolveActiveStyle(t *testing.T) {
	dir := t.TempDir()
	writeStyle(t, dir, "concise.md", "---\nname: concise\n---\nBrief.")
	s := &store{dirs: []string{dir}}
	s.set("concise")

	got, ok := s.resolve()
	require.True(t, ok)
	assert.Equal(t, "Brief.", got.Body)
}

// The error must name what is available, or the user has to guess.
func TestUnknownStyleErrorListsAvailable(t *testing.T) {
	err := errUnknownStyle("nope", map[string]Style{"concise": {}, "teacher": {}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "concise, teacher")
}
