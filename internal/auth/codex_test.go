package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// A rate limit must not end a login the user is about to approve. Aborting
// throws away a code that is still valid and makes the user start again.
func TestPollCodexApprovalSurvivesARateLimit(t *testing.T) {
	oldStep := codexSlowDownStep
	codexSlowDownStep = 20 * time.Millisecond
	t.Cleanup(func() { codexSlowDownStep = oldStep })

	var calls int
	var gaps []time.Duration
	last := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		gaps = append(gaps, time.Since(last))
		last = time.Now()
		switch calls {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusForbidden) // not approved yet
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"authorization_code":"ac","code_verifier":"cv"}`)
		}
	}))
	t.Cleanup(srv.Close)

	old := codexDeviceURL
	codexDeviceURL = srv.URL
	t.Cleanup(func() { codexDeviceURL = old })

	sess := &CodexDevice{deviceAuthID: "id", UserCode: "uc", interval: time.Millisecond}
	approval, err := pollCodexApproval(t.Context(), sess)
	require.NoError(t, err, "a rate limit must not end the login")
	require.Equal(t, "ac", approval.AuthorizationCode)
	require.Len(t, gaps, 3)
	require.Greater(t, gaps[1], 15*time.Millisecond, "the rate limit must widen the interval")
}

// An unexpected status is a real failure and must still stop the login rather
// than polling until the deadline.
func TestPollCodexApprovalStopsOnAServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))
	t.Cleanup(srv.Close)

	old := codexDeviceURL
	codexDeviceURL = srv.URL
	t.Cleanup(func() { codexDeviceURL = old })

	sess := &CodexDevice{deviceAuthID: "id", UserCode: "uc", interval: time.Millisecond}
	_, err := pollCodexApproval(t.Context(), sess)
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}

// The user may approve before the first interval elapses, and waiting first
// adds that delay to every login.
func TestPollCodexApprovalPollsBeforeWaiting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"authorization_code":"ac","code_verifier":"cv"}`)
	}))
	t.Cleanup(srv.Close)

	old := codexDeviceURL
	codexDeviceURL = srv.URL
	t.Cleanup(func() { codexDeviceURL = old })

	sess := &CodexDevice{deviceAuthID: "id", UserCode: "uc", interval: 30 * time.Second}
	start := time.Now()
	_, err := pollCodexApproval(t.Context(), sess)
	require.NoError(t, err)
	require.Less(t, time.Since(start), 5*time.Second, "the first poll must not wait for the interval")
}
