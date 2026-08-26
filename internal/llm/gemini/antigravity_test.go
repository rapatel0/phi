package gemini

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/util"
)

func agConfig(base string) llm.ModelConfig {
	return llm.ModelConfig{Name: "antigravity-gemini-3.1-pro", BaseURL: base, APIKey: "tok"}
}

// collect drains a stream into text and the first error.
func collect(t *testing.T, cfg llm.ModelConfig) (string, error) {
	t.Helper()
	var sb strings.Builder
	for ev, err := range StreamAntigravity(t.Context(), util.DefaultHTTPClient(), cfg, generateRequest{}) {
		if err != nil {
			return sb.String(), err
		}
		sb.WriteString(ev.Delta.Content)
	}
	return sb.String(), nil
}

// The provider must be routed to the Gemini client, or the transport below is
// never reached.
func TestAntigravityIsRoutedThroughGemini(t *testing.T) {
	for _, name := range []string{
		"antigravity-gemini-3.1-pro",
		"antigravity-claude-opus-4-6-thinking",
		"antigravity-gpt-oss-120b-medium",
		// A bare name carries no antigravity prefix, so only the base
		// URL can route it. Without that check a config naming one of
		// these reaches the OpenAI path and is sent the wrong body.
		"claude-opus-4-6-thinking",
		"gpt-oss-120b-medium",
	} {
		cfg := llm.ModelConfig{Name: name, BaseURL: AntigravityBaseURL}
		assert.True(t, IsProvider(cfg), "%s must reach the gemini client", name)
		assert.True(t, IsAntigravity(cfg), "%s must use the antigravity transport", name)
	}
}

// A public Gemini model must keep the ordinary transport.
func TestPlainGeminiIsNotAntigravity(t *testing.T) {
	cfg := llm.ModelConfig{Name: "gemini-2.5-pro", BaseURL: "https://generativelanguage.googleapis.com/v1beta"}
	assert.True(t, IsProvider(cfg))
	assert.False(t, IsAntigravity(cfg))
}

// The endpoint checks the envelope and the agent string. Sending a bare Gemini
// body, or a generic agent, is refused.
func TestAntigravitySendsTheEnvelopeAndAgent(t *testing.T) {
	var gotPath, gotAgent, gotAuth string
	var envelope antigravityEnvelope

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotAgent = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &envelope)
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}]}\n\n")
	}))
	defer srv.Close()

	text, err := collect(t, agConfig(srv.URL))
	require.NoError(t, err)
	assert.Equal(t, "hi", text)

	assert.Equal(t, "/v1internal:streamGenerateContent?alt=sse", gotPath)
	assert.Equal(t, "Bearer tok", gotAuth)
	assert.Contains(t, gotAgent, "antigravity/cli/", "the endpoint refuses a generic agent")
	assert.Contains(t, gotAgent, "aidev_client")

	assert.Equal(t, "agent", envelope.RequestType)
	assert.Equal(t, "antigravity", envelope.UserAgent)
	assert.NotEmpty(t, envelope.Project)
}

// The catalog prefixes names so they cannot collide with public Gemini models
// in the same palette; the endpoint wants the bare name.
func TestAntigravityStripsTheNamePrefix(t *testing.T) {
	var envelope antigravityEnvelope
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &envelope)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	_, err := collect(t, agConfig(srv.URL))
	require.NoError(t, err)
	assert.Equal(t, "gemini-3.1-pro", envelope.Model)
}

func TestAntigravityModelMapping(t *testing.T) {
	assert.Equal(t, "gemini-3.1-pro", antigravityModel("antigravity-gemini-3.1-pro"))
	assert.Equal(t, "claude-opus-4-6-thinking", antigravityModel("antigravity-claude-opus-4-6-thinking"))
	assert.Equal(t, "gemini-2.5-pro", antigravityModel("gemini-2.5-pro"), "an unprefixed name passes through")
}

// This is the failure this provider is expected to reach: Google withdraws the
// endpoint or stops accepting the borrowed client. It must be reported as its
// own condition, because telling the user to log in again would not help.
func TestAntigravityReportsWithdrawal(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":{"message":"nope"}}`)
		}))

		_, err := collect(t, agConfig(srv.URL))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAntigravityUnavailable, "HTTP %d must report withdrawal", status)
		srv.Close()
	}
}

// An ordinary server error is not a withdrawal: retrying later can work, so it
// must not be reported as the provider being gone.
func TestAntigravityKeepsOrdinaryErrorsDistinct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	_, err := collect(t, agConfig(srv.URL))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrAntigravityUnavailable)
	assert.Contains(t, err.Error(), "antigravity API error")
}

// The IDE tries the daily endpoint, then production. A configured base must
// not fall through to Google: a test server, or a deliberately set base, is
// the only endpoint the caller asked for.
func TestAntigravityEndpointOrder(t *testing.T) {
	assert.Equal(t,
		[]string{AntigravityBaseURL, antigravityFallbackURL},
		antigravityEndpoints(""),
		"an unset base uses the IDE order")

	assert.Equal(t,
		[]string{AntigravityBaseURL, antigravityFallbackURL},
		antigravityEndpoints(AntigravityBaseURL),
		"the daily endpoint falls back to production")

	assert.Equal(t,
		[]string{"http://127.0.0.1:9"},
		antigravityEndpoints("http://127.0.0.1:9"),
		"a configured base must not reach Google")
}

// A withdrawal must stop the walk: the second endpoint refuses an unrecognized
// client the same way, so trying it only doubles the latency.
func TestAntigravityDoesNotRetryAWithdrawal(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := collect(t, agConfig(srv.URL))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAntigravityUnavailable)
	assert.Equal(t, 1, hits)
}

// The agent string carries the platform the IDE reports.
func TestAntigravityUserAgentShape(t *testing.T) {
	ua := antigravityUserAgent()
	assert.True(t, strings.HasPrefix(ua, "antigravity/cli/"), "got %q", ua)
	assert.Contains(t, ua, "os_type=")
	assert.Contains(t, ua, "arch=")
	assert.Contains(t, ua, "auth_method=consumer")
}

func TestAntigravityURL(t *testing.T) {
	assert.Equal(t,
		"https://example.com/v1internal:streamGenerateContent?alt=sse",
		antigravityURL("https://example.com/"))
}

// A transport error must not be swallowed into a nil error and an empty stream.
func TestAntigravityReportsAnUnreachableEndpoint(t *testing.T) {
	cfg := agConfig("http://127.0.0.1:1")
	_, err := collect(t, cfg)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrAntigravityUnavailable), "a dead socket is not a withdrawal")
}
