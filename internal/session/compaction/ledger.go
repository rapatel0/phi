package compaction

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LedgerEntry is one deterministic evidence row. It is not LLM output.
type LedgerEntry struct {
	EntryID string   `json:"entryId"`
	Index   int      `json:"index"`
	Role    string   `json:"role"`
	Text    string   `json:"text"`
	Anchors []string `json:"anchors"`
	Files   []string `json:"files"`
}

// LedgerCompaction is provenance for one compact run.
type LedgerCompaction struct {
	FirstKeptEntryID string   `json:"firstKeptEntryId"`
	SourceEntryIDs   []string `json:"sourceEntryIds"`
	SummaryHash      string   `json:"summaryHash"`
}

// SessionLedger is the derived VCC index. Raw session JSONL stays source of truth.
type SessionLedger struct {
	Version     int                `json:"version"`
	SessionID   string             `json:"sessionId"`
	SessionFile string             `json:"sessionFile,omitempty"`
	UpdatedAt   int64              `json:"updatedAt"`
	Entries     []LedgerEntry      `json:"entries"`
	Compactions []LedgerCompaction `json:"compactions"`
}

// LedgerPath is ~/.alpha/vcc-llm-compaction/sessions/<id>.json.
func LedgerPath(sessionID string) string {
	dir := vccDir()
	if dir == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(dir, "sessions", sessionID+".json")
}

func emptyLedger(sessionID, sessionFile string) SessionLedger {
	return SessionLedger{
		Version:     1,
		SessionID:   sessionID,
		SessionFile: sessionFile,
		UpdatedAt:   time.Now().UnixMilli(),
	}
}

// LoadLedger reads the derived index. A missing file is an empty ledger.
func LoadLedger(sessionID, sessionFile string) SessionLedger {
	path := LedgerPath(sessionID)
	if path == "" {
		return emptyLedger(sessionID, sessionFile)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return emptyLedger(sessionID, sessionFile)
	}
	var led SessionLedger
	if json.Unmarshal(raw, &led) != nil || led.Version != 1 {
		return emptyLedger(sessionID, sessionFile)
	}
	if led.SessionFile == "" {
		led.SessionFile = sessionFile
	}
	return led
}

// SaveLedger writes the derived index.
func SaveLedger(led SessionLedger) error {
	path := LedgerPath(led.SessionID)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	led.UpdatedAt = time.Now().UnixMilli()
	raw, err := json.MarshalIndent(led, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// RecordEvidence appends new source records. Duplicate entry IDs are skipped.
func RecordEvidence(sessionID, sessionFile string, records []CanonicalRecord) error {
	if sessionID == "" {
		return nil
	}
	led := LoadLedger(sessionID, sessionFile)
	known := make(map[string]struct{}, len(led.Entries))
	for _, e := range led.Entries {
		known[e.EntryID] = struct{}{}
	}
	for _, rec := range records {
		id := rec.EntryID
		if id == "" {
			id = rec.Hash
		}
		if id == "" {
			continue
		}
		if _, ok := known[id]; ok {
			continue
		}
		known[id] = struct{}{}
		led.Entries = append(led.Entries, LedgerEntry{
			EntryID: id,
			Index:   len(led.Entries),
			Role:    rec.Role,
			Text:    rec.Text,
			Anchors: rec.Anchors,
			Files:   rec.Files,
		})
	}
	return SaveLedger(led)
}

// RecordCompaction appends provenance for one compact run.
func RecordCompaction(sessionID, sessionFile, firstKept string, records []CanonicalRecord, summary string) error {
	if sessionID == "" {
		return nil
	}
	led := LoadLedger(sessionID, sessionFile)
	var ids []string
	for _, rec := range records {
		if rec.EntryID != "" {
			ids = append(ids, rec.EntryID)
		}
	}
	led.Compactions = append(led.Compactions, LedgerCompaction{
		FirstKeptEntryID: firstKept,
		SourceEntryIDs:   ids,
		SummaryHash:      hashText(summary),
	})
	return SaveLedger(led)
}

// SearchLedger finds query in derived evidence text.
func SearchLedger(sessionID, query string, limit int) []RecallHit {
	if limit <= 0 {
		limit = 10
	}
	led := LoadLedger(sessionID, "")
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var hits []RecallHit
	for _, e := range led.Entries {
		if !strings.Contains(strings.ToLower(e.Text), q) && !strings.Contains(strings.ToLower(e.EntryID), q) {
			continue
		}
		hits = append(hits, RecallHit{Index: e.Index, Role: e.Role, Text: e.Text})
		if len(hits) >= limit {
			break
		}
	}
	return hits
}
