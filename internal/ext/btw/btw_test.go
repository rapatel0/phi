package btw

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArgsQuestion(t *testing.T) {
	a := ParseArgs([]string{"what", "does", "PATH", "do?"})
	assert.Equal(t, "what does PATH do?", a.Prompt)
	assert.False(t, a.Tangent)
	assert.Empty(t, a.Sub)
}

// A subcommand takes no further words, so "list the open files" stays a
// question rather than silently listing the thread.
func TestParseArgsSubcommandNeedsToBeAlone(t *testing.T) {
	assert.Equal(t, "list", ParseArgs([]string{"list"}).Sub)
	assert.Equal(t, "clear", ParseArgs([]string{"clear"}).Sub)

	a := ParseArgs([]string{"list", "the", "open", "files"})
	assert.Empty(t, a.Sub, "a question that starts with list is still a question")
	assert.Equal(t, "list the open files", a.Prompt)
}

func TestParseArgsTangentFlag(t *testing.T) {
	for _, flag := range []string{"--tangent", ":tangent"} {
		a := ParseArgs([]string{flag, "unrelated", "question"})
		assert.True(t, a.Tangent, "%s must start a tangent", flag)
		assert.Equal(t, "unrelated question", a.Prompt, "the flag must not leak into the prompt")
	}
}

func TestParseArgsEmpty(t *testing.T) {
	assert.Empty(t, ParseArgs(nil).Prompt)
	assert.Empty(t, ParseArgs([]string{"   "}).Prompt)
}

// A subcommand name is matched case-insensitively, since a user may type LIST.
func TestParseArgsSubcommandCase(t *testing.T) {
	assert.Equal(t, "list", ParseArgs([]string{"LIST"}).Sub)
}

func TestThreadRecordsAndClears(t *testing.T) {
	var th Thread
	assert.Zero(t, th.Len())

	th.add(Turn{Prompt: "q1", Summary: "a1", JobID: "j1"})
	th.add(Turn{Prompt: "q2", Summary: "a2", JobID: "j2"})
	require.Equal(t, 2, th.Len())

	turns := th.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, "q1", turns[0].Prompt)

	// The returned slice is a copy, so a caller cannot corrupt the thread.
	turns[0].Prompt = "mutated"
	assert.Equal(t, "q1", th.Turns()[0].Prompt)

	th.reset()
	assert.Zero(t, th.Len())
}

func TestRenderEmptyThreadExplainsHow(t *testing.T) {
	assert.Contains(t, Render(nil), "/btw <question>")
}

// A listing must stay one line per entry, or a long answer floods the view.
func TestRenderKeepsOneLinePerEntry(t *testing.T) {
	got := Render([]Turn{{
		Prompt:  "why?\nsecond line of the question",
		Summary: "because\nsecond line of the answer",
	}})
	assert.Contains(t, got, "why?")
	assert.Contains(t, got, "because")
	assert.NotContains(t, got, "second line")
}

func TestFirstLineTruncatesLongText(t *testing.T) {
	got := firstLine(strings.Repeat("x", 200))
	assert.Len(t, []rune(got), 100)
	assert.True(t, strings.HasSuffix(got, "…"))
}

// An answerless turn must still render, rather than showing a blank row.
func TestFirstLineEmpty(t *testing.T) {
	assert.Equal(t, "(no answer)", firstLine("   "))
}

func TestPluralReadsNaturally(t *testing.T) {
	assert.Equal(t, "1 aside", plural(1))
	assert.Equal(t, "2 asides", plural(2))
	assert.Equal(t, "12 asides", plural(12))
}
