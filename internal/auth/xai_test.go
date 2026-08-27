package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	}))
	t.Cleanup(srv.Close)
	old := xaiTokenURL
	xaiTokenURL = srv.URL
	t.Cleanup(func() { xaiTokenURL = old })

	_, pending, err := pollXAIToken(t.Context(), "dev", "")
	require.NoError(t, err)
	require.Equal(t, pendingApproval, pending)
}

// stubXAIToken points the token endpoint at a local server and counts the
// requests it receives.
func stubXAIToken(t *testing.T, handler func(w http.ResponseWriter, n int)) {
	t.Helper()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		handler(w, calls)
	}))
	t.Cleanup(srv.Close)

	old := xaiTokenURL
	xaiTokenURL = srv.URL
	t.Cleanup(func() { xaiTokenURL = old })
}

func writeXAIError(w http.ResponseWriter, code string) {
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// A refresh has nobody to approve it, so a pending reply is a failure rather
// than something to wait for.
//
// Reporting success here returns an empty credential with a nil error, and the
// caller in apply.go stores that over the working token: the user is logged
// out by a refresh that was only rate limited.
func TestRefreshXAIRejectsAPendingReply(t *testing.T) {
	for _, code := range []string{"slow_down", "authorization_pending"} {
		t.Run(code, func(t *testing.T) {
			stubXAIToken(t, func(w http.ResponseWriter, _ int) { writeXAIError(w, code) })

			cred, err := RefreshXAI(t.Context(), "r1")
			require.Error(t, err, "a pending reply must not read as success")
			require.Empty(t, cred.AccessToken)
		})
	}
}

// The stored token must survive a refresh that fails, or a rate limit logs the
// user out.
func TestRefreshFailureLeavesTheStoredTokenAlone(t *testing.T) {
	stubXAIToken(t, func(w http.ResponseWriter, _ int) { writeXAIError(w, "slow_down") })

	path := filepath.Join(t.TempDir(), "auth.json")
	st := OpenStore(path)
	require.NoError(t, st.Put(Credential{
		Provider:     ProviderXAI,
		AccessToken:  "still-good",
		RefreshToken: "r1",
		ExpiresAt:    time.Now().Add(-time.Hour), // expired, so a refresh is attempted
	}))

	cfg := &llm.ModelConfig{Name: "grok-4"}
	err := Apply(t.Context(), cfg, path)
	require.Error(t, err, "the failure must reach the caller")

	got, ok := OpenStore(path).Get(ProviderXAI)
	require.True(t, ok, "the credential must not be deleted")
	require.Equal(t, "still-good", got.AccessToken, "a failed refresh must not overwrite the token")
	require.Equal(t, "r1", got.RefreshToken)
}

// Waiting a full interval before the first poll adds that delay to every
// login, including one the user has already approved.
func TestCompleteXAIDevicePollsBeforeWaiting(t *testing.T) {
	stubXAIToken(t, func(w http.ResponseWriter, _ int) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "expires_in": 3600})
	})

	sess := &XAIDevice{
		deviceCode: "dev",
		interval:   30 * time.Second, // long enough that waiting first would fail the test
		expires:    time.Now().Add(time.Minute),
	}

	start := time.Now()
	cred, err := CompleteXAIDevice(t.Context(), sess)
	require.NoError(t, err)
	require.Equal(t, "at", cred.AccessToken)
	require.Less(t, time.Since(start), 5*time.Second, "the first poll must not wait for the interval")
}

// RFC 8628 section 3.5: slow_down means the interval must grow, or the server
// keeps refusing and the login never completes.
func TestCompleteXAIDeviceBacksOffOnSlowDown(t *testing.T) {
	var gaps []time.Duration
	last := time.Now()
	stubXAIToken(t, func(w http.ResponseWriter, n int) {
		gaps = append(gaps, time.Since(last))
		last = time.Now()
		if n < 3 {
			writeXAIError(w, "slow_down")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at"})
	})

	// Shortened so the test does not sleep for the real five seconds a
	// slow_down adds.
	oldStep := xaiSlowDownStep
	xaiSlowDownStep = 40 * time.Millisecond
	t.Cleanup(func() { xaiSlowDownStep = oldStep })

	sess := &XAIDevice{
		deviceCode: "dev",
		interval:   time.Millisecond,
		expires:    time.Now().Add(time.Minute),
	}

	cred, err := CompleteXAIDevice(t.Context(), sess)
	require.NoError(t, err)
	require.Equal(t, "at", cred.AccessToken)
	require.Len(t, gaps, 3)

	// Each slow_down widens the interval, so every wait is longer than the
	// one before it.
	require.Greater(t, gaps[1], 30*time.Millisecond, "slow_down must widen the interval")
	require.Greater(t, gaps[2], gaps[1], "a second slow_down must widen it again")
}

// An expired code must be reported rather than polled forever.
func TestCompleteXAIDeviceStopsWhenTheCodeExpires(t *testing.T) {
	stubXAIToken(t, func(w http.ResponseWriter, _ int) { writeXAIError(w, "authorization_pending") })

	sess := &XAIDevice{
		deviceCode: "dev",
		interval:   10 * time.Millisecond,
		expires:    time.Now().Add(50 * time.Millisecond),
	}

	_, err := CompleteXAIDevice(t.Context(), sess)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired")
}
