package compaction

import (
	"fmt"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
)

const defaultEvidenceTokens = 24000

// SourceMessage is one transcript excerpt for evidence selection.
type SourceMessage struct {
	EntryID string
	Role    string
	Kind    string
	Text    string
}

// CanonicalRecord is a deduplicated evidence excerpt.
type CanonicalRecord struct {
	ID       string
	EntryID  string
	Role     string
	Kind     string
	Text     string
	Anchors  []string
	Files    []string
	Priority int
	Hash     string
}

// EvidencePacket is the evidence reducer output.
type EvidencePacket struct {
	Anchors         []Anchor
	Records         []CanonicalRecord
	EstimatedTokens int
}

func tokenEstimate(text string) int {
	return (len(text) + 3) / 4
}

func priorityFor(message SourceMessage) int {
	text := message.Text
	if message.Kind == "final-message" {
		return 0
	}
	switch message.Role {
	case "system", "developer", "user":
		return 0
	}
	if matchAny(text, []string{"decision", "decided", "blocker", "blocked", "fix", "fixed", "edit", "edited", "write", "written", "created"}) {
		return 1
	}
	if matchAny(text, []string{"error", "fail", "failed", "warn", "warning", "pass", "test", "exit code", "exception"}) {
		return 2
	}
	if strings.Contains(message.Kind, "tool") || strings.Contains(message.Kind, "read") {
		return 2
	}
	if message.Role == "assistant" {
		return 3
	}
	return 4
}

func matchAny(text string, words []string) bool {
	lower := strings.ToLower(text)
	for _, w := range words {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

func collapseLogLines(text string) string {
	var result []string
	previous := ""
	count := 0
	flush := func() {
		if previous == "" {
			return
		}
		if count > 1 {
			result = append(result, fmt.Sprintf("%s [repeated %d times]", previous, count))
			return
		}
		result = append(result, previous)
	}
	for _, line := range strings.Split(text, "\n") {
		if line == previous {
			count++
			continue
		}
		flush()
		previous = line
		count = 1
	}
	flush()
	return strings.Join(result, "\n")
}

func trimExcerpt(text string, maxTokens int) string {
	if tokenEstimate(text) <= maxTokens {
		return text
	}
	lines := strings.Split(text, "\n")
	var important []string
	for _, line := range lines {
		if matchAny(line, []string{"error", "fail", "warn", "pass", "assert", "expected", "received"}) {
			important = append(important, line)
		}
	}
	head := lines
	if len(head) > 20 {
		head = head[:20]
	}
	tail := lines
	if len(tail) > 20 {
		tail = tail[len(tail)-20:]
	}
	seen := map[string]struct{}{}
	var merged []string
	for _, line := range append(append(head, important...), tail...) {
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		merged = append(merged, line)
	}
	out := strings.Join(merged, "\n")
	budget := maxTokens * 4
	if len(out) <= budget {
		return out
	}
	return out[:budget] + "\n[excerpt truncated]"
}

// BuildEvidence selects high-value excerpts up to maxTokens.
func BuildEvidence(messages []SourceMessage, maxTokens int) EvidencePacket {
	if maxTokens <= 0 {
		maxTokens = defaultEvidenceTokens
	}
	var records []CanonicalRecord
	allAnchors := map[string]Anchor{}
	seenContent := map[string]struct{}{}

	for index, message := range messages {
		collapsed := collapseLogLines(message.Text)
		anchors := extractAnchors(collapsed)
		for _, a := range anchors {
			allAnchors[a.Value] = a
		}
		hash := hashText(message.Role + "\x00" + collapsed)
		if _, dup := seenContent[hash]; dup && message.Role != "user" {
			continue
		}
		seenContent[hash] = struct{}{}
		anchorVals := make([]string, 0, len(anchors))
		for _, a := range anchors {
			anchorVals = append(anchorVals, a.Value)
		}
		records = append(records, CanonicalRecord{
			ID:       fmt.Sprintf("E%d", index+1),
			EntryID:  message.EntryID,
			Role:     message.Role,
			Kind:     message.Kind,
			Text:     trimExcerpt(collapsed, 4000),
			Anchors:  anchorVals,
			Files:    filesFromAnchors(anchors),
			Priority: priorityFor(message),
			Hash:     hash,
		})
	}

	selected := make([]CanonicalRecord, 0, len(records))
	usedTokens := 0
	// Stable insertion: sort by priority without rearranging equal items much.
	order := make([]int, len(records))
	for i := range records {
		order[i] = i
	}
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if records[order[j]].Priority < records[order[i]].Priority {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	for _, idx := range order {
		record := records[idx]
		recordTokens := tokenEstimate(record.Text)
		if usedTokens+recordTokens > maxTokens && record.Priority > 0 {
			continue
		}
		selected = append(selected, record)
		usedTokens += recordTokens
	}

	anchorList := make([]Anchor, 0, len(allAnchors))
	for _, a := range allAnchors {
		anchorList = append(anchorList, a)
	}
	for i := range anchorList {
		anchorList[i].ID = "A" + pad3(i+1)
	}
	return EvidencePacket{Anchors: anchorList, Records: selected, EstimatedTokens: usedTokens}
}

// RenderEvidence formats a packet for the compaction LLM.
func RenderEvidence(packet EvidencePacket) string {
	var anchorLines []string
	for _, a := range packet.Anchors {
		anchorLines = append(anchorLines, fmt.Sprintf("@%s = %s", a.ID, a.Value))
	}
	anchorBlock := "Unknown."
	if len(anchorLines) > 0 {
		anchorBlock = strings.Join(anchorLines, "\n")
	}
	var recordParts []string
	for _, record := range packet.Records {
		source := "unknown entry"
		if record.EntryID != "" {
			source = "entry " + record.EntryID
		}
		lines := []string{
			"[" + record.ID + "]",
			"Kind: " + record.Kind,
			"Source: " + record.Role + ", " + source,
		}
		if len(record.Anchors) > 0 {
			lines = append(lines, "Anchors: "+strings.Join(record.Anchors, ", "))
		}
		lines = append(lines, "Content:", record.Text)
		recordParts = append(recordParts, strings.Join(lines, "\n"))
	}
	recordBlock := "Unknown."
	if len(recordParts) > 0 {
		recordBlock = strings.Join(recordParts, "\n\n")
	}
	return strings.Join([]string{
		"Evidence IDs identify source records.",
		"Anchor values are exact literals from the source.",
		"The final message is preserved verbatim after the handoff.",
		"Use it for semantic state extraction. Do not repeat its full text in the handoff.",
		"A missing record means Unknown.",
		"Do not treat unsupported assistant claims as completed work.",
		"",
		"## Anchor Registry",
		anchorBlock,
		"",
		"## Source Records",
		recordBlock,
	}, "\n")
}

// SourceFromMessages projects LLM messages into source excerpts.
func SourceFromMessages(messages []llm.Message) []SourceMessage {
	var out []SourceMessage
	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			if strings.TrimSpace(msg.Content) != "" {
				out = append(out, SourceMessage{Role: "user", Kind: "message", Text: msg.Content})
			}
		case llm.RoleAssistant:
			if strings.TrimSpace(msg.Content) != "" {
				out = append(out, SourceMessage{Role: "assistant", Kind: "message", Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				out = append(out, SourceMessage{
					Role: "assistant",
					Kind: "tool-call",
					Text: tc.Function.Name + "(" + tc.Function.Arguments + ")",
				})
			}
		case llm.RoleTool:
			if strings.TrimSpace(msg.Content) != "" {
				out = append(out, SourceMessage{Role: "tool", Kind: "tool-result", Text: msg.Content})
			}
		case llm.RoleSystem:
			if strings.TrimSpace(msg.Content) != "" {
				out = append(out, SourceMessage{Role: "system", Kind: "message", Text: msg.Content})
			}
		}
	}
	if last := lastContentMessage(messages); last.Text != "" {
		out = append(out, SourceMessage{Role: last.Role, Kind: "final-message", Text: last.Text})
	}
	return out
}

type contentMsg struct {
	Role string
	Text string
}

func lastContentMessage(messages []llm.Message) contentMsg {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == llm.RoleTool {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		return contentMsg{Role: string(msg.Role), Text: msg.Content}
	}
	return contentMsg{}
}

// VerbatimLast returns the last non-tool message text. Tool-call-only
// assistant turns are skipped.
func VerbatimLast(messages []llm.Message) string {
	return lastContentMessage(messages).Text
}
