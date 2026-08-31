// Package compaction prepares session history for the model.
//
// Compact selects VCC evidence, then asks the current model (or a configured
// compact model) for a continuation handoff. OpenAI /responses/compact is
// fallback only. The ledger under ~/.alpha/vcc-llm-compaction is a
// deterministic index, not an LLM. vcc_recall searches raw history and that
// ledger.
package compaction
