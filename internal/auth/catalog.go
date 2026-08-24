package auth

import (
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
)

const (
	anthropicBase = "https://api.anthropic.com"
	geminiBase    = "https://generativelanguage.googleapis.com/v1beta"
	xaiBase       = "https://api.x.ai/v1"
)

// Catalog is the TUI palette list for a logged-in provider. Names already in
// config.yaml are not duplicated by the injector.
func Catalog(provider string) []llm.ModelConfig {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ProviderAnthropic:
		return []llm.ModelConfig{
			entry("claude-opus-5", anthropicBase, 200_000),
			entry("claude-opus-4-8", anthropicBase, 200_000),
			entry("claude-sonnet-5", anthropicBase, 200_000),
			entry("claude-sonnet-4-6", anthropicBase, 200_000),
			entry("claude-haiku-4-5", anthropicBase, 200_000),
		}
	case ProviderCodex:
		return []llm.ModelConfig{
			entry("gpt-5.5", CodexBackendBaseURL, 272_000),
			entry("gpt-5.4", CodexBackendBaseURL, 272_000),
			entry("gpt-5.4-codex", CodexBackendBaseURL, 272_000),
		}
	case ProviderXAI:
		return []llm.ModelConfig{
			entry("grok-4.6", xaiBase, 500_000),
			entry("grok-4.5", xaiBase, 256_000),
		}
	case ProviderGemini:
		return []llm.ModelConfig{
			entry("gemini-2.5-pro", geminiBase, 1_000_000),
			entry("gemini-2.5-flash", geminiBase, 1_000_000),
		}
	default:
		return nil
	}
}

func entry(name, base string, window int) llm.ModelConfig {
	return llm.ModelConfig{Name: name, BaseURL: base, ContextWindow: window}
}
