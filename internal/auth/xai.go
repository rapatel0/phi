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
		"referrer":  {"phi"},
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
	ticker := time.NewTicker(sess.interval)
	defer ticker.Stop()
	deadline := time.NewTimer(time.Until(sess.expires))
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return Credential{}, ctx.Err()
		case <-deadline.C:
			return Credential{}, fmt.Errorf("auth: xAI device code expired")
		case <-ticker.C:
			cred, pending, err := pollXAIToken(ctx, sess.deviceCode, "")
			if err != nil {
				return Credential{}, err
			}
			if pending {
				continue
			}
			return cred, nil
		}
	}
}

func pollXAIToken(ctx context.Context, deviceCode, refreshTok string) (Credential, bool, error) {
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
		return Credential{}, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credential{}, false, fmt.Errorf("auth: xAI token: %w", err)
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
	if body.Error == "authorization_pending" {
		return Credential{}, true, nil
	}
	if body.Error == "slow_down" {
		return Credential{}, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		if body.Error != "" {
			return Credential{}, false, fmt.Errorf("auth: xAI token: %s", body.Error)
		}
		return Credential{}, false, fmt.Errorf("auth: xAI token (%d): %s", resp.StatusCode, truncateBody(raw))
	}
	if body.AccessToken == "" {
		return Credential{}, false, fmt.Errorf("auth: xAI token missing access_token")
	}
	if body.RefreshToken == "" {
		body.RefreshToken = refreshTok
	}
	cred := Credential{Provider: ProviderXAI, AccessToken: body.AccessToken, RefreshToken: body.RefreshToken}
	if body.ExpiresIn > 0 {
		cred.ExpiresAt = time.Now().Add(time.Duration(body.ExpiresIn)*time.Second - 5*time.Minute)
	}
	return cred, false, nil
}

// RefreshXAI exchanges a SuperGrok refresh token.
func RefreshXAI(ctx context.Context, refreshToken string) (Credential, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Credential{}, fmt.Errorf("auth: missing xAI refresh token")
	}
	cred, _, err := pollXAIToken(ctx, "", refreshToken)
	return cred, err
}
