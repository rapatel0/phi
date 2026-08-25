package chat

import (
	"unicode"
	"unicode/utf8"
)

// ActiveSkill reports whether the cursor sits in a $skill token.
// start/end are byte offsets into value for the range to replace on accept
// (from '$' through the cursor). query is the text after '$' up to the cursor.
//
// A skill token can appear anywhere in a line, like an @-mention and unlike a
// slash command, because "review this with $code-review" is the normal way to
// name one.
func ActiveSkill(value string, cursor int) (query string, start, end int, ok bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	i := cursor
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(value[:i])
		if r == '$' {
			at := i - size
			if !skillBoundaryBefore(value, at) {
				return "", 0, 0, false
			}
			if skillShellAfter(value, at+size) {
				return "", 0, 0, false
			}
			q := value[at+size : cursor]
			if !skillQuery(q) {
				return "", 0, 0, false
			}
			return q, at, cursor, true
		}
		if r == '\n' || r == '\r' || unicode.IsSpace(r) {
			return "", 0, 0, false
		}
		i -= size
	}
	return "", 0, 0, false
}

// skillShellAfter reports whether the rune at byte offset i makes the
// preceding '$' shell syntax: ${VAR}, $(cmd), or $$ for a pid. Those must stay
// quiet even before the user types anything the query check could reject.
func skillShellAfter(value string, i int) bool {
	if i >= len(value) {
		return false
	}
	switch r, _ := utf8.DecodeRuneInString(value[i:]); r {
	case '{', '(', '$':
		return true
	}
	return false
}

// skillQuery reports whether q can be the start of a skill name. Skill names
// come from a directory name, so they are letters, digits, '-', and '_'.
// Rejecting anything else keeps shell text such as ${HOME} and $(pwd) quiet.
func skillQuery(q string) bool {
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

// skillBoundaryBefore reports whether a '$' at byte offset at opens a token
// rather than sitting inside one.
func skillBoundaryBefore(value string, at int) bool {
	if at == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(value[:at])
	if unicode.IsSpace(r) || r == '\n' || r == '\r' {
		return true
	}
	switch r {
	case '(', '[', '{', '<', '"', '\'', '`', ',', ';', ':', '!', '?', '/', '\\', '|', '=', '+':
		return true
	}
	// Reject mid-token, so shell text like ${VAR}, PATH$X, and US$50 stay quiet.
	// '$' is included for $$, where the second '$' must not open a token.
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '-' || r == '$' {
		return false
	}
	return true
}
