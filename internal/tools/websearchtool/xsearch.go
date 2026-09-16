package websearchtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

// xaiSearchModel is used when the active model is not an xAI model, so the
// search still runs against a model that answers with sourced posts.
const xaiSearchModel = "grok-4.6"

// XSearchTool returns an X post search tool backed by the xAI Responses API.
//
// websearch covers the open web. X threads, handles, and announcement history
// are not indexed well enough there, so this keeps one narrow source behind one
// narrow tool instead of asking the model to fetch a timeline by hand.
func XSearchTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "x_search",
			Description: `Search X (Twitter) posts, handles, and threads. Not a replacement for websearch: websearch reads pages, x_search reads posts.

Use for announcement history, maintainer threads, or a named handle. Do not use it to open URLs.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"query": llm.Object{
						"type":        "string",
						"description": "Phrase or keywords to match in posts. Example: grok 4.6 release notes",
					},
					"handles": llm.Object{
						"type":        "array",
						"items":       llm.Object{"type": "string"},
						"description": "Optional X handles to restrict the search, without the @.",
					},
				},
				Required: []string{"query"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Query)
		},
		Run: runXSearch,
	}
}

func runXSearch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in struct {
		Query   string   `json:"query"`
		Handles []string `json:"handles"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("x_search args: %w", err)
	}
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return tooldef.Result{}, errors.New("x_search: query is required")
	}
	if len([]rune(q)) > maxQueryRun {
		q = string([]rune(q)[:maxQueryRun])
	}

	prompt := q
	if handles := joinHandles(in.Handles); handles != "" {
		prompt = fmt.Sprintf("%s\nOnly posts from these handles: %s.", q, handles)
	}

	cfg := xaiConfig(tooldef.Model(ctx))
	text, err := responsesSearch(ctx, prompt, cfg, xaiDefault)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("x_search: %w", err)
	}
	return tooldef.Result{Content: text, Output: text, Detail: q}, nil
}

// joinHandles normalizes handles so a pasted "@name" matches a bare "name".
func joinHandles(handles []string) string {
	parts := make([]string, 0, len(handles))
	for _, h := range handles {
		h = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(h), "@"))
		if h != "" {
			parts = append(parts, h)
		}
	}
	return strings.Join(parts, ", ")
}

// xaiConfig picks the xAI connection for the search request.
//
// An xAI session already holds the right key and base URL. Any other session
// still needs a key, so XAI_API_KEY is the documented fallback, matching what
// alpha config reads for the xAI provider.
func xaiConfig(cfg llm.ModelConfig) llm.ModelConfig {
	if nativeBackend(cfg) == "xai" {
		return cfg
	}
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("XAI_API_KEY"))
	}
	if cfg.Name == "" {
		cfg.Name = xaiSearchModel
	}
	cfg.APIKey = key
	cfg.BaseURL = xaiDefault
	return cfg
}
