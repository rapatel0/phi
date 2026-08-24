package anthropic

import (
	"net/http"
	"strings"
	"testing"

	"github.com/rapatel0/alpha/internal/llm"
)

func TestSetAuthHeadersOAuth(t *testing.T) {
	req, _ := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"https://api.anthropic.com/v1/messages",
		http.NoBody,
	)
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
	req, _ := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"https://api.anthropic.com/v1/messages",
		http.NoBody,
	)
	setAuthHeaders(req, llm.ModelConfig{APIKey: "sk-ant-api03-x"})
	if req.Header.Get("X-Api-Key") != "sk-ant-api03-x" {
		t.Fatalf("X-Api-Key = %q", req.Header.Get("X-Api-Key"))
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("Authorization should be empty for API keys")
	}
}

func TestOutboundClaudeCodeNamesForNewTools(t *testing.T) {
	cases := map[string]string{
		"skill":             "Skill",
		"webfetch":          "WebFetch",
		"websearch":         "WebSearch",
		"ask_user_question": "AskUserQuestion",
		"todo_write":        "TodoWrite",
	}
	for in, want := range cases {
		if got := outboundToolName(in, true); got != want {
			t.Fatalf("%s = %q, want %q", in, got, want)
		}
		if got := inboundToolName(want, true); got != in {
			t.Fatalf("inbound %s = %q, want %q", want, got, in)
		}
	}
}

func TestUniqueOutboundNameCollisionFallsBack(t *testing.T) {
	used := map[string]struct{}{}
	if got := uniqueOutboundName("agent_wait", true, used); got != "TaskOutput" {
		t.Fatalf("wait = %q", got)
	}
	if got := uniqueOutboundName("agent_wait", true, used); got != "agent_wait" {
		t.Fatalf("second wait should keep original, got %q", got)
	}
}
