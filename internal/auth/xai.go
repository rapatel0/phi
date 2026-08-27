package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SuperGrok / X Premium device OAuth (same client Pi and Oh My Pi use).
const (
	ProviderXAI     = "xai"
	xaiClientID     = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiScope        = "openid profile email offline_access grok-cli:access api:access"
	xaiDeviceURL    = "https://auth.x.ai/oauth2/device/code"
	xaiTokenDefault = "https://auth.x.ai/oauth2/token"
)

var xaiTokenURL = xaiTokenDefault

// xaiSlowDownStep is what RFC 8628 section 3.5 adds to the interval on each
// slow_down. A var so a test can shorten it instead of sleeping.
var xaiSlowDownStep = 5 * time.Second

// pollState says whether a poll produced a credential, and if not, why.
//
// A bool cannot carry this: slow_down and authorization_pending both mean
// "keep waiting", but only slow_down also means "wait longer".
type pollState int

const (
	pendingNone     pollState = iota // the token arrived
	pendingApproval                  // the user has not approved yet
	pendingSlowDown                  // polling too fast; widen the interval
)

// XAIDevice is an in-progress SuperGrok device login.
type XAIDevice struct {
	VerificationURL string
	UserCode        string
	deviceCode      string
	interval        time.Duration
	expires         time.Time
}

// StartXAIDevice requests a SuperGrok user code (RFC 8628).
func StartXAIDevice(ctx context.Context) (*XAIDevice, error) {
	data := url.Values{
		"client_id": {xaiClientID},
		"scope":     {xaiScope},
		"referrer":  {"alpha"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, xaiDeviceURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: xAI device code: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth: xAI device code (%d): %s", resp.StatusCode, truncateBody(raw))
	}
	var body struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		Interval                int    `json:"interval"`
		ExpiresIn               int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("auth: xAI device json: %w", err)
	}
	if body.DeviceCode == "" || body.UserCode == "" || body.VerificationURI == "" {
		return nil, fmt.Errorf("auth: xAI device response missing fields")
	}
	verify := body.VerificationURIComplete
	if verify == "" {
		verify = body.VerificationURI
	}
	if !strings.HasPrefix(verify, "https://") {
		return nil, fmt.Errorf("auth: untrusted xAI verification URI")
	}
	interval := time.Duration(body.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	exp := 15 * time.Minute
	if body.ExpiresIn > 0 {
		exp = time.Duration(body.ExpiresIn) * time.Second
	}
	return &XAIDevice{
		VerificationURL: verify,
		UserCode:        body.UserCode,
		deviceCode:      body.DeviceCode,
		interval:        interval,
		expires:         time.Now().Add(exp),
	}, nil
}

// CompleteXAIDevice polls until the user approves the SuperGrok code.
func CompleteXAIDevice(ctx context.Context, sess *XAIDevice) (Credential, error) {
	if sess == nil {
		return Credential{}, fmt.Errorf("auth: nil xAI device session")
	}
	deadline := time.NewTimer(time.Until(sess.expires))
	defer deadline.Stop()

	// Poll once before waiting. The user may approve while the interval is
	// still running, and waiting first adds that delay to every login.
	interval := sess.interval
	for {
		cred, pending, err := pollXAIToken(ctx, sess.deviceCode, "")
		if err != nil {
			return Credential{}, err
		}
		if pending == pendingNone {
			return cred, nil
		}
		if pending == pendingSlowDown {
			// RFC 8628 section 3.5: slow_down means this client is polling
			// too fast, and the interval must grow. Polling on unchanged
			// means the server keeps refusing.
			interval += xaiSlowDownStep
		}

		select {
		case <-ctx.Done():
			return Credential{}, ctx.Err()
		case <-deadline.C:
			return Credential{}, fmt.Errorf("auth: xAI device code expired")
		case <-time.After(interval):
		}
	}
}

func pollXAIToken(ctx context.Context, deviceCode, refreshTok string) (Credential, pollState, error) {
	data := url.Values{
		"client_id": {xaiClientID},
	}
	if refreshTok != "" {
		data.Set("grant_type", "refresh_token")
		data.Set("refresh_token", refreshTok)
	} else {
		data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		data.Set("device_code", deviceCode)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, xaiTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return Credential{}, pendingNone, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credential{}, pendingNone, fmt.Errorf("auth: xAI token: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	switch body.Error {
	case "authorization_pending":
		return Credential{}, pendingApproval, nil
	case "slow_down":
		return Credential{}, pendingSlowDown, nil
	}
	if resp.StatusCode != http.StatusOK {
		if body.Error != "" {
			return Credential{}, pendingNone, fmt.Errorf("auth: xAI token: %s", body.Error)
		}
		return Credential{}, pendingNone, fmt.Errorf("auth: xAI token (%d): %s", resp.StatusCode, truncateBody(raw))
	}
	if body.AccessToken == "" {
		return Credential{}, pendingNone, fmt.Errorf("auth: xAI token missing access_token")
	}
	if body.RefreshToken == "" {
		body.RefreshToken = refreshTok
	}
	cred := Credential{Provider: ProviderXAI, AccessToken: body.AccessToken, RefreshToken: body.RefreshToken}
	if body.ExpiresIn > 0 {
		cred.ExpiresAt = time.Now().Add(time.Duration(body.ExpiresIn)*time.Second - 5*time.Minute)
	}
	return cred, pendingNone, nil
}

// RefreshXAI exchanges a SuperGrok refresh token.
func RefreshXAI(ctx context.Context, refreshToken string) (Credential, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Credential{}, fmt.Errorf("auth: missing xAI refresh token")
	}
	cred, pending, err := pollXAIToken(ctx, "", refreshToken)
	if err != nil {
		return Credential{}, err
	}
	// A refresh is not a device login, so there is nobody to approve and
	// nothing to wait for. Reporting success here would hand the caller an
	// empty credential, which it stores over the working one.
	if pending != pendingNone {
		return Credential{}, fmt.Errorf("auth: xAI refused the refresh: %s", pendingReason(pending))
	}
	return cred, nil
}

func pendingReason(p pollState) string {
	if p == pendingSlowDown {
		return "rate limited"
	}
	return "authorization pending"
}
