package vcctool

import (
	"context"
	"testing"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/session"
)

func TestRecallToolFindsHistory(t *testing.T) {
	entries := []session.MessageEntry{
		session.SessionMessageEntry{
			SessionBaseEntry: session.SessionBaseEntry{ID: "e1"},
			Message:          llm.Message{Role: llm.RoleUser, Content: "fix toast pointer"},
		},
	}
	tl := Tool(func() []session.MessageEntry { return entries })
	if tl.Definition.Name != "vcc_recall" {
		t.Fatalf("name=%s", tl.Definition.Name)
	}
	res, err := tl.Run(context.Background(), []byte(`{"query":"toast"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Content == "" || res.Content == "No matches." {
		t.Fatalf("content=%q", res.Content)
	}
}
