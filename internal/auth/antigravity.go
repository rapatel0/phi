package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ProviderAntigravity is the credential name for a Google account logged in
// through Antigravity.
const ProviderAntigravity = "antigravity"

// Antigravity is Google's agent IDE. It reaches Gemini and Claude models
// through an internal Cloud Code endpoint rather than the public API, which is
// why this cannot reuse the Gemini client's URL or headers.
const (
	antigravityClientIDEnv     = "ALPHA_ANTIGRAVITY_CLIENT_ID"
	antigravityClientSecretEnv = "ALPHA_ANTIGRAVITY_CLIENT_SECRET"
)

// antigravityClientCredentials reads OAuth credentials supplied by the user.
// They are not stored in source because Google revokes exposed client secrets.
func antigravityClientCredentials() (string, string, error) {
	clientID := strings.TrimSpace(os.Getenv(antigravityClientIDEnv))
	clientSecret := strings.TrimSpace(os.Getenv(antigravityClientSecretEnv))
	if clientID == "" || clientSecret == "" {
		return "", "", fmt.Errorf("antigravity: set %s and %s", antigravityClientIDEnv, antigravityClientSecretEnv)
	}
	return clientID, clientSecret, nil
}

const (
	antigravityAuthURL = "https://accounts.google.com/o/oauth2/v2/auth"

	// Registered with the OAuth client, so the path is fixed: Google will
	// redirect here and nowhere else.
	antigravityCallbackPath = "/oauth-callback"
	antigravityRedirectURI  = "http://localhost:51121/oauth-callback"
)

// A var, like antigravityTokenURL, so a test can bind a free port instead of
// the registered one and avoid colliding with a real login.
var antigravityCallbackPort = 51121

// A var, like anthropicTokenURL, so a test can point it at a local server
// instead of Google.
var antigravityTokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // G101: a public endpoint, not a credential

// antigravityScopes are what the IDE requests. Asking for fewer gets a token
// the endpoint rejects.
var antigravityScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// ErrUnavailable reports that Antigravity stopped accepting these requests.
//
// This provider talks to an undocumented internal endpoint with credentials
// copied from another application. Google can change either at any time, and
// the failure then looks like an ordinary authentication error. Recognizing it
// as a distinct condition is what lets the caller say "this provider stopped
// working" rather than "your login is wrong". Recognizing it early also avoids
// a pointless re-login.
var ErrUnavailable = errors.New("antigravity: no longer accepted by Google")

// AntigravityAuthURL builds the consent URL for the login flow.
func AntigravityAuthURL(challenge, state string) string {
	q := url.Values{}
	q.Set("client_id", os.Getenv(antigravityClientIDEnv))
	q.Set("redirect_uri", antigravityRedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(antigravityScopes, " "))
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	// A refresh token is only issued with consent forced and offline access
	// asked for; without it the login lasts an hour.
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	return antigravityAuthURL + "?" + q.Encode()
}

// LoginAntigravity runs the Google OAuth flow and returns a credential.
//
// Like the Anthropic flow, a local server on the registered callback port and
// a pasted URL race each other. The server finishes the login by itself when
// the browser runs on this machine. Paste covers the rest: over SSH the
// browser is elsewhere and its redirect to localhost cannot reach this
// process, and the port may already be taken by another alpha.
func LoginAntigravity(ctx context.Context, opts LoginOpts) (Credential, error) {
	if _, _, err := antigravityClientCredentials(); err != nil {
		return Credential{}, err
	}

	verifier, challenge := generatePKCE()
	state := generateState()
	authURL := AntigravityAuthURL(challenge, state)

	srv := listenCallback(ctx, antigravityCallbackPort, antigravityCallbackPath, state, antigravitySuccessHTML)
	defer srv.close()

	if opts.OnURL != nil {
		opts.OnURL(authURL)
	}
	if opts.OpenBrowser != nil {
		_ = opts.OpenBrowser(authURL)
	}

	var code string
	select {
	case got := <-srv.wait():
		if got.err != nil {
			return Credential{}, fmt.Errorf("antigravity: %w", got.err)
		}
		if got.state != state {
			return Credential{}, errors.New("antigravity: state mismatch: this redirect is from a different login")
		}
		code = got.code
	case line := <-waitPaste(opts.Paste):
		pasted, gotState, err := parseAntigravityRedirect(line)
		if err != nil {
			return Credential{}, err
		}
		// A bare code carries no state, so it can only be checked when the
		// whole URL was pasted.
		if gotState != "" && gotState != state {
			return Credential{}, errors.New("antigravity: state mismatch: the pasted URL is from a different login")
		}
		code = pasted
	case <-ctx.Done():
		return Credential{}, ctx.Err()
	}

	return exchangeAntigravityCode(ctx, code, verifier)
}

// The browser lands here after consent, so it has to say the login worked.
// Without it the user sees a connection error and assumes it failed.
const antigravitySuccessHTML = `<!doctype html><title>alpha</title>` +
	`<p>Antigravity login complete. You can close this tab.</p>`

// parseAntigravityRedirect accepts either the whole redirect URL or a bare
// authorization code, because a user who copies from the address bar and one
// who copies from the page both expect it to work.
func parseAntigravityRedirect(line string) (code, state string, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", errors.New("antigravity: no code pasted")
	}
	if !strings.Contains(line, "://") && !strings.Contains(line, "?") {
		return line, "", nil
	}
	u, parseErr := url.Parse(line)
	if parseErr != nil {
		return "", "", fmt.Errorf("antigravity: cannot read the pasted URL: %w", parseErr)
	}
	q := u.Query()
	if e := q.Get("error"); e != "" {
		return "", "", fmt.Errorf("antigravity: Google refused the login: %s", e)
	}
	code = q.Get("code")
	if code == "" {
		return "", "", errors.New("antigravity: the pasted URL has no code parameter")
	}
	return code, q.Get("state"), nil
}

// exchangeAntigravityCode trades an authorization code for tokens.
func exchangeAntigravityCode(ctx context.Context, code, verifier string) (Credential, error) {
	clientID, clientSecret, err := antigravityClientCredentials()
	if err != nil {
		return Credential{}, err
	}
	return postAntigravityToken(ctx, url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {antigravityRedirectURI},
	})
}

// RefreshAntigravity exchanges a refresh token for a new access token.
func RefreshAntigravity(ctx context.Context, refreshToken string) (Credential, error) {
	clientID, clientSecret, err := antigravityClientCredentials()
	if err != nil {
		return Credential{}, err
	}
	cred, err := postAntigravityToken(ctx, url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return Credential{}, err
	}
	// A refresh response usually omits the refresh token; dropping it here
	// would log the user out at the next expiry.
	if cred.RefreshToken == "" {
		cred.RefreshToken = refreshToken
	}
	return cred, nil
}

// tokenResponse is the subset of Google's token reply that matters.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// postAntigravityToken posts to Google's token endpoint.
func postAntigravityToken(ctx context.Context, form url.Values) (Credential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityTokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credential{}, fmt.Errorf("antigravity: token request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var tr tokenResponse
	_ = json.Unmarshal(raw, &tr)

	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		// invalid_client means Google retired or revoked the embedded
		// application credentials, which is the failure mode this
		// provider is expected to reach.
		if tr.Error == "invalid_client" || tr.Error == "unauthorized_client" {
			return Credential{}, fmt.Errorf("%w: %s", ErrUnavailable, tr.Error)
		}
		detail := tr.ErrorDesc
		if detail == "" {
			detail = truncateBody(raw)
		}
		return Credential{}, fmt.Errorf("antigravity: token exchange failed (%d): %s", resp.StatusCode, detail)
	}

	cred := Credential{
		Provider:     ProviderAntigravity,
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
	}
	if tr.ExpiresIn > 0 {
		cred.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return cred, nil
}
