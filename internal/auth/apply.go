package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
)

// Apply fills cfg.APIKey from ~/.alpha/auth.json when the config has no key.
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
	if provider == ProviderAntigravity && cfg.BaseURL == "" {
		cfg.BaseURL = antigravityBase
	}
	return nil
}

// CodexBackendBaseURL is the ChatGPT Codex responses endpoint origin.
const CodexBackendBaseURL = "https://chatgpt.com/backend-api/codex"

// ProviderFor names the backend a model config points at, or "" when it is
// not recognized. Callers outside auth use it to apply provider-specific
// limits, so the rules live in one place rather than being restated.
func ProviderFor(cfg llm.ModelConfig) string { return providerFor(cfg) }

func providerFor(cfg llm.ModelConfig) string {
	base := strings.ToLower(cfg.BaseURL)
	name := strings.ToLower(cfg.Name)
	// Antigravity is checked first because it serves Claude and Gemini
	// models. A config naming one of those over the antigravity endpoint
	// would otherwise match the Anthropic or Gemini rule below and be sent
	// that provider's token.
	if strings.Contains(base, "cloudcode-pa.googleapis.com") || strings.HasPrefix(name, "antigravity-") {
		return ProviderAntigravity
	}
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
	fresh, err := refreshFor(ctx, provider, refreshToken)
	if err != nil {
		return Credential{}, err
	}
	// The caller stores what comes back, so an empty token here replaces a
	// working credential with a broken one and logs the user out. A provider
	// that reports success without a token is misbehaving; say so instead.
	if provider != "" && fresh.AccessToken == "" {
		return Credential{}, fmt.Errorf("auth: %s returned no access token", provider)
	}
	return fresh, nil
}

// refreshHook stands in for a provider during tests. It is the only way to
// reach the empty-token guard below, because every provider compiled in today
// makes that check itself. The guard exists for the next one that does not.
var refreshHook func(ctx context.Context, provider, refreshToken string) (Credential, error)

func refreshFor(ctx context.Context, provider, refreshToken string) (Credential, error) {
	if refreshHook != nil {
		return refreshHook(ctx, provider, refreshToken)
	}
	switch provider {
	case ProviderAnthropic:
		return RefreshAnthropic(ctx, refreshToken)
	case ProviderCodex:
		return RefreshCodex(ctx, refreshToken)
	case ProviderXAI:
		return RefreshXAI(ctx, refreshToken)
	case ProviderAntigravity:
		return RefreshAntigravity(ctx, refreshToken)
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
