package composer

import (
	"strings"

	"github.com/rapatel0/alpha/internal/components/mention"
)

func questionShortcutItems() []mention.Item {
	return []mention.Item{
		{Path: "/", Description: "slash commands"},
		{Path: "!", Description: "shell commands"},
		{Path: "@", Description: "mention files"},
		{Path: "?", Description: "shortcut help"},
		{Path: "Esc", Description: "cancel / stop"},
		{Path: "Ctrl+A", Description: "start of line"},
		{Path: "Ctrl+E", Description: "end of line"},
		{Path: "Ctrl+K", Description: "command palette"},
		{Path: "Ctrl+U", Description: "clear input"},
		{Path: "Ctrl+V", Description: "paste images or text"},
		{Path: "Shift+Enter", Description: "newline"},
	}
}

func filterQuestionItems(query string, all []mention.Item) []mention.Item {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return append([]mention.Item(nil), all...)
	}
	var out []mention.Item
	for _, item := range all {
		if strings.Contains(strings.ToLower(item.Path), q) || strings.Contains(strings.ToLower(item.Description), q) {
			out = append(out, item)
		}
	}
	return out
}
