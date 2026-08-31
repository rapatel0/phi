package util

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeLF converts CRLF and lone CR line endings to LF.
func NormalizeLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

// Truncate cuts s to n bytes and appends an ellipsis when it was longer.
func Truncate(s string, n int) string {
	if n < 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// MustJSON marshals v. On failure it returns a small error object.
func MustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"error":"marshal failed"}`
	}
	return string(b)
}

// MustJSONIndent marshals v with indent. On failure it returns fmt %v.
func MustJSONIndent(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
