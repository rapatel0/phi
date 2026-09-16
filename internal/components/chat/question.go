package chat

import "strings"

// ActiveQuestion reports a leading ? shortcut-help token under the cursor.
func ActiveQuestion(value string, cursor int) (query string, start, end int, ok bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	if !strings.HasPrefix(value, "?") || cursor < 1 {
		return "", 0, 0, false
	}
	for i := 1; i < len(value); i++ {
		if strings.ContainsRune(" \t\n\r", rune(value[i])) {
			if cursor > i {
				return "", 0, 0, false
			}
			break
		}
	}
	return value[1:cursor], 0, cursor, true
}
