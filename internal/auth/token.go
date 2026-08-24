package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// IsAnthropicOAuthToken reports a Claude Pro/Max access token (sk-ant-oat…).
func IsAnthropicOAuthToken(key string) bool {
	return strings.Contains(key, "sk-ant-oat")
}

// IsCodexOAuthToken reports a ChatGPT Codex OAuth JWT (not an sk- platform key).
func IsCodexOAuthToken(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "sk-") {
		return false
	}
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return false
		}
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil {
		return false
	}
	_, ok := raw["https://api.openai.com/auth"]
	return ok
}

// ChatGPTAccountID pulls chatgpt_account_id from a Codex OAuth JWT.
func ChatGPTAccountID(token string) string {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil {
		return ""
	}
	blob, ok := raw["https://api.openai.com/auth"]
	if !ok {
		return ""
	}
	var claims struct {
		ChatGPTAccountID string `json:"chatgpt_account_id"`
	}
	if json.Unmarshal(blob, &claims) != nil {
		return ""
	}
	return claims.ChatGPTAccountID
}
