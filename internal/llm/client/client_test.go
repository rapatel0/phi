package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/llm"
)

func TestIsAnthropicProvider(t *testing.T) {
	cases := []struct {
		cfg  llm.ModelConfig
		want bool
	}{
		{llm.ModelConfig{Name: "claude-sonnet-4-20250514", BaseURL: "https://api.anthropic.com"}, true},
		{llm.ModelConfig{Name: "gpt-4o", BaseURL: "https://api.anthropic.com"}, true},
		{llm.ModelConfig{Name: "claude-3-5-sonnet", BaseURL: "https://api.openai.com/v1"}, true},
		{llm.ModelConfig{Name: "gpt-4o", BaseURL: "https://api.openai.com/v1"}, false},
		{llm.ModelConfig{Name: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1"}, false},
	}
	for i, c := range cases {
		if got := isAnthropicProvider(c.cfg); got != c.want {
			t.Fatalf("case %d: isAnthropicProvider(%+v) = %v, want %v", i, c.cfg, got, c.want)
		}
	}
}

func TestClientStreamAnthropicEndToEnd(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-Api-Key")
		gotVersion = r.Header.Get("Anthropic-Version")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"type":"message_start","message":{"usage":{"input_tokens":3}}}`,
			"",
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
			"",
			`data: {"type":"message_delta","usage":{"output_tokens":2}}`,
			"",
			`data: {"type":"message_stop"}`,
			"",
		}, "\n")))
	}))
	defer srv.Close()

	client := NewClient(
		llm.ModelConfig{Name: "claude-sonnet-4-20250514", BaseURL: srv.URL, APIKey: "sk-test"},
		nil,
		"be brief",
	)
	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}))

	if gotPath != "/v1/messages" {
		t.Fatalf("expected POST /v1/messages, got %q", gotPath)
	}
	if gotKey != "sk-test" {
		t.Fatalf("expected X-Api-Key sk-test, got %q", gotKey)
	}
	if gotVersion == "" {
		t.Fatal("expected Anthropic-Version header")
	}
	var text strings.Builder
	var done *llm.StreamEvent
	for _, ev := range events {
		if ev.Err != "" {
			t.Fatalf("stream error: %v", ev.Err)
		}
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			text.WriteString(ev.Delta.Content)
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	if text.String() != "hi" || done == nil || done.Partial.Choices[0].Message.Content != "hi" {
		t.Fatalf("unexpected stream result: text=%q done=%v", text.String(), done)
	}
	if done.Partial.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected total tokens: %+v", done.Partial.Usage)
	}
}

func TestClientStreamOpenAIEndToEnd(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"role":"assistant","content":"he"}}]}`,
			"",
			`data: {"choices":[{"delta":{"content":"llo"}}]}`,
			"",
			`data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")))
	}))
	defer srv.Close()

	client := NewClient(llm.ModelConfig{Name: "gpt-4o", BaseURL: srv.URL, APIKey: "sk-test"}, nil, "")
	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}))

	if gotPath != "/chat/completions" {
		t.Fatalf("expected POST /chat/completions, got %q", gotPath)
	}
	var text strings.Builder
	var done *llm.StreamEvent
	for _, ev := range events {
		if ev.Err != "" {
			t.Fatalf("stream error: %v", ev.Err)
		}
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			text.WriteString(ev.Delta.Content)
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	if text.String() != "hello" || done == nil || done.Partial.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected stream result: text=%q done=%v", text.String(), done)
	}
	if done.Partial.Usage.TotalTokens != 6 {
		t.Fatalf("unexpected total tokens: %+v", done.Partial.Usage)
	}
}

func TestClientCompactAnthropic(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"summary here"}]}`))
	}))
	defer srv.Close()

	client := NewClient(llm.ModelConfig{Name: "claude-sonnet-4-20250514", BaseURL: srv.URL, APIKey: "sk-test"}, nil, "")
	out, err := client.Compact(t.Context(), "summarize")
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("expected POST /v1/messages, got %q", gotPath)
	}
	if out != "summary here" {
		t.Fatalf("expected 'summary here', got %q", out)
	}
}

func TestClientCompactOpenAI(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"summary here"}}]}`))
	}))
	defer srv.Close()

	client := NewClient(llm.ModelConfig{Name: "gpt-4o", BaseURL: srv.URL, APIKey: "sk-test"}, nil, "")
	out, err := client.Compact(t.Context(), "summarize")
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("expected POST /chat/completions, got %q", gotPath)
	}
	if out != "summary here" {
		t.Fatalf("expected 'summary here', got %q", out)
	}
}

// collectEvents drains an iter.Seq2 into a slice.
func collectEvents(seq func(func(llm.StreamEvent, error) bool)) []llm.StreamEvent {
	var events []llm.StreamEvent
	for ev, err := range seq {
		if err != nil {
			events = append(events, llm.StreamEvent{Type: llm.StreamEventTypeError, Err: err.Error()})
			continue
		}
		events = append(events, ev)
	}
	return events
}

// Provider must agree with the branch Stream actually takes. If it drifts, a
// hook would apply one provider's limits to another provider's request.
func TestProviderMatchesDispatch(t *testing.T) {
	cases := []struct {
		name string
		cfg  llm.ModelConfig
		want string
	}{
		{
			"anthropic",
			llm.ModelConfig{Name: "claude-sonnet-4", BaseURL: "https://api.anthropic.com"},
			ProviderAnthropic,
		},
		{
			"gemini",
			llm.ModelConfig{Name: "gemini-2.0-flash", BaseURL: "https://generativelanguage.googleapis.com"},
			ProviderGemini,
		},
		{
			"openai",
			llm.ModelConfig{Name: "gpt-4o", BaseURL: "https://api.openai.com/v1"},
			ProviderOpenAI,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NewClient(tc.cfg, nil, "").Provider())
		})
	}
}

// A hook must be told which backend is about to receive the request.
func TestStreamPassesProviderToHooks(t *testing.T) {
	t.Cleanup(hooks.ResetProviderHooks)
	hooks.ResetProviderHooks()

	var got hooks.ProviderRequest
	hooks.RegisterProviderHook("probe", func(_ context.Context, req hooks.ProviderRequest) ([]llm.Message, error) {
		got = req
		return req.Messages, nil
	})

	// A stub endpoint so the stream reaches the hook and then ends cleanly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := llm.ModelConfig{Name: "claude-sonnet-4", BaseURL: srv.URL, APIKey: "x"}
	c := NewClient(cfg, nil, "sys")
	for range c.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hi"}}) { //nolint:revive // drain
	}
	assert.Equal(t, ProviderAnthropic, got.Provider)
	assert.Equal(t, "claude-sonnet-4", got.Model)
}
