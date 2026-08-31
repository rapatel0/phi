package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/rapatel0/alpha/internal/llm"
)

type fakeCompactor struct {
	prompt string
}

func (f *fakeCompactor) Compact(_ context.Context, summary string) (string, error) {
	f.prompt = summary
	return "# Continuation Handoff\n\n## Active Goal\nfix toast\n", nil
}

func TestGenerateSummarySendsEvidenceNotRawDump(t *testing.T) {
	f := &fakeCompactor{}
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "fix the toast pointer in editor.go"},
		{Role: llm.RoleAssistant, Content: "I will pass *toast.Toast"},
	}
	out, err := generateSummary(context.Background(), f, msgs, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Active Goal") {
		t.Fatalf("handoff=%q", out)
	}
	if !strings.Contains(f.prompt, "## Source Records") {
		t.Fatalf("prompt missing evidence:\n%s", f.prompt)
	}
	if strings.Contains(f.prompt, "[User]:") {
		t.Fatalf("prompt still uses raw dump:\n%s", f.prompt)
	}
}

func TestCompactAppendsLastMessage(t *testing.T) {
	f := &fakeCompactor{}
	prep := CompactionPreparation{
		FirstKeptEntryId:    "keep",
		MessagesToSummarize: []llm.Message{{Role: llm.RoleUser, Content: "hello world"}},
	}
	res, err := Compact(context.Background(), prep, f, Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Summary, verbatimHeading) || !strings.Contains(res.Summary, "hello world") {
		t.Fatalf("summary=%q", res.Summary)
	}
}
