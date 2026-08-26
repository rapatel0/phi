package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"runtime"
	"strings"

	"github.com/rapatel0/alpha/internal/debuglog"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/util"
)

// Antigravity is Google's agent IDE. It reaches Gemini and Claude models
// through an internal Cloud Code endpoint, which speaks the same
// generateContent body as the public API but wraps it in an envelope and
// requires the IDE's own headers.
//
// The wire format is the only reason this is not just a base URL: everything
// below the envelope is the ordinary Gemini request this package already
// builds.
const (
	// AntigravityBaseURL identifies the provider in a model config. The
	// daily endpoint is what the IDE itself uses.
	AntigravityBaseURL = "https://daily-cloudcode-pa.googleapis.com"

	// antigravityFallbackURL is the production endpoint, tried when the
	// daily one refuses the request.
	antigravityFallbackURL = "https://cloudcode-pa.googleapis.com"

	// antigravityProject is the project id the IDE falls back to when the
	// account has none of its own.
	antigravityProject = "rising-fact-p41fc"

	// antigravityVersion is the IDE build this client reports as.
	antigravityVersion = "1.18.3"
)

// ErrAntigravityUnavailable reports that the endpoint stopped accepting these
// requests.
//
// This provider talks to an undocumented internal endpoint using another
// application's identity. Google can withdraw it at any time, and the failure
// then arrives as a plain 401 or 403 that is indistinguishable from a bad
// login. Naming the condition is what lets the caller tell the user the
// provider is gone rather than send them to log in again.
var ErrAntigravityUnavailable = errors.New("antigravity is no longer accepted by Google")

// IsAntigravity reports whether a model config targets Antigravity.
func IsAntigravity(cfg llm.ModelConfig) bool {
	base := strings.ToLower(cfg.BaseURL)
	return strings.Contains(base, "cloudcode-pa.googleapis.com")
}

// antigravityEnvelope wraps a Gemini request for the internal endpoint.
type antigravityEnvelope struct {
	Project     string          `json:"project"`
	Request     generateRequest `json:"request"`
	Model       string          `json:"model"`
	UserAgent   string          `json:"userAgent"`
	RequestType string          `json:"requestType"`
}

// antigravityUserAgent identifies this client to the endpoint.
//
// The endpoint checks it: a generic agent is refused. os_type and arch mirror
// what the IDE reports.
func antigravityUserAgent() string {
	osType := "LINUX"
	switch runtime.GOOS {
	case "darwin":
		osType = "DARWIN"
	case "windows":
		osType = "WINDOWS"
	}
	arch := runtime.GOARCH
	if arch == "arm64" {
		arch = "aarch64"
	}
	return fmt.Sprintf("antigravity/cli/%s (aidev_client; os_type=%s; arch=%s; auth_method=consumer)",
		antigravityVersion, osType, arch)
}

// antigravityURL builds the stream endpoint for a base.
func antigravityURL(base string) string {
	return strings.TrimRight(base, "/") + "/v1internal:streamGenerateContent?alt=sse"
}

// StreamAntigravity POSTs a request to the Antigravity endpoint and yields
// Alpha stream events.
//
// The daily endpoint is tried first, matching the IDE, and production is used
// when it refuses. Only one retry is made: a second refusal means the account
// or the client is rejected, not that the endpoint was busy.
func StreamAntigravity(
	ctx context.Context,
	httpClient *http.Client,
	cfg llm.ModelConfig,
	req generateRequest,
) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		body, err := json.Marshal(antigravityEnvelope{
			Project:     antigravityProject,
			Request:     req,
			Model:       antigravityModel(cfg.Name),
			UserAgent:   "antigravity",
			RequestType: "agent",
		})
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}

		stream, lastErr := openAntigravityStream(ctx, httpClient, cfg, body)
		if stream != nil {
			defer stream.Close()
			processStream(stream, yield)
			return
		}
		yield(llm.StreamEvent{}, lastErr)
	}
}

// openAntigravityStream returns the body of the first endpoint that accepts
// the request, or the error from the last one that did not.
//
// The daily endpoint is tried first, matching the IDE, and production is used
// when it refuses. A rejected identity stops the walk: a second endpoint will
// refuse it the same way.
func openAntigravityStream(
	ctx context.Context,
	httpClient *http.Client,
	cfg llm.ModelConfig,
	body []byte,
) (io.ReadCloser, error) {
	var lastErr error
	for _, base := range antigravityEndpoints(cfg.BaseURL) {
		resp, err := postAntigravity(ctx, httpClient, base, cfg.APIKey, body)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return resp.Body, nil
		}
		raw, _ := io.ReadAll(resp.Body)
		if cerr := resp.Body.Close(); cerr != nil {
			debuglog.Logf("antigravity: close body: %v", cerr)
		}
		lastErr = antigravityError(resp.StatusCode, raw)
		if errors.Is(lastErr, ErrAntigravityUnavailable) {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("antigravity: no endpoint answered")
	}
	return nil, lastErr
}

// antigravityEndpoints lists the endpoints to try, in order.
//
// The production endpoint is only appended when the configured base is the
// real daily one. A test server, or a base someone set deliberately, must not
// silently fall through to Google.
func antigravityEndpoints(base string) []string {
	if strings.TrimSpace(base) == "" {
		return []string{AntigravityBaseURL, antigravityFallbackURL}
	}
	if strings.EqualFold(strings.TrimRight(base, "/"), AntigravityBaseURL) {
		return []string{base, antigravityFallbackURL}
	}
	return []string{base}
}

// postAntigravity sends one request to one endpoint.
func postAntigravity(
	ctx context.Context,
	httpClient *http.Client,
	base, token string,
	body []byte,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityURL(base), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUserAgent())
	return util.DoWithRetry(httpClient, req)
}

// antigravityError turns a failed response into an error, separating "this
// provider is gone" from an ordinary failure.
//
// 401 and 403 are the shapes the withdrawal is expected to take: the token is
// refused, or the client is no longer recognized. Both are reported as
// ErrAntigravityUnavailable so the caller can say so plainly, because telling
// the user to log in again would not help.
func antigravityError(status int, raw []byte) error {
	detail := strings.TrimSpace(string(raw))
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w (HTTP %d): %s", ErrAntigravityUnavailable, status, detail)
	case http.StatusNotFound:
		// The internal endpoint was removed or renamed.
		return fmt.Errorf("%w (HTTP 404: endpoint gone): %s", ErrAntigravityUnavailable, detail)
	default:
		return fmt.Errorf("antigravity API error: (%d) %s", status, detail)
	}
}

// antigravityModel maps a catalog name to the wire model name.
//
// The catalog prefixes names with "antigravity-" so they cannot collide with
// the public Gemini models in the same palette; the endpoint wants the bare
// name.
func antigravityModel(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "antigravity-")
}
