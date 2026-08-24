package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pulseaiclub/phi/internal/llm"
)

func TestToAPIMessagesPlainString(t *testing.T) {
	msgs := toAPIMessages([]llm.Message{{Role: llm.RoleUser, Content: "hi"}})
	if s, ok := msgs[0].Content.(string); !ok || s != "hi" {
		t.Fatalf("content %+v", msgs[0].Content)
	}
}

func TestToAPIMessagesImages(t *testing.T) {
	msgs := toAPIMessages([]llm.Message{{
		Role:    llm.RoleUser,
		Content: "look",
		Images:  []llm.Image{{MIME: "image/png", Data: []byte("PNG")}},
	}})
	parts, ok := msgs[0].Content.([]apiContentPart)
	if !ok || len(parts) != 2 {
		t.Fatalf("parts %+v", msgs[0].Content)
	}
	if parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Fatalf("types %q %q", parts[0].Type, parts[1].Type)
	}
	if parts[1].ImageURL == nil || !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("url %+v", parts[1].ImageURL)
	}
	raw, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"images"`) {
		t.Fatalf("api body must not leak Phi images field: %s", raw)
	}
}

func TestToAPIMessagesToolImages(t *testing.T) {
	msgs := toAPIMessages([]llm.Message{
		{
			Role:       llm.RoleTool,
			ToolCallID: "c1",
			Content:    "meta",
			Images:     []llm.Image{{MIME: "image/png", Data: []byte("PNG")}},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("len %d", len(msgs))
	}
	if msgs[0].Role != "tool" {
		t.Fatalf("first %s", msgs[0].Role)
	}
	if s, ok := msgs[0].Content.(string); !ok || s != "meta" {
		t.Fatalf("tool content %+v", msgs[0].Content)
	}
	if msgs[1].Role != "user" {
		t.Fatalf("vision follow-up %s", msgs[1].Role)
	}
	parts, ok := msgs[1].Content.([]apiContentPart)
	if !ok || len(parts) != 1 || parts[0].Type != "image_url" {
		t.Fatalf("parts %+v", msgs[1].Content)
	}
}

func TestResponsesUserContent(t *testing.T) {
	got := responsesUserContent(llm.Message{
		Content: "x",
		Images:  []llm.Image{{MIME: "image/jpeg", Data: []byte{1, 2, 3}}},
	})
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), "input_image") || !strings.Contains(string(raw), "input_text") {
		t.Fatalf("got %s", raw)
	}
}
