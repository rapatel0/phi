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
	require.Empty(t, Catalog("unknown"))
}
