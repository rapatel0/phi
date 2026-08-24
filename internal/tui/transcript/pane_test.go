package transcript

import (
	"testing"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/status"
	"github.com/rapatel0/alpha/internal/session"
)

func TestTranscriptPane_ApplySessionAndSync(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	pane := NewTranscriptPane(th, spin, "Alpha test")

	pane.ApplySession(session.UserAppend{Text: "hello"})
	pane.Sync()

	if pane.IsEmpty() {
		t.Fatal("expected transcript entries after user append")
	}
	if len(pane.Snapshot().Messages) != 1 {
		t.Fatalf("snap messages = %d, want 1", len(pane.Snapshot().Messages))
	}
}

func TestTranscriptPane_IsStreaming(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	pane := NewTranscriptPane(th, spin, "Alpha test")

	if pane.IsStreaming() {
		t.Fatal("empty pane should not stream")
	}

	pane.ApplySession(session.AssistantMessageUpdate{Message: session.Message{
		ID:    "a1",
		State: session.StateStreaming,
	}})
	if !pane.IsStreaming() {
		t.Fatal("expected streaming after assistant StateStreaming")
	}

	pane.ApplySession(session.AssistantMessageUpdate{Message: session.Message{
		ID:    "a1",
		State: session.StateComplete,
	}})
	if pane.IsStreaming() {
		t.Fatal("expected idle after StreamEnd")
	}
}

func TestTranscriptPane_LoadReplayClearsWidgets(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	pane := NewTranscriptPane(th, spin, "Alpha test")

	pane.ApplySession(session.UserAppend{Text: "x"})
	pane.Sync()
	if pane.IsEmpty() {
		t.Fatal("setup: expected entries")
	}

	pane.LoadReplay(session.Snapshot{})
	pane.Sync()
	if !pane.IsEmpty() {
		t.Fatal("LoadReplay should clear visible entries until snap has items")
	}
}

func TestTranscriptPane_TakeAndRestoreSubagents(t *testing.T) {
	th := components.DefaultTheme()
	pane := NewTranscriptPane(th, status.NewSpinner(th.ToolName), "Alpha test")
	old := pane.TakeSubagents()
	if old == nil {
		t.Fatal("expected previous store")
	}
	if pane.subagents == old {
		t.Fatal("TakeSubagents should replace the store")
	}
	pane.RestoreSubagents(old)
	if pane.subagents != old {
		t.Fatal("RestoreSubagents should put the store back")
	}
}
