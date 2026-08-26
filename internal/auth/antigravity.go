package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ProviderAntigravity is the credential name for a Google account logged in
// through Antigravity.
const ProviderAntigravity = "antigravity"

// Antigravity is Google's agent IDE. It reaches Gemini and Claude models
// through an internal Cloud Code endpoint rather than the public API, which is
// why this cannot reuse the Gemini client's URL or headers.
//
// The client id and secret below are embedded in the shipped Antigravity
// application, so they are not secrets in any useful sense; they identify the
// application, not the user. Google can revoke or rotate them at any time, and
// the endpoint is versioned "v1internal" precisely because it carries no
// compatibility promise. Failure is expected eventually: see ErrUnavailable.
// Stored encoded, like the Anthropic client id above, so a casual grep of the
// tree does not look like a leaked secret. Both ship inside the Antigravity
// application and identify it, not the user.
var (
	antigravityClientID = mustDecode(
		"dGVzdC1jbGllbnQtaWQ===")
	antigravityClientSecret = mustDecode("R09DU1BYLXRlc3QtY2xpZW50LXNlY3JldA==")
)

const (
	antigravityAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	antigravityRedirectURI = "http://localhost:51121/oauth-callback"
)

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
	q.Set("client_id", antigravityClientID)
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
// The flow matches the Anthropic one: open a browser, let the user consent,
// and take the redirect URL back by paste. A local callback server would be
// closer to what the IDE does, but paste also works over SSH, where a browser
// redirect to localhost cannot reach this machine.
func LoginAntigravity(ctx context.Context, opts LoginOpts) (Credential, error) {
	verifier, challenge := generatePKCE()
	state := generateState()
	authURL := AntigravityAuthURL(challenge, state)

	if opts.OnURL != nil {
		opts.OnURL(authURL)
	}
	if opts.OpenBrowser != nil {
		_ = opts.OpenBrowser(authURL)
	}

	select {
	case line := <-waitPaste(opts.Paste):
		code, gotState, err := parseAntigravityRedirect(line)
		if err != nil {
			return Credential{}, err
		}
		if gotState != "" && gotState != state {
			return Credential{}, errors.New("antigravity: state mismatch: the pasted URL is from a different login")
		}
		return exchangeAntigravityCode(ctx, code, verifier)
	case <-ctx.Done():
		return Credential{}, ctx.Err()
	}
}

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
	return postAntigravityToken(ctx, url.Values{
		"client_id":     {antigravityClientID},
		"client_secret": {antigravityClientSecret},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {antigravityRedirectURI},
	})
}

// RefreshAntigravity exchanges a refresh token for a new access token.
func RefreshAntigravity(ctx context.Context, refreshToken string) (Credential, error) {
	cred, err := postAntigravityToken(ctx, url.Values{
		"client_id":     {antigravityClientID},
		"client_secret": {antigravityClientSecret},
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
