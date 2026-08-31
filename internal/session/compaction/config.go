package compaction

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rapatel0/alpha/internal/brand"
	"github.com/rapatel0/alpha/internal/llm"
)

const (
	transportAuto            = "auto"
	transportPi              = "pi"
	transportOpenAIResponses = "openai-responses"
)

// CompactorSpec names a model used to write the handoff.
// An empty Name means the session model.
type CompactorSpec struct {
	Name      string `json:"name,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

// Config controls VCC compaction. Missing fields keep the defaults.
type Config struct {
	Enabled                bool                     `json:"enabled"`
	Transport              string                   `json:"transport"`
	Compactor              CompactorSpec            `json:"compactor"`
	Compactors             map[string]CompactorSpec `json:"compactors"`
	AllowedResponseOrigins []string                 `json:"allowedResponseOrigins"`
	MaxInputTokens         int                      `json:"maxInputTokens"`
	MaxOutputTokens        int                      `json:"maxOutputTokens"`
	TimeoutMs              int                      `json:"timeoutMs"`
	Index                  IndexConfig              `json:"index"`
}

// IndexConfig is the deterministic VCC ledger. It is not an LLM.
type IndexConfig struct {
	Enabled          bool `json:"enabled"`
	MaxSearchResults int  `json:"maxSearchResults"`
}

func defaultConfig() Config {
	return Config{
		Enabled:   true,
		Transport: transportAuto,
		AllowedResponseOrigins: []string{
			"https://api.openai.com",
			"https://chatgpt.com",
		},
		MaxInputTokens:  24000,
		MaxOutputTokens: 5000,
		TimeoutMs:       120000,
		Index: IndexConfig{
			Enabled:          true,
			MaxSearchResults: 10,
		},
	}
}

func vccDir() string {
	if d := strings.TrimSpace(os.Getenv("ALPHA_VCC_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(brand.HomeDir(home), "vcc-llm-compaction")
}

// ConfigPath is ~/.alpha/vcc-llm-compaction/config.json.
func ConfigPath() string {
	dir := vccDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.json")
}

// EnsureGlobalConfig writes the default config when the file is missing.
func EnsureGlobalConfig() error {
	path := ConfigPath()
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(defaultConfig(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// LoadConfig reads the global config. Missing files use defaults.
func LoadConfig() Config {
	cfg := defaultConfig()
	path := ConfigPath()
	if path == "" {
		return cfg
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	var file map[string]json.RawMessage
	if json.Unmarshal(raw, &file) != nil {
		return cfg
	}
	applyJSON(&cfg.Transport, file["transport"])
	applyJSON(&cfg.Compactor, file["compactor"])
	applyJSON(&cfg.Compactors, file["compactors"])
	applyJSON(&cfg.AllowedResponseOrigins, file["allowedResponseOrigins"])
	applyJSON(&cfg.MaxInputTokens, file["maxInputTokens"])
	applyJSON(&cfg.MaxOutputTokens, file["maxOutputTokens"])
	applyJSON(&cfg.TimeoutMs, file["timeoutMs"])
	if v, ok := file["enabled"]; ok {
		_ = json.Unmarshal(v, &cfg.Enabled)
	}
	if v, ok := file["index"]; ok {
		var idx IndexConfig
		if json.Unmarshal(v, &idx) == nil {
			if idx.MaxSearchResults > 0 {
				cfg.Index.MaxSearchResults = idx.MaxSearchResults
			}
			var flags struct {
				Enabled *bool `json:"enabled"`
			}
			if json.Unmarshal(v, &flags) == nil && flags.Enabled != nil {
				cfg.Index.Enabled = *flags.Enabled
			}
		}
	}
	if cfg.MaxInputTokens <= 0 {
		cfg.MaxInputTokens = 24000
	}
	if cfg.MaxOutputTokens <= 0 {
		cfg.MaxOutputTokens = 5000
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 120000
	}
	if cfg.Transport == "" {
		cfg.Transport = transportAuto
	}
	return cfg
}

func applyJSON[T any](dst *T, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	_ = json.Unmarshal(raw, dst)
}

// ResolveCompactor picks per-model override, then global, then the session model.
func ResolveCompactor(session llm.ModelConfig, cfg Config) llm.ModelConfig {
	spec := cfg.Compactor
	if cfg.Compactors != nil {
		if override, ok := cfg.Compactors[session.Name]; ok {
			spec = override
		}
	}
	if spec.Name == "" || spec.Name == "session" {
		return session
	}
	out := session
	out.Name = spec.Name
	if spec.BaseURL != "" {
		out.BaseURL = spec.BaseURL
	}
	if spec.APIKeyEnv != "" {
		if v := strings.TrimSpace(os.Getenv(spec.APIKeyEnv)); v != "" {
			out.APIKey = v
		}
	}
	return out
}
