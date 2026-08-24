package session

import (
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
)

const cancelledToolContent = "User cancelled the tool call."

// SnapshotFromEntries projects persisted session entries into a UI snapshot
// (user/assistant text, tool_use rows, and tool results).
func SnapshotFromEntries(entries []MessageEntry) Snapshot {
	var snap Snapshot
	for _, entry := range entries {
		switch entry.GetType() {
		case EntryCompaction:
			snap = Apply(snap, CompactionComplete{ID: entry.GetID()})
		case EntryMessage:
			ent := entry.(SessionMessageEntry)
			msg := ent.Message
			switch msg.Role {
			case llm.RoleUser:
				var names []string
				for _, img := range msg.Images {
					names = append(names, img.Label())
				}
				snap = Apply(
					snap,
					UserAppend{ID: entry.GetID(), Text: msg.Content, Images: names, ImageData: msg.Images},
				)
			case llm.RoleAssistant:
				snap = Apply(snap, AssistantMessageUpdate{Message: replayAssistant(entry.GetID(), msg, ent.Usage)})
			case llm.RoleTool:
				snap = Apply(snap, ToolData{Run: replayToolRun(msg)})
			}
		}
	}
	return snap
}

func replayAssistant(id string, msg llm.Message, usage llm.Usage) Message {
	text := msg.Content
	var blocks []ContentBlock
	if trim := strings.TrimSpace(msg.ReasoningContent); trim != "" {
		blocks = append(blocks, ContentBlock{Type: BlockThinking, Text: msg.ReasoningContent})
	}
	if text != "" {
		blocks = append(blocks, ContentBlock{Type: BlockText, Text: text})
	}
	for _, tc := range msg.ToolCalls {
		blocks = append(blocks, ContentBlock{
			Type:     BlockToolUse,
			ID:       tc.ID,
			Name:     tc.Function.Name,
			Input:    strings.TrimSpace(tc.Function.Arguments),
			Complete: true,
		})
	}
	reason := StopEndTurn
	if len(msg.ToolCalls) > 0 {
		reason = StopToolUse
	}
	return Message{
		ID:         id,
		State:      StateComplete,
		StopReason: reason,
		Text:       text,
		Content:    blocks,
		Usage: TokenUsage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			CachedTokens:     usage.CachedTokens(),
			TotalTokens:      usage.TotalTokens,
		},
	}
}

func replayToolRun(msg llm.Message) ToolRun {
	st := ToolDone
	if msg.Content == cancelledToolContent {
		st = ToolCancelled
	}
	return ToolRun{
		ToolUseID: msg.ToolCallID,
		Status:    st,
		Output:    msg.Content,
	}
}
