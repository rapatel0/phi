// Package compaction prepares session history for the model.
//
// Compact selects high-value evidence, then asks the current model (or a
// configured compact model) for a continuation handoff. OpenAI
// /responses/compact is fallback only. Autocompact runs at 95% of the
// context window or 400k tokens, whichever is first (config
// thresholdPercent and thresholdTokens). The ledger under
// ~/.alpha/compaction is a deterministic index. The recall tool searches
// raw history and that ledger.
package compaction
