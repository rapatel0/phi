package modellist

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/llm"
)

// codexJWT is an unsigned Codex-shaped OAuth token with a chatgpt_account_id claim.
const codexJWT = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
	"eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdC0xMjMifX0.sig"

func TestDisabled(t *testing.T) {
	cases := map[string]bool{"0": true, "false": true, "FALSE": true, "1": false, "": false}
	for v, want := range cases {
		t.Setenv("ALPHA_MODEL_LIST", v)
		if got := Disabled(); got != want {
			t.Fatalf("ALPHA_MODEL_LIST=%q: Disabled()=%v want %v", v, got, want)
		}
	}
}

func TestFetchMissingAPIKey(t *testing.T) {
	t.Setenv("ALPHA_MODEL_LIST", "")
	_, err := Fetch(t.Context(), llm.ModelConfig{Name: "gpt-4o", APIKey: "  "})
	if err == nil || !strings.Contains(err.Error(), "missing api key") {
		t.Fatalf("err %v", err)
	}
}

func TestFetchSortsAndDedupes(t *testing.T) {
	t.Setenv("ALPHA_MODEL_LIST", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "gpt-4o"}, {"id": "gpt-3.5"}, {"id": "gpt-4o"}, {"id": "o3-mini"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	ids, err := Fetch(t.Context(), llm.ModelConfig{Name: "gpt-4o", APIKey: "sk", BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gpt-3.5", "gpt-4o", "o3-mini"}
	if len(ids) != len(want) {
		t.Fatalf("ids %v want %v", ids, want)
	}
	for i, id := range ids {
		if id != want[i] {
			t.Fatalf("ids %v want %v", ids, want)
		}
	}
}

func TestFetchStatusFallsBackToStatusText(t *testing.T) {
	t.Setenv("ALPHA_MODEL_LIST", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	_, err := Fetch(t.Context(), llm.ModelConfig{Name: "gpt-4o", APIKey: "sk", BaseURL: srv.URL + "/v1"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err %v", err)
	}
}

func TestFetchInvalidJSON(t *testing.T) {
	t.Setenv("ALPHA_MODEL_LIST", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	t.Cleanup(srv.Close)

	_, err := Fetch(t.Context(), llm.ModelConfig{Name: "gpt-4o", APIKey: "sk", BaseURL: srv.URL + "/v1"})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err %v", err)
	}
}

// Codex only ever talks to chatgpt.com, so the headers are checked directly
// instead of through a loopback test server.
func TestSetHeadersCodex(t *testing.T) {
	if !auth.IsCodexOAuthToken(codexJWT) {
		t.Fatal("fixture token is not recognized as a Codex OAuth token")
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	setHeaders(req, llm.ModelConfig{Name: "gpt-5.5", APIKey: codexJWT}, kindCodex)

	if got := req.Header.Get("Authorization"); got != "Bearer "+codexJWT {
		t.Fatalf("auth %q", got)
	}
	if got := req.Header.Get("OpenAI-Beta"); got != "responses=experimental" {
		t.Fatalf("beta %q", got)
	}
	if got := req.Header.Get("originator"); got != "alpha" {
		t.Fatalf("originator %q", got)
	}
	if got := req.Header.Get("chatgpt-account-id"); got != "acct-123" {
		t.Fatalf("account %q", got)
	}
}

func TestSetHeadersCodexWithoutAccountClaim(t *testing.T) {
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	setHeaders(req, llm.ModelConfig{APIKey: "not-a-jwt"}, kindCodex)
	if got := req.Header.Get("chatgpt-account-id"); got != "" {
		t.Fatalf("account header must be absent without the claim, got %q", got)
	}
}

func TestFetchAnthropicAPIKeyHeader(t *testing.T) {
	t.Setenv("ALPHA_MODEL_LIST", "")
	var gotKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "claude-sonnet-5"}},
		})
	}))
	t.Cleanup(srv.Close)

	if _, err := Fetch(t.Context(), llm.ModelConfig{
		Name: "claude-sonnet-5", APIKey: "sk-ant-api03-x", BaseURL: srv.URL,
	}); err != nil {
		t.Fatal(err)
	}
	if gotKey != "sk-ant-api03-x" || gotAuth != "" {
		t.Fatalf("key=%q auth=%q", gotKey, gotAuth)
	}
}

func TestEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		cfg      llm.ModelConfig
		wantURL  string
		wantKind providerKind
	}{
		{
			name:     "openai default",
			cfg:      llm.ModelConfig{Name: "gpt-4o", APIKey: "sk"},
			wantURL:  "https://api.openai.com/v1/models",
			wantKind: kindOpenAI,
		},
		{
			name:     "openai custom base",
			cfg:      llm.ModelConfig{Name: "gpt-4o", APIKey: "sk", BaseURL: "https://proxy.local/v1/"},
			wantURL:  "https://proxy.local/v1/models",
			wantKind: kindOpenAI,
		},
		{
			name:     "openai base already ends in models",
			cfg:      llm.ModelConfig{Name: "gpt-4o", APIKey: "sk", BaseURL: "https://proxy.local/v1/models"},
			wantURL:  "https://proxy.local/v1/models",
			wantKind: kindOpenAI,
		},
		{
			name:     "anthropic default",
			cfg:      llm.ModelConfig{Name: "claude-sonnet-5", APIKey: "sk-ant-api03-x"},
			wantURL:  "https://api.anthropic.com/v1/models",
			wantKind: kindAnthropic,
		},
		{
			name:     "anthropic base with v1",
			cfg:      llm.ModelConfig{Name: "claude-sonnet-5", APIKey: "k", BaseURL: "https://api.anthropic.com/v1"},
			wantURL:  "https://api.anthropic.com/v1/models",
			wantKind: kindAnthropic,
		},
		{
			name:     "codex oauth default base",
			cfg:      llm.ModelConfig{Name: "gpt-5.5", APIKey: codexJWT},
			wantURL:  auth.CodexBackendBaseURL + "/models",
			wantKind: kindCodex,
		},
		{
			name:     "codex oauth rewrites openai base",
			cfg:      llm.ModelConfig{Name: "gpt-5.5", APIKey: codexJWT, BaseURL: "https://api.openai.com/v1"},
			wantURL:  auth.CodexBackendBaseURL + "/models",
			wantKind: kindCodex,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, kind, err := endpoint(c.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.wantURL || kind != c.wantKind {
				t.Fatalf("url=%q kind=%v want %q %v", got, kind, c.wantURL, c.wantKind)
			}
		})
	}
}

func TestEndpointGeminiVertexOmitsKeyQuery(t *testing.T) {
	got, kind, err := endpoint(llm.ModelConfig{
		Name: "gemini-2.5-pro", APIKey: "k",
		BaseURL: "https://us-central1-aiplatform.googleapis.com/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if kind != kindGemini {
		t.Fatalf("kind %v", kind)
	}
	if strings.Contains(got, "key=") {
		t.Fatalf("vertex url must not carry an api key: %q", got)
	}
}

func TestSetHeadersGeminiVertexUsesBearer(t *testing.T) {
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	setHeaders(req, llm.ModelConfig{
		APIKey: "k", BaseURL: "https://us-central1-aiplatform.googleapis.com/v1",
	}, kindGemini)
	if req.Header.Get("Authorization") != "Bearer k" {
		t.Fatalf("auth %q", req.Header.Get("Authorization"))
	}
}

func TestSetHeadersGeminiPublicOmitsAuth(t *testing.T) {
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	setHeaders(req, llm.ModelConfig{APIKey: "k"}, kindGemini)
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("public gemini must not set Authorization: %q", req.Header.Get("Authorization"))
	}
}

func TestParseIDsPrefersFirstNonEmptyName(t *testing.T) {
	body := []byte(`{"data":[
		{"display_name":"From Display Name"},
		{"displayName":"From Camel Name"},
		{"name":"models/gemini-2.5-flash"},
		{"id":"  "}
	]}`)
	ids, err := parseIDs(body, kindOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"From Display Name", "From Camel Name", "gemini-2.5-flash"}
	if len(ids) != len(want) {
		t.Fatalf("ids %v want %v", ids, want)
	}
	for i, id := range ids {
		if id != want[i] {
			t.Fatalf("ids %v want %v", ids, want)
		}
	}
}

func TestParseIDsGeminiKeepsModelsWithoutMethodList(t *testing.T) {
	body := []byte(`{"models":[
		{"name":"models/gemini-2.5-flash"},
		{"name":"models/gemini-2.5-pro","supportedGenerationMethods":["streamGenerateContent"]},
		{"name":"models/embedding-001","supportedGenerationMethods":["embedContent"]}
	]}`)
	ids, err := parseIDs(body, kindGemini)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids %v", ids)
	}
}

func TestKeepFiltersNonChatModels(t *testing.T) {
	drop := []string{
		"text-embedding-3-large", "whisper-1", "tts-1", "dall-e-3", "davinci-002",
		"babbage-002", "text-moderation-latest", "veo-3", "sora-2", "imagen-4",
		"gpt-4o-transcribe", "gpt-4o-realtime-preview", "aqa", "computer-use-preview",
	}
	for _, id := range drop {
		if keep(id) {
			t.Errorf("keep(%q) = true, want false", id)
		}
	}
	for _, id := range []string{"gpt-4o", "claude-sonnet-5", "gemini-2.5-pro", "grok-4"} {
		if !keep(id) {
			t.Errorf("keep(%q) = false, want true", id)
		}
	}
}

func TestSupportsGenerate(t *testing.T) {
	if !supportsGenerate([]string{"embedContent", "generateContent"}) {
		t.Error("generateContent must be supported")
	}
	if !supportsGenerate([]string{"streamGenerateContent"}) {
		t.Error("streamGenerateContent must be supported")
	}
	if supportsGenerate([]string{"embedContent"}) {
		t.Error("embedContent alone must not be supported")
	}
}

func TestListClientBlocksCrossOriginRedirect(t *testing.T) {
	t.Setenv("ALPHA_MODEL_LIST", "")
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "leaked"}}})
	}))
	t.Cleanup(other.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/models", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	_, err := Fetch(t.Context(), llm.ModelConfig{Name: "gpt-4o", APIKey: "sk", BaseURL: srv.URL + "/v1"})
	if err == nil || !strings.Contains(err.Error(), "redirect changed origin") {
		t.Fatalf("cross-origin redirect must be refused, got %v", err)
	}
}

func TestListClientAllowsSameOriginRedirect(t *testing.T) {
	t.Setenv("ALPHA_MODEL_LIST", "")
	var mux http.ServeMux
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v2/models", http.StatusFound)
	})
	mux.HandleFunc("/v2/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "gpt-4o"}}})
	})
	srv := httptest.NewServer(&mux)
	t.Cleanup(srv.Close)

	ids, err := Fetch(t.Context(), llm.ModelConfig{Name: "gpt-4o", APIKey: "sk", BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "gpt-4o" {
		t.Fatalf("ids %v", ids)
	}
}
