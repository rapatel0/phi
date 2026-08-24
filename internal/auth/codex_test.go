package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartCodexDevice(t *testing.T) {
	var usercodeHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/usercode"):
			usercodeHits++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_auth_id": "dev1",
				"user_code":      "ABCD-EFGH",
				"interval":       "1",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := codexDeviceURL
	codexDeviceURL = srv.URL
	t.Cleanup(func() { codexDeviceURL = old })

	sess, err := StartCodexDevice(t.Context())
	require.NoError(t, err)
	require.Equal(t, "ABCD-EFGH", sess.UserCode)
	require.Equal(t, 1, usercodeHits)
}

func TestRefreshCodex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "hdr.payload.sig",
			"refresh_token": "r2",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(srv.Close)
	old := codexTokenURL
	codexTokenURL = srv.URL
	t.Cleanup(func() { codexTokenURL = old })

	cred, err := RefreshCodex(t.Context(), "r1")
	require.NoError(t, err)
	require.Equal(t, "hdr.payload.sig", cred.AccessToken)
	require.Equal(t, ProviderCodex, cred.Provider)
}
