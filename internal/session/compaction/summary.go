package compaction

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/rapatel0/alpha/internal/llm"
)

var (
	//go:embed compaction-summary.tmpl
	compactionSummaryPrompt string
	//go:embed compaction-update-summary.tmpl
	compactionUpdateSummaryPrompt string
)

func handoffPrompt(messages []llm.Message, previousSummary string, maxTokens int) (string, EvidencePacket) {
	if maxTokens <= 0 {
		maxTokens = defaultEvidenceTokens
	}
	packet := BuildEvidence(SourceFromMessages(messages), maxTokens)
	prompt := fmt.Sprintf("<evidence>\n%s\n</evidence>", RenderEvidence(packet))
	base := compactionSummaryPrompt
	if previousSummary != "" {
		prompt += fmt.Sprintf("\n<previous-summary>\n%s\n</previous-summary>", previousSummary)
		base = compactionUpdateSummaryPrompt
	}
	return prompt + "\n" + base, packet
}

func generateSummary(
	ctx context.Context,
	compactor llm.Compactor,
	currentMessages []llm.Message,
	previousSummary string,
) (string, error) {
	prompt, _ := handoffPrompt(currentMessages, previousSummary, LoadConfig().MaxInputTokens)
	return compactor.Compact(ctx, prompt)
}

func generateTurnPrefixSummary(
	ctx context.Context,
	compactor llm.Compactor,
	messages []llm.Message,
) (string, error) {
	prompt, _ := handoffPrompt(messages, "", LoadConfig().MaxInputTokens)
	return compactor.Compact(ctx, prompt)
}
