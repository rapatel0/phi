package auth

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	codexClientID         = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexDeviceVerifyURL  = "https://auth.openai.com/codex/device"
	codexDeviceRedirect   = "https://auth.openai.com/deviceauth/callback"
	codexDeviceMaxWait    = 15 * time.Minute
	codexDeviceDefaultInt = 5 * time.Second
)

var (
	codexTokenURL  = "https://auth.openai.com/oauth/token"
	codexDeviceURL = "https://auth.openai.com/api/accounts/deviceauth"
)

// CodexDevice is an in-progress ChatGPT device login. Display UserCode and
// VerificationURL, then call CompleteCodexDevice.
type CodexDevice struct {
	VerificationURL string
	UserCode        string

	deviceAuthID string
	interval     time.Duration
}

// StartCodexDevice requests a user code. No local callback listener.
func StartCodexDevice(ctx context.Context) (*CodexDevice, error) {
	status, body, err := postJSON(ctx, codexDeviceURL+"/usercode", map[string]string{
		"client_id": codexClientID,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: codex device code: %w", err)
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("auth: Codex device login is not enabled for this server — use a platform API key")
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("auth: codex device code (%d): %s", status, truncateBody(body))
	}
	var ucr struct {
		DeviceAuthID string        `json:"device_auth_id"`
		UserCode     string        `json:"user_code"`
		UserCodeAlt  string        `json:"usercode"`
		Interval     codexInterval `json:"interval"`
	}
	if err := json.Unmarshal(body, &ucr); err != nil {
		return nil, fmt.Errorf("auth: codex device code json: %w", err)
	}
	userCode := cmp.Or(ucr.UserCode, ucr.UserCodeAlt)
	if ucr.DeviceAuthID == "" || userCode == "" {
		return nil, fmt.Errorf("auth: codex device code missing device_auth_id or user_code")
	}
	interval := time.Duration(ucr.Interval) * time.Second
	if interval <= 0 {
		interval = codexDeviceDefaultInt
	}
	return &CodexDevice{
		VerificationURL: codexDeviceVerifyURL,
		UserCode:        userCode,
		deviceAuthID:    ucr.DeviceAuthID,
		interval:        interval,
	}, nil
}

// CompleteCodexDevice polls until the user approves, then exchanges the code.
func CompleteCodexDevice(ctx context.Context, sess *CodexDevice) (Credential, error) {
	if sess == nil {
		return Credential{}, fmt.Errorf("auth: nil codex device session")
	}
	approval, err := pollCodexApproval(ctx, sess)
	if err != nil {
		return Credential{}, err
	}
	return exchangeCodexCode(ctx, approval.AuthorizationCode, codexDeviceRedirect, approval.CodeVerifier)
}

type codexApproval struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

func pollCodexApproval(ctx context.Context, sess *CodexDevice) (*codexApproval, error) {
	deadline := time.NewTimer(codexDeviceMaxWait)
	defer deadline.Stop()
	ticker := time.NewTicker(sess.interval)
	defer ticker.Stop()
	payload := map[string]string{
		"device_auth_id": sess.deviceAuthID,
		"user_code":      sess.UserCode,
	}
	for {
		status, body, err := postJSON(ctx, codexDeviceURL+"/token", payload)
		switch {
		case err != nil:
			return nil, fmt.Errorf("auth: polling Codex device: %w", err)
		case status == http.StatusOK:
			var approval codexApproval
			if err := json.Unmarshal(body, &approval); err != nil {
				return nil, fmt.Errorf("auth: Codex device json: %w", err)
			}
			if approval.AuthorizationCode == "" || approval.CodeVerifier == "" {
				return nil, fmt.Errorf("auth: Codex device response missing authorization_code or code_verifier")
			}
			return &approval, nil
		case status == http.StatusForbidden, status == http.StatusNotFound:
			// not approved yet
		default:
			return nil, fmt.Errorf("auth: Codex device (%d): %s", status, truncateBody(body))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("auth: Codex device timed out after %s", codexDeviceMaxWait)
		case <-ticker.C:
		}
	}
}

func exchangeCodexCode(ctx context.Context, code, redirect, verifier string) (Credential, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {codexClientID},
		"code_verifier": {verifier},
	}
	return postCodexToken(ctx, data)
}

// RefreshCodex exchanges a Codex refresh token.
func RefreshCodex(ctx context.Context, refreshToken string) (Credential, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Credential{}, fmt.Errorf("auth: missing Codex refresh token")
	}
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {codexClientID},
	}
	return postCodexToken(ctx, data)
}

func postCodexToken(ctx context.Context, data url.Values) (Credential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credential{}, fmt.Errorf("auth: Codex token: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Credential{}, fmt.Errorf("auth: Codex token (%d): %s", resp.StatusCode, truncateBody(raw))
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return Credential{}, fmt.Errorf("auth: Codex token json: %w", err)
	}
	if tok.AccessToken == "" {
		return Credential{}, fmt.Errorf("auth: Codex token missing access_token")
	}
	cred := Credential{
		Provider:     ProviderCodex,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
	}
	if tok.ExpiresIn > 0 {
		cred.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second - 5*time.Minute)
	}
	return cred, nil
}

func postJSON(ctx context.Context, endpoint string, payload any) (int, []byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// codexInterval accepts JSON string ("5") or number.
type codexInterval int

func (c *codexInterval) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(strings.Trim(string(b), `"`))
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("device auth interval %q: %w", s, err)
	}
	*c = codexInterval(n)
	return nil
}
