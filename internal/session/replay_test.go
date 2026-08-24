package session

import (
	"testing"

	"github.com/pulseaiclub/phi/internal/llm"
)

func TestSnapshotFromEntriesReplaysTools(t *testing.T) {
	entries := []MessageEntry{
		SessionHeader{Type: EntrySession, ID: "s1"},
		SessionMessageEntry{
			SessionBaseEntry: SessionBaseEntry{Type: EntryMessage, ID: "u1"},
			Message:          llm.Message{Role: llm.RoleUser, Content: "fan out"},
		},
		SessionMessageEntry{
			SessionBaseEntry: SessionBaseEntry{Type: EntryMessage, ID: "a1"},
			Message: llm.Message{
				Role:    llm.RoleAssistant,
				Content: "spawning",
				ToolCalls: []llm.ToolCall{{
					ID:   "t1",
					Type: "function",
					Function: llm.Function{
						Name:      "agent_spawn",
						Arguments: `{"role":"review","description":"Review TUI"}`,
					},
				}},
			},
			Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14},
		},
		SessionMessageEntry{
			SessionBaseEntry: SessionBaseEntry{Type: EntryMessage, ID: "r1"},
			Message: llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: "t1",
				Content:    `{"job_id":"job_abc","description":"Review TUI"}`,
			},
		},
		SessionMessageEntry{
			SessionBaseEntry: SessionBaseEntry{Type: EntryMessage, ID: "a2"},
			Message: llm.Message{
				Role:    llm.RoleAssistant,
				Content: "",
				ToolCalls: []llm.ToolCall{{
					ID:       "t2",
					Type:     "function",
					Function: llm.Function{Name: "bash", Arguments: `{"command":"ls"}`},
				}},
			},
		},
		SessionMessageEntry{
			SessionBaseEntry: SessionBaseEntry{Type: EntryMessage, ID: "r2"},
			Message: llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: "t2",
				Content:    cancelledToolContent,
			},
		},
	}
	snap := SnapshotFromEntries(entries)
	if len(snap.Messages) != 3 {
		t.Fatalf("messages=%d", len(snap.Messages))
	}
	if !snap.Messages[1].Usage.Reported() || snap.Messages[1].Usage.TotalTokens != 14 {
		t.Fatalf("usage %+v", snap.Messages[1].Usage)
	}
	if snap.Tools["t1"].Status != ToolDone || snap.Tools["t1"].Output == "" {
		t.Fatalf("t1 %+v", snap.Tools["t1"])
	}
	if snap.Tools["t2"].Status != ToolCancelled {
		t.Fatalf("t2 %+v", snap.Tools["t2"])
	}
	items := Project(snap)
	kinds := make([]ItemKind, len(items))
	var sawSpawn, sawBash bool
	for i, it := range items {
		kinds[i] = it.Kind
		if it.Kind == ItemTool && it.ToolName == "agent_spawn" {
			sawSpawn = true
		}
		if it.Kind == ItemTool && it.ToolName == "bash" {
			sawBash = true
		}
	}
	if !sawSpawn || !sawBash {
		t.Fatalf("missing tool rows: %+v", kinds)
	}
}
