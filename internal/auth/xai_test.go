package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefreshXAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "xai-access",
			"refresh_token": "r2",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(srv.Close)
	old := xaiTokenURL
	xaiTokenURL = srv.URL
	t.Cleanup(func() { xaiTokenURL = old })

	cred, err := RefreshXAI(t.Context(), "r1")
	require.NoError(t, err)
	require.Equal(t, "xai-access", cred.AccessToken)
	require.Equal(t, ProviderXAI, cred.Provider)
}

func TestPollXAIPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	}))
	t.Cleanup(srv.Close)
	old := xaiTokenURL
	xaiTokenURL = srv.URL
	t.Cleanup(func() { xaiTokenURL = old })

	_, pending, err := pollXAIToken(t.Context(), "dev", "")
	require.NoError(t, err)
	require.True(t, pending)
}
