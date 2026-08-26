package chat

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// commonEnvVars are shell variables common enough that `$NAME` almost always
// means the variable, not a skill. The check is case-sensitive on purpose:
// `$HOME` is the variable, `$home` is a plausible skill name.
var commonEnvVars = map[string]struct{}{
	"HOME": {}, "PATH": {}, "USER": {}, "SHELL": {}, "PWD": {},
	"OLDPWD": {}, "TMPDIR": {}, "TEMP": {}, "TMP": {}, "LANG": {},
	"LC_ALL": {}, "TERM": {}, "EDITOR": {}, "VISUAL": {}, "HOSTNAME": {},
	"LOGNAME": {}, "XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {}, "XDG_CACHE_HOME": {},
	"GOPATH": {}, "GOROOT": {},
}

// isCommonEnvVar reports whether name is a well-known shell variable. A name
// containing any lowercase letter is never one, so a skill called `home` still
// completes while `$HOME` stays quiet.
func isCommonEnvVar(name string) bool {
	if strings.ContainsFunc(name, unicode.IsLower) {
		return false
	}
	_, ok := commonEnvVars[name]
	return ok
}

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
			// $HOME is a variable; $home may be a skill.
			if isCommonEnvVar(q) {
				return "", 0, 0, false
			}
			// A skill name comes from a directory, so it starts with a letter.
			// This keeps money ($50), shell positionals ($1), and the shell's
			// $_ and $- quiet.
			if r, _ := utf8.DecodeRuneInString(q); q != "" && !unicode.IsLetter(r) {
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
// come from a directory name, so they are letters, digits, '-', and '_'. A ':'
// is allowed for a plugin-qualified name such as `plugin:skill`.
// Rejecting anything else keeps shell text such as ${HOME} and $(pwd) quiet.
func skillQuery(q string) bool {
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == ':' {
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
