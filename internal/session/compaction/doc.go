// Package compaction prepares session history for the model.
//
// Compact selects VCC evidence, asks the LLM for a continuation handoff,
// and appends the last non-tool message verbatim. SearchHistory powers
// the vcc_recall tool.
package compaction
