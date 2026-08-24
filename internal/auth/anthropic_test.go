package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostAnthropicToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "refresh_token", body["grant_type"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "sk-ant-oat-new",
			"refresh_token": "r2",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(srv.Close)

	old := anthropicTokenURL
	anthropicTokenURL = srv.URL
	t.Cleanup(func() { anthropicTokenURL = old })

	cred, err := RefreshAnthropic(t.Context(), "r1")
	require.NoError(t, err)
	require.Equal(t, "sk-ant-oat-new", cred.AccessToken)
	require.Equal(t, "r2", cred.RefreshToken)
	require.Equal(t, ProviderAnthropic, cred.Provider)
}
