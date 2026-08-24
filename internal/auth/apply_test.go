package auth

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/llm"
)

func TestApplyFillsAnthropicKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	st := OpenStore(path)
	require.NoError(t, st.Put(Credential{Provider: ProviderAnthropic, AccessToken: "sk-ant-oat-x"}))

	cfg := llm.ModelConfig{Name: "claude-sonnet-4-5"}
	require.NoError(t, Apply(t.Context(), &cfg, path))
	require.Equal(t, "sk-ant-oat-x", cfg.APIKey)
	require.Equal(t, "https://api.anthropic.com", cfg.BaseURL)
}

func TestApplyLeavesExplicitKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	st := OpenStore(path)
	require.NoError(t, st.Put(Credential{Provider: ProviderAnthropic, AccessToken: "sk-ant-oat-x"}))

	cfg := llm.ModelConfig{Name: "claude-sonnet-4-5", APIKey: "sk-ant-api03-keep"}
	require.NoError(t, Apply(t.Context(), &cfg, path))
	require.Equal(t, "sk-ant-api03-keep", cfg.APIKey)
}
