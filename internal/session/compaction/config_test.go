package compaction

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rapatel0/alpha/internal/llm"
)

func TestResolveCompactorOrder(t *testing.T) {
	session := llm.ModelConfig{Name: "claude-sonnet", BaseURL: "https://api.anthropic.com", APIKey: "k"}
	cfg := defaultConfig()
	if got := ResolveCompactor(session, cfg); got.Name != "claude-sonnet" {
		t.Fatalf("session default = %s", got.Name)
	}
	cfg.Compactor = CompactorSpec{Name: "gpt-4.1-mini"}
	if got := ResolveCompactor(session, cfg); got.Name != "gpt-4.1-mini" {
		t.Fatalf("global = %s", got.Name)
	}
	cfg.Compactors = map[string]CompactorSpec{
		"claude-sonnet": {Name: "gpt-4.1", BaseURL: "https://api.openai.com/v1"},
	}
	got := ResolveCompactor(session, cfg)
	if got.Name != "gpt-4.1" || got.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("per-model = %+v", got)
	}
}

func TestLoadConfigEnabledFalse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ALPHA_COMPACT_DIR", dir)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"enabled":false,"index":{"enabled":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig()
	if cfg.Enabled {
		t.Fatal("enabled should be false")
	}
	if cfg.Index.Enabled {
		t.Fatal("index.enabled should be false")
	}
}

func TestEnsureGlobalConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ALPHA_COMPACT_DIR", dir)
	if err := EnsureGlobalConfig(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGlobalConfig(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigThresholdPercent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ALPHA_COMPACT_DIR", dir)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"thresholdPercent":80}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig()
	if cfg.ThresholdPercent != 80 {
		t.Fatalf("percent=%d", cfg.ThresholdPercent)
	}
	settings := DefaultSettings().WithThresholdPercent(cfg.ThresholdPercent)
	if ShouldCompact(79, 100, settings) {
		t.Fatal("79 of 80 must not compact")
	}
	if !ShouldCompact(80, 100, settings) {
		t.Fatal("80 of 80 must compact")
	}
}

