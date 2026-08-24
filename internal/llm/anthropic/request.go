package anthropic

import (
	"encoding/base64"
	"encoding/json"

	"github.com/rapatel0/alpha/internal/llm"
)

type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    []sysBlock         `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

type anthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type sysBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

// anthropicContentBlock represents a single content block inside an Anthropic message.
type anthropicContentBlock struct {
	Type         string                `json:"type"`
	Text         string                `json:"text,omitempty"`
	ID           string                `json:"id,omitempty"`
	Name         string                `json:"name,omitempty"`
	Input        json.RawMessage       `json:"input,omitempty"`
	ToolUseID    string                `json:"tool_use_id,omitempty"`
	Content      string                `json:"content,omitempty"`
	Source       *anthropicImageSource `json:"source,omitempty"`
	CacheControl *cacheControl         `json:"cache_control,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func resolveCacheControl() *cacheControl {
	return &cacheControl{
		Type: "ephemeral",
		TTL:  "1h",
	}
}

func userContent(m llm.Message) any {
	if !m.HasImages() {
		return m.Content
	}
	var blocks []anthropicContentBlock
	if m.Content != "" {
		blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
	}
	blocks = append(blocks, imageBlocks(m.Images)...)
	return blocks
}

func imageBlocks(images []llm.Image) []anthropicContentBlock {
	var blocks []anthropicContentBlock
	for _, img := range images {
		mime := img.MIME
		if mime == "" {
			mime = "image/png"
		}
		blocks = append(blocks, anthropicContentBlock{
			Type: "image",
			Source: &anthropicImageSource{
				Type:      "base64",
				MediaType: mime,
				Data:      base64.StdEncoding.EncodeToString(img.Data),
			},
		})
	}
	return blocks
}
