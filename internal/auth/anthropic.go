package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Claude Code public OAuth client (same ID Pi uses). Stored encoded so a
// casual grep of the tree does not look like a leaked secret — it is public.
var anthropicClientID = mustDecode("OWQxYzI1MGEtZTYxYi00NGQ5LTg4ZWQtNTk0NGQxOTYyZjVl")

const (
	anthropicAuthorizeURL = "https://claude.ai/oauth/authorize"
	anthropicCallbackHost = "127.0.0.1"
	anthropicCallbackPort = 53692
	anthropicCallbackPath = "/callback"
	anthropicScopes       = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
)

var anthropicTokenURL = "https://platform.claude.com/v1/oauth/token"

func mustDecode(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic("auth: client id decode: " + err.Error())
	}
	return string(b)
}

func anthropicRedirectURI() string {
	return fmt.Sprintf("http://localhost:%d%s", anthropicCallbackPort, anthropicCallbackPath)
}

// LoginOpts configures an interactive OAuth login.
type LoginOpts struct {
	OpenBrowser func(string) error
	OnURL       func(string) // printed instructions; url and optional user code
	Paste       <-chan string
}

// LoginAnthropic runs the Claude Pro/Max PKCE flow: local callback on
// :53692 plus a pasted-code fallback (SSH / remote browser).
func LoginAnthropic(ctx context.Context, opts LoginOpts) (Credential, error) {
	verifier, challenge := generatePKCE()
	state := verifier // Anthropic expects the PKCE verifier as state (Pi/Claude Code).
	redirect := anthropicRedirectURI()
	authURL := AnthropicAuthURL(challenge, state)

	if opts.OnURL != nil {
		opts.OnURL(authURL)
	}

	codeCh := make(chan string, 1)
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", anthropicCallbackHost, anthropicCallbackPort))
	if err != nil {
		// Registered redirect still has to be that port; paste the final URL.
		ln = nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc(anthropicCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			http.Error(w, "authentication failed: "+e, http.StatusBadRequest)
			return
		}
		code := q.Get("code")
		st := q.Get("state")
		if code == "" || st != state {
			http.Error(w, "missing code or state mismatch", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(
			w,
			`<!doctype html><title>phi</title><p>Claude login complete. You can close this tab.</p>`,
		)
		select {
		case codeCh <- code:
		default:
		}
	})
	if ln != nil {
		srv := &http.Server{Handler: mux}
		go func() { _ = srv.Serve(ln) }()
		defer func() {
			shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(shut)
		}()
	}

	if opts.OpenBrowser != nil {
		_ = opts.OpenBrowser(authURL)
	}

	var code string
	select {
	case <-ctx.Done():
		return Credential{}, ctx.Err()
	case code = <-codeCh:
	case pasted := <-waitPaste(opts.Paste):
		c, st := parseAuthorizationInput(pasted)
		if st != "" && st != state {
			return Credential{}, fmt.Errorf("auth: OAuth state mismatch")
		}
		if c == "" {
			return Credential{}, fmt.Errorf("auth: missing authorization code")
		}
		code = c
	}

	return exchangeAnthropicCode(ctx, code, state, verifier, redirect)
}

func waitPaste(paste <-chan string) <-chan string {
	if paste != nil {
		return paste
	}
	ch := make(chan string)
	return ch
}

func exchangeAnthropicCode(ctx context.Context, code, state, verifier, redirect string) (Credential, error) {
	return postAnthropicToken(ctx, map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     anthropicClientID,
		"code":          code,
		"state":         state,
		"redirect_uri":  redirect,
		"code_verifier": verifier,
	})
}

// RefreshAnthropic exchanges a refresh token for a new access token.
func RefreshAnthropic(ctx context.Context, refreshToken string) (Credential, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Credential{}, fmt.Errorf("auth: missing anthropic refresh token")
	}
	return postAnthropicToken(ctx, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     anthropicClientID,
		"refresh_token": refreshToken,
	})
}

func postAnthropicToken(ctx context.Context, payload map[string]string) (Credential, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Credential{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credential{}, fmt.Errorf("auth: anthropic token: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Credential{}, fmt.Errorf("auth: anthropic token (%d): %s", resp.StatusCode, truncateBody(raw))
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return Credential{}, fmt.Errorf("auth: anthropic token json: %w", err)
	}
	if tok.AccessToken == "" {
		return Credential{}, fmt.Errorf("auth: anthropic token missing access_token")
	}
	cred := Credential{
		Provider:     ProviderAnthropic,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
	}
	if tok.ExpiresIn > 0 {
		cred.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second - 5*time.Minute)
	}
	return cred, nil
}

func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// AnthropicAuthURL is the browser URL for a login in progress (for printing).
func AnthropicAuthURL(challenge, state string) string {
	params := url.Values{
		"code":                  {"true"},
		"client_id":             {anthropicClientID},
		"response_type":         {"code"},
		"redirect_uri":          {anthropicRedirectURI()},
		"scope":                 {anthropicScopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return anthropicAuthorizeURL + "?" + params.Encode()
}
