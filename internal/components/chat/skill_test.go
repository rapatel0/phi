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

// A well-known shell variable stays quiet, but the same name in lowercase is a
// plausible skill and must still complete. This mirrors the Codex rule.
func TestActiveSkillEnvVarGuardIsCaseSensitive(t *testing.T) {
	quiet := []string{"$HOME", "$PATH", "$USER", "$SHELL", "$PWD", "$GOPATH", "$XDG_CONFIG_HOME"}
	for _, v := range quiet {
		t.Run("quiet"+v, func(t *testing.T) {
			_, _, _, ok := ActiveSkill(v, len(v))
			assert.False(t, ok, "%q is a shell variable", v)
		})
	}

	open := []string{"$home", "$path", "$Home", "$HOMEX", "$HOME2"}
	for _, v := range open {
		t.Run("open"+v, func(t *testing.T) {
			query, _, _, ok := ActiveSkill(v, len(v))
			require.True(t, ok, "%q is not a known variable, so it may be a skill", v)
			assert.Equal(t, v[1:], query)
		})
	}
}

// A plugin-qualified name is one token, so ':' must not split it.
func TestActiveSkillAllowsPluginQualifiedName(t *testing.T) {
	const value = "$plugin:skill"
	query, start, end, ok := ActiveSkill(value, len(value))
	require.True(t, ok)
	assert.Equal(t, "plugin:skill", query)
	assert.Equal(t, value, value[start:end], "the whole token is replaced on accept")
}

// A ':' before the '$' still opens a token, so prose like "note: $rev" works.
func TestActiveSkillAfterColonAndSpace(t *testing.T) {
	query, start, _, ok := ActiveSkill("note: $rev", 10)
	require.True(t, ok)
	assert.Equal(t, "rev", query)
	assert.Equal(t, 6, start)
}

// A skill name comes from a directory, so it starts with a letter. Money,
// shell positionals, and the shell's $_ and $- must stay quiet. The README
// promises this for $5. Non-ASCII digits count too, since the check uses
// unicode.IsLetter rather than an ASCII range.
func TestActiveSkillNeedsLeadingLetter(t *testing.T) {
	quiet := []string{"$5", "$50", "$1000", "$0", "$_", "$-", "$\u0665", "$\uff15"}
	for _, v := range quiet {
		t.Run("quiet"+v, func(t *testing.T) {
			_, _, _, ok := ActiveSkill(v, len(v))
			assert.False(t, ok, "%q cannot be a skill name", v)
		})
	}

	// A digit later in the name is fine, and a non-ASCII letter may start one.
	for _, tc := range []struct{ value, query string }{
		{"$s3", "s3"},
		{"$my_skill", "my_skill"},
		{"$\u00e9cole", "\u00e9cole"},
	} {
		t.Run("open"+tc.value, func(t *testing.T) {
			query, _, _, ok := ActiveSkill(tc.value, len(tc.value))
			require.True(t, ok)
			assert.Equal(t, tc.query, query)
		})
	}

	// A bare '$' must still open the picker; that is how you browse the list.
	_, _, _, ok := ActiveSkill("$", 1)
	assert.True(t, ok, "a bare $ opens the picker")
}
