package compaction

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/session"
)

// RecallHit is one raw-history match.
type RecallHit struct {
	Index int
	Role  string
	Text  string
}

// SearchHistory finds query in session entries. Query "#N" expands that index.
func SearchHistory(entries []session.MessageEntry, query string, limit int) []RecallHit {
	if limit <= 0 {
		limit = 10
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	hits := make([]RecallHit, 0, limit)
	if strings.HasPrefix(query, "#") {
		n, err := strconv.Atoi(strings.TrimPrefix(query, "#"))
		if err == nil && n >= 0 && n < len(entries) {
			if hit, ok := hitFromEntry(n, entries[n]); ok {
				return []RecallHit{hit}
			}
		}
	}
	q := strings.ToLower(query)
	for i, entry := range entries {
		hit, ok := hitFromEntry(i, entry)
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(hit.Text), q) {
			hits = append(hits, hit)
			if len(hits) >= limit {
				break
			}
		}
	}
	return hits
}

func hitFromEntry(index int, entry session.MessageEntry) (RecallHit, bool) {
	msg := getMessageFromEntry(entry)
	if msg == nil {
		if entry.GetType() == session.EntryCompaction {
			comp := entry.(session.CompactionEntry)
			text := comp.Compaction.Summary
			if strings.TrimSpace(text) == "" {
				return RecallHit{}, false
			}
			return RecallHit{Index: index, Role: "compaction", Text: text}, true
		}
		return RecallHit{}, false
	}
	text := strings.TrimSpace(msg.Content)
	if text == "" && len(msg.ToolCalls) > 0 {
		var parts []string
		for _, tc := range msg.ToolCalls {
			parts = append(parts, tc.Function.Name+"("+tc.Function.Arguments+")")
		}
		text = strings.Join(parts, "; ")
	}
	if text == "" {
		return RecallHit{}, false
	}
	role := string(msg.Role)
	if role == "" {
		role = "unknown"
	}
	if msg.Role == llm.RoleTool {
		role = "tool"
	}
	return RecallHit{Index: index, Role: role, Text: text}, true
}

// FormatHits renders recall results for the model.
func FormatHits(hits []RecallHit) string {
	if len(hits) == 0 {
		return "No matches."
	}
	var b strings.Builder
	for _, h := range hits {
		text := h.Text
		trunc := false
		if len(text) > 1000 {
			text = text[:1000]
			trunc = true
		}
		fmt.Fprintf(&b, "#%d [%s]\n%s", h.Index, h.Role, text)
		if trunc {
			b.WriteString("\n[truncated. Use #N to expand.]")
		}
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}
