package anthropic

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pulseaiclub/phi/internal/llm"
)

func TestSetAuthHeadersOAuth(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	setAuthHeaders(req, llm.ModelConfig{APIKey: "sk-ant-oat-x"})
	if got := req.Header.Get("Authorization"); got != "Bearer sk-ant-oat-x" {
		t.Fatalf("Authorization = %q", got)
	}
	if req.Header.Get("X-Api-Key") != "" {
		t.Fatal("X-Api-Key must be absent for OAuth")
	}
	if !strings.Contains(req.Header.Get("anthropic-beta"), "oauth-2025-04-20") {
		t.Fatalf("missing oauth beta: %q", req.Header.Get("anthropic-beta"))
	}
}

func TestSetAuthHeadersAPIKey(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	setAuthHeaders(req, llm.ModelConfig{APIKey: "sk-ant-api03-x"})
	if req.Header.Get("X-Api-Key") != "sk-ant-api03-x" {
		t.Fatalf("X-Api-Key = %q", req.Header.Get("X-Api-Key"))
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("Authorization should be empty for API keys")
	}
}
