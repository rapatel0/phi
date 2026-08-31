package compaction

import "testing"

func TestLedgerRoundTrip(t *testing.T) {
	t.Setenv("ALPHA_VCC_DIR", t.TempDir())
	recs := []CanonicalRecord{{
		EntryID: "e1",
		Role:    "user",
		Text:    "fix toast pointer",
		Hash:    "abc",
	}}
	if err := RecordEvidence("sess1", "/tmp/s.jsonl", recs); err != nil {
		t.Fatal(err)
	}
	if err := RecordCompaction("sess1", "/tmp/s.jsonl", "keep", recs, "summary"); err != nil {
		t.Fatal(err)
	}
	led := LoadLedger("sess1", "")
	if len(led.Entries) != 1 || led.Entries[0].Text != "fix toast pointer" {
		t.Fatalf("%+v", led.Entries)
	}
	if len(led.Compactions) != 1 || led.Compactions[0].FirstKeptEntryID != "keep" {
		t.Fatalf("%+v", led.Compactions)
	}
	hits := SearchLedger("sess1", "toast", 10)
	if len(hits) != 1 {
		t.Fatalf("hits=%d", len(hits))
	}
}
