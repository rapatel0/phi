package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveSkill(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		cursor int
		query  string
		start  int
		ok     bool
	}{
		{name: "bare dollar opens empty query", value: "$", cursor: 1, query: "", start: 0, ok: true},
		{name: "partial name", value: "$rev", cursor: 4, query: "rev", start: 0, ok: true},
		{name: "mid sentence", value: "run $rev", cursor: 8, query: "rev", start: 4, ok: true},
		{name: "after open paren", value: "($rev", cursor: 5, query: "rev", start: 1, ok: true},
		{name: "hyphenated name", value: "$code-review", cursor: 12, query: "code-review", start: 0, ok: true},
		{name: "cursor inside token", value: "$review", cursor: 3, query: "re", start: 0, ok: true},
		{name: "second line", value: "hi\n$rev", cursor: 7, query: "rev", start: 3, ok: true},

		{name: "no dollar", value: "review", cursor: 6, ok: false},
		{name: "space ends token", value: "$rev now", cursor: 8, ok: false},
		{name: "empty value", value: "", cursor: 0, ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			query, start, end, ok := ActiveSkill(tc.value, tc.cursor)
			require.Equal(t, tc.ok, ok)
			if !tc.ok {
				return
			}
			assert.Equal(t, tc.query, query)
			assert.Equal(t, tc.start, start)
			assert.Equal(t, tc.cursor, end, "end must be the cursor")
		})
	}
}

// '$' is common in ordinary text, so the picker must stay closed for it.
// A false positive here interrupts typing, which is worse than a missing
// completion.
func TestActiveSkillIgnoresShellAndMoneyText(t *testing.T) {
	quiet := []struct {
		name  string
		value string
	}{
		{"shell brace expansion", "echo ${HOME}"},
		{"variable after word", "PATH$X"},
		{"currency", "US$50"},
		{"double dollar pid", "$$"},
		{"digit before", "5$x"},
		{"underscore before", "a_$x"},
		{"dot before", "pkg.$x"},
		{"hyphen before", "co-$x"},
	}

	for _, tc := range quiet {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, ok := ActiveSkill(tc.value, len(tc.value))
			assert.False(t, ok, "%q must not open the skill picker", tc.value)
		})
	}
}

// ${HOME} must stay quiet at every cursor position, not just at the end.
func TestActiveSkillQuietAcrossCursorPositions(t *testing.T) {
	const value = "echo ${HOME}"
	for cursor := 0; cursor <= len(value); cursor++ {
		_, _, _, ok := ActiveSkill(value, cursor)
		assert.False(t, ok, "cursor %d in %q must not open the picker", cursor, value)
	}
}

func TestActiveSkillClampsCursor(t *testing.T) {
	_, _, _, ok := ActiveSkill("$rev", -5)
	assert.False(t, ok, "a negative cursor is start of line, so no token")

	query, _, end, ok := ActiveSkill("$rev", 99)
	require.True(t, ok, "an oversized cursor must clamp, not panic")
	assert.Equal(t, "rev", query)
	assert.Equal(t, 4, end)
}

// The replace range must cover the whole token so accept overwrites it.
func TestActiveSkillRangeCoversToken(t *testing.T) {
	const value = "please $rev"
	query, start, end, ok := ActiveSkill(value, len(value))
	require.True(t, ok)
	assert.Equal(t, "rev", query)
	assert.Equal(t, "$rev", value[start:end], "range must span '$' through cursor")
}

// A '$' does not start a token when it directly follows an @-mention path,
// which would otherwise hijack a filename containing a dollar sign.
func TestActiveSkillDoesNotBreakMentions(t *testing.T) {
	_, _, _, ok := ActiveSkill("@internal/ext$", 14)
	assert.False(t, ok, "a '$' inside a path token is not a skill")
}
