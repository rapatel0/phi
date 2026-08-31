package compaction

import "strings"

const (
	recallHeading    = "## Recall Index"
	toolStateHeading = "## Tool and File State"
	verbatimHeading  = "## Verbatim Last Pre-Compaction Message"
)

var requiredHeadings = []string{
	"## Active Goal",
	"## Constraints and Steering",
	"## Current State",
	"## Completed Work and Evidence",
	"## Next Actions",
	"## Exact Anchors",
	toolStateHeading,
	"## Todos and Decisions",
	"## Missing Information and Risks",
	"## Evidence Quality",
}

func validateSummary(summary string, maxTokens int) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	if maxTokens > 0 && tokenEstimate(summary) > maxTokens {
		return ""
	}
	if strings.Contains(summary, recallHeading) {
		return ""
	}
	prev := -1
	for _, heading := range requiredHeadings {
		i := strings.Index(summary, heading)
		if i < 0 || i <= prev {
			return ""
		}
		prev = i
	}
	return summary
}

func insertRecallIndex(summary, sessionID, sessionFile string) string {
	raw := sessionFile
	if strings.TrimSpace(raw) == "" {
		raw = "Unknown"
	}
	led := LedgerPath(sessionID)
	if led == "" {
		led = "Unknown"
	}
	section := recallHeading + "\n- Tool: `recall`\n- Raw session source: `" + raw + "`\n- Derived ledger: `" + led + "`"
	if strings.Contains(summary, toolStateHeading) {
		return strings.Replace(summary, toolStateHeading, section+"\n\n"+toolStateHeading, 1)
	}
	return strings.TrimSpace(summary) + "\n\n" + section
}

func appendVerbatimTail(summary, role, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return summary
	}
	if role == "" {
		role = "unknown"
	}
	return strings.TrimSpace(summary) + "\n\n" + verbatimHeading + "\n- Role: " + role + "\n\n" + text
}
