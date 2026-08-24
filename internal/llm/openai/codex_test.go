package openai

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pulseaiclub/phi/internal/llm"
)

func TestUseCodexBackend(t *testing.T) {
	tok := fakeCodexJWT(t)
	if !UseCodexBackend(llm.ModelConfig{APIKey: tok}) {
		t.Fatal("expected codex backend for JWT")
	}
	if UseCodexBackend(llm.ModelConfig{APIKey: "sk-test"}) {
		t.Fatal("sk- keys must not use codex backend")
	}
}

func TestStreamCodexText(t *testing.T) {
	tok := fakeCodexJWT(t)
	var gotPath, gotOriginator, gotAccount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotOriginator = r.Header.Get("originator")
		gotAccount = r.Header.Get("chatgpt-account-id")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"hi"}`,
			"",
			`data: {"type":"response.completed"}`,
			"",
		}, "\n")))
	}))
	t.Cleanup(srv.Close)

	cfg := llm.ModelConfig{Name: "gpt-5.4-codex", APIKey: tok, BaseURL: srv.URL}
	req := BuildResponsesRequest(cfg, "sys", []llm.Message{{Role: llm.RoleUser, Content: "hello"}}, nil)
	var text strings.Builder
	for ev, err := range StreamCodex(t.Context(), srv.Client(), cfg, req) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Type == llm.StreamEventTypeDelta {
			text.WriteString(ev.Delta.Content)
		}
	}
	if gotPath != "/responses" {
		t.Fatalf("path %q", gotPath)
	}
	if gotOriginator != "phi" {
		t.Fatalf("originator %q", gotOriginator)
	}
	if gotAccount != "acct-1" {
		t.Fatalf("account %q", gotAccount)
	}
	if text.String() != "hi" {
		t.Fatalf("text %q", text.String())
	}
}

func fakeCodexJWT(t *testing.T) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "acct-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}
