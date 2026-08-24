package openai

import (
	"encoding/base64"

	"github.com/rapatel0/alpha/internal/llm"
)

type apiChatMessage struct {
	Role             string         `json:"role"`
	Content          any            `json:"content,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []llm.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type apiContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *apiImageURL `json:"image_url,omitempty"`
}

type apiImageURL struct {
	URL string `json:"url"`
}

func toAPIMessages(msgs []llm.Message) []apiChatMessage {
	out := make([]apiChatMessage, 0, len(msgs))
	var pending []llm.Image
	flushVision := func() {
		if len(pending) == 0 {
			return
		}
		out = append(out, apiChatMessage{
			Role:    string(llm.RoleUser),
			Content: openaiContent(llm.Message{Images: pending}),
		})
		pending = nil
	}
	for _, m := range msgs {
		if m.Role != llm.RoleTool {
			flushVision()
		}
		content := openaiContent(m)
		if m.Role == llm.RoleTool {
			content = m.Content
		}
		out = append(out, apiChatMessage{
			Role:             string(m.Role),
			Content:          content,
			ReasoningContent: m.ReasoningContent,
			ToolCalls:        m.ToolCalls,
			ToolCallID:       m.ToolCallID,
		})
		if m.Role == llm.RoleTool && m.HasImages() {
			pending = append(pending, m.Images...)
		}
	}
	flushVision()
	return out
}

func openaiContent(m llm.Message) any {
	if !m.HasImages() {
		return m.Content
	}
	var parts []apiContentPart
	if m.Content != "" {
		parts = append(parts, apiContentPart{Type: "text", Text: m.Content})
	}
	for _, img := range m.Images {
		mime := img.MIME
		if mime == "" {
			mime = "image/png"
		}
		parts = append(parts, apiContentPart{
			Type: "image_url",
			ImageURL: &apiImageURL{
				URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
			},
		})
	}
	return parts
}

func responsesUserContent(m llm.Message) any {
	if !m.HasImages() {
		return m.Content
	}
	type part struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		ImageURL string `json:"image_url,omitempty"`
	}
	var parts []part
	if m.Content != "" {
		parts = append(parts, part{Type: "input_text", Text: m.Content})
	}
	for _, img := range m.Images {
		mime := img.MIME
		if mime == "" {
			mime = "image/png"
		}
		parts = append(parts, part{
			Type:     "input_image",
			ImageURL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
		})
	}
	return parts
}
