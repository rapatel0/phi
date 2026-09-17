package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogKnownProviders(t *testing.T) {
	for _, p := range []string{ProviderAnthropic, ProviderCodex, ProviderXAI, ProviderGemini} {
		got := Catalog(p)
		require.NotEmpty(t, got, p)
		seen := map[string]struct{}{}
		for _, m := range got {
			require.NotEmpty(t, m.Name, p)
			require.NotEmpty(t, m.BaseURL, p)
			require.Greater(t, m.ContextWindow, 0, m.Name)
			_, dup := seen[m.Name]
			require.False(t, dup, m.Name)
			seen[m.Name] = struct{}{}
		}
	}
	codex := Catalog(ProviderCodex)
	for _, name := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		found := false
		for _, model := range codex {
			if model.Name == name {
				found = true
				break
			}
		}
		require.True(t, found, name)
	}
	require.Empty(t, Catalog("unknown"))
}
