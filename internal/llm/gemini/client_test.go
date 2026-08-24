package gemini

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rapatel0/alpha/internal/llm"
)

func TestBuildRequestImages(t *testing.T) {
	req := BuildRequest(llm.ModelConfig{Name: "gemini-2.5-flash"}, "", []llm.Message{{
		Role:    llm.RoleUser,
		Content: "see",
		Images:  []llm.Image{{MIME: "image/png", Data: []byte("PNG")}},
	}}, nil)
	if len(req.Contents) != 1 || len(req.Contents[0].Parts) != 2 {
		t.Fatalf("parts %+v", req.Contents)
	}
	if req.Contents[0].Parts[0].Text != "see" {
		t.Fatalf("text %q", req.Contents[0].Parts[0].Text)
	}
	if req.Contents[0].Parts[1].InlineData == nil || req.Contents[0].Parts[1].InlineData.MIMEType != "image/png" {
		t.Fatalf("inline %+v", req.Contents[0].Parts[1].InlineData)
	}
}

func TestIsProvider(t *testing.T) {
	if !IsProvider(llm.ModelConfig{Name: "gemini-2.5-flash"}) {
		t.Fatal("gemini name")
	}
	if IsProvider(llm.ModelConfig{Name: "gpt-4o"}) {
		t.Fatal("gpt should not be gemini")
	}
}

func TestStreamText(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}]}\n\n"))
	}))
	t.Cleanup(srv.Close)
	cfg := llm.ModelConfig{Name: "gemini-2.5-flash", APIKey: "k", BaseURL: srv.URL}
	req := BuildRequest(cfg, "sys", []llm.Message{{Role: llm.RoleUser, Content: "hello"}}, nil)
	var text strings.Builder
	for ev, err := range Stream(t.Context(), srv.Client(), cfg, req) {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Type == llm.StreamEventTypeDelta {
			text.WriteString(ev.Delta.Content)
		}
	}
	if !strings.Contains(gotPath, "gemini-2.5-flash:streamGenerateContent") {
		t.Fatalf("path %q", gotPath)
	}
	if text.String() != "hi" {
		t.Fatalf("text %q", text.String())
	}
}
