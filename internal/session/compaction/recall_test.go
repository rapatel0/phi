package compaction

import (
	"strings"
	"testing"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/session"
)

func TestSearchHistoryFindsTextAndIndex(t *testing.T) {
	entries := []session.MessageEntry{
		session.SessionMessageEntry{
			SessionBaseEntry: session.SessionBaseEntry{ID: "e1"},
			Message:          llm.Message{Role: llm.RoleUser, Content: "fix toast pointer"},
		},
		session.SessionMessageEntry{
			SessionBaseEntry: session.SessionBaseEntry{ID: "e2"},
			Message:          llm.Message{Role: llm.RoleAssistant, Content: "pass *toast.Toast"},
		},
	}
	hits := SearchHistory(entries, "toast", 10)
	if len(hits) != 2 {
		t.Fatalf("hits=%d", len(hits))
	}
	one := SearchHistory(entries, "#1", 10)
	if len(one) != 1 || !strings.Contains(one[0].Text, "toast.Toast") {
		t.Fatalf("%+v", one)
	}
}
