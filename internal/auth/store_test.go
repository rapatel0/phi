package auth

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStorePutGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	st := OpenStore(path)
	cred := Credential{
		Provider:     ProviderAnthropic,
		AccessToken:  "sk-ant-oat-test",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	require.NoError(t, st.Put(cred))
	got, ok := st.Get(ProviderAnthropic)
	require.True(t, ok)
	require.Equal(t, cred.AccessToken, got.AccessToken)
	require.Equal(t, cred.RefreshToken, got.RefreshToken)

	require.NoError(t, st.Put(Credential{Provider: ProviderCodex, AccessToken: "oat"}))
	require.Equal(t, []string{ProviderAnthropic, ProviderCodex}, st.Providers())
}

func TestCredentialExpiredSkew(t *testing.T) {
	c := Credential{AccessToken: "x", ExpiresAt: time.Now().Add(2 * time.Minute)}
	require.True(t, c.Expired(), "within 5m skew should refresh")
	c.ExpiresAt = time.Now().Add(10 * time.Minute)
	require.False(t, c.Expired())
	c.ExpiresAt = time.Time{}
	require.False(t, c.Expired())
}
