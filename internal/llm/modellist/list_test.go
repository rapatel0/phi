package modellist

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pulseaiclub/phi/internal/llm"
)

func TestFetchOpenAI(t *testing.T) {
	t.Setenv("PHI_MODEL_LIST", "")
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "gpt-4o"},
				{"id": "whisper-1"},
				{"id": "gpt-4o"},
			},
		})
	}))
	t.Cleanup(srv.Close)
	ids, err := Fetch(t.Context(), llm.ModelConfig{Name: "gpt-4o", APIKey: "sk-test", BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test" || !strings.HasSuffix(gotPath, "/models") {
		t.Fatalf("auth=%q path=%q", gotAuth, gotPath)
	}
	if len(ids) != 1 || ids[0] != "gpt-4o" {
		t.Fatalf("ids %v", ids)
	}
}

func TestFetchAnthropicOAuth(t *testing.T) {
	t.Setenv("PHI_MODEL_LIST", "")
	var gotKey, gotAuth, gotVer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		gotAuth = r.Header.Get("Authorization")
		gotVer = r.Header.Get("Anthropic-Version")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "claude-sonnet-5"}},
		})
	}))
	t.Cleanup(srv.Close)
	ids, err := Fetch(t.Context(), llm.ModelConfig{
		Name: "claude-sonnet-5", APIKey: "sk-ant-oat-x", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "" || !strings.HasPrefix(gotAuth, "Bearer ") || gotVer != anthropicVer {
		t.Fatalf("key=%q auth=%q ver=%q", gotKey, gotAuth, gotVer)
	}
	if len(ids) != 1 || ids[0] != "claude-sonnet-5" {
		t.Fatalf("ids %v", ids)
	}
}

func TestFetchGemini(t *testing.T) {
	t.Setenv("PHI_MODEL_LIST", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "gem-k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"name": "models/gemini-2.5-flash", "supportedGenerationMethods": []string{"generateContent"}},
				{"name": "models/embedding-001", "supportedGenerationMethods": []string{"embedContent"}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	ids, err := Fetch(t.Context(), llm.ModelConfig{
		Name: "gemini-2.5-flash", APIKey: "gem-k",
		BaseURL: srv.URL + "/v1beta",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "gemini-2.5-flash" {
		t.Fatalf("ids %v", ids)
	}
}

func TestFetchDisabled(t *testing.T) {
	t.Setenv("PHI_MODEL_LIST", "0")
	ids, err := Fetch(t.Context(), llm.ModelConfig{Name: "x", APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if err != nil || ids != nil {
		t.Fatalf("got %v %v", ids, err)
	}
}

func TestFetchHTTPError(t *testing.T) {
	t.Setenv("PHI_MODEL_LIST", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"nope"}`)
	}))
	t.Cleanup(srv.Close)
	_, err := Fetch(t.Context(), llm.ModelConfig{Name: "gpt-4o", APIKey: "sk", BaseURL: srv.URL + "/v1"})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err %v", err)
	}
}
