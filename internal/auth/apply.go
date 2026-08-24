package auth

import (
	"context"
	"strings"

	"github.com/pulseaiclub/phi/internal/llm"
)

// Apply fills cfg.APIKey from ~/.phi/auth.json when the config has no key.
// Expired tokens are refreshed in place. Existing API keys are left alone.
func Apply(ctx context.Context, cfg *llm.ModelConfig, storePath string) error {
	if cfg == nil || strings.TrimSpace(cfg.APIKey) != "" {
		return nil
	}
	st := OpenStore(storePath)
	provider := providerFor(*cfg)
	if provider == "" {
		return nil
	}
	cred, ok := st.Get(provider)
	if !ok {
		return nil
	}
	if cred.Expired() && cred.RefreshToken != "" {
		fresh, err := refresh(ctx, provider, cred.RefreshToken)
		if err != nil {
			return err
		}
		if fresh.RefreshToken == "" {
			fresh.RefreshToken = cred.RefreshToken
		}
		if err := st.Put(fresh); err != nil {
			return err
		}
		cred = fresh
	}
	cfg.APIKey = cred.AccessToken
	if provider == ProviderAnthropic && cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if provider == ProviderCodex && (cfg.BaseURL == "" || strings.Contains(cfg.BaseURL, "api.openai.com")) {
		// ChatGPT backend, not platform /v1 — platform rejects oat JWTs.
		cfg.BaseURL = CodexBackendBaseURL
	}
	if provider == ProviderXAI && cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.x.ai/v1"
	}
	if provider == ProviderGemini && cfg.BaseURL == "" {
		cfg.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return nil
}

// CodexBackendBaseURL is the ChatGPT Codex responses endpoint origin.
const CodexBackendBaseURL = "https://chatgpt.com/backend-api/codex"

func providerFor(cfg llm.ModelConfig) string {
	base := strings.ToLower(cfg.BaseURL)
	name := strings.ToLower(cfg.Name)
	if strings.Contains(base, "anthropic") || strings.HasPrefix(name, "claude") {
		return ProviderAnthropic
	}
	if strings.Contains(base, "x.ai") || strings.HasPrefix(name, "grok") {
		return ProviderXAI
	}
	if strings.Contains(base, "generativelanguage.googleapis.com") || strings.HasPrefix(name, "gemini") {
		return ProviderGemini
	}
	if strings.Contains(name, "codex") || strings.HasPrefix(name, "gpt-") || strings.HasPrefix(name, "o1") ||
		strings.HasPrefix(name, "o3") ||
		strings.HasPrefix(name, "o4") {
		return ProviderCodex
	}
	return ""
}

func refresh(ctx context.Context, provider, refreshToken string) (Credential, error) {
	switch provider {
	case ProviderAnthropic:
		return RefreshAnthropic(ctx, refreshToken)
	case ProviderCodex:
		return RefreshCodex(ctx, refreshToken)
	case ProviderXAI:
		return RefreshXAI(ctx, refreshToken)
	default:
		return Credential{}, nil
	}
}

// EnsureAccess refreshes cfg.APIKey when it is an OAuth token near expiry.
func EnsureAccess(ctx context.Context, cfg *llm.ModelConfig, storePath string) error {
	if cfg == nil || storePath == "" {
		return nil
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return Apply(ctx, cfg, storePath)
	}
	var provider string
	switch {
	case IsAnthropicOAuthToken(cfg.APIKey):
		provider = ProviderAnthropic
	case IsCodexOAuthToken(cfg.APIKey):
		provider = ProviderCodex
	default:
		return nil
	}
	st := OpenStore(storePath)
	cred, ok := st.Get(provider)
	if !ok {
		return nil
	}
	if !cred.Expired() {
		cfg.APIKey = cred.AccessToken
		return nil
	}
	if cred.RefreshToken == "" {
		return nil
	}
	fresh, err := refresh(ctx, provider, cred.RefreshToken)
	if err != nil {
		return err
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = cred.RefreshToken
	}
	if err := st.Put(fresh); err != nil {
		return err
	}
	cfg.APIKey = fresh.AccessToken
	return nil
}
