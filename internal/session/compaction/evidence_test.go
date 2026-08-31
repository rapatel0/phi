package compaction

import (
	"strings"
	"testing"

	"github.com/rapatel0/alpha/internal/llm"
)

func TestBuildEvidenceKeepsUserAndDropsDuplicateAssistant(t *testing.T) {
	msgs := []SourceMessage{
		{Role: "user", Kind: "message", Text: "fix the toast pointer"},
		{Role: "assistant", Kind: "message", Text: "I will edit editor.go"},
		{Role: "assistant", Kind: "message", Text: "I will edit editor.go"},
		{Role: "tool", Kind: "tool-result", Text: "error: boom\nerror: boom\nerror: boom"},
	}
	packet := BuildEvidence(msgs, 8000)
	var roles []string
	for _, r := range packet.Records {
		roles = append(roles, r.Role+":"+r.Kind)
	}
	joined := strings.Join(roles, ",")
	if !strings.Contains(joined, "user:message") {
		t.Fatalf("missing user record: %s", joined)
	}
	nAssist := 0
	for _, r := range packet.Records {
		if r.Role == "assistant" && r.Kind == "message" {
			nAssist++
		}
	}
	if nAssist != 1 {
		t.Fatalf("duplicate assistant kept: %d in %s", nAssist, joined)
	}
	foundRepeat := false
	for _, r := range packet.Records {
		if strings.Contains(r.Text, "repeated") {
			foundRepeat = true
		}
	}
	if !foundRepeat {
		t.Fatal("expected collapsed repeated log lines")
	}
}

func TestVerbatimLastSkipsToolOnlyAssistant(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{Function: llm.Function{Name: "read"}}}},
		{Role: llm.RoleTool, Content: "file bytes"},
	}
	if got := VerbatimLast(msgs); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestSourceFromMessagesAddsFinal(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "do the thing"},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	src := SourceFromMessages(msgs)
	last := src[len(src)-1]
	if last.Kind != "final-message" || last.Text != "done" {
		t.Fatalf("%+v", last)
	}
}
