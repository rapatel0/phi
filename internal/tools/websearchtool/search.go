package websearchtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/tools/tooldef"
)

const maxQueryRun = 200

// WebSearchTool returns a web search tool. Native provider search is used when
// the current model supports it (Anthropic / Gemini / OpenAI / xAI); otherwise
// DuckDuckGo HTML. Native failures fall back to DuckDuckGo.
func WebSearchTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "websearch",
			Description: `Search the web and return titled links with snippets.

Uses the current model's native search when available (Anthropic web_search, Gemini google_search, OpenAI/xAI Responses web_search), otherwise DuckDuckGo. Prefer webfetch on a specific URL when you already have one.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"query": llm.Object{
						"type":        "string",
						"description": "Search query. Example: golang bufio Scanner MaxScanTokenSize",
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
		Run: runWebSearch,
	}
}

func runWebSearch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("websearch args: %w", err)
	}
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return tooldef.Result{}, errors.New("websearch: empty query")
	}
	if len([]rune(q)) > maxQueryRun {
		q = string([]rune(q)[:maxQueryRun])
	}
	text, err := doSearch(ctx, q, tooldef.Model(ctx))
	if err != nil {
		return tooldef.Result{}, err
	}
	return tooldef.Result{Content: text, Output: text, Detail: q}, nil
}

func doSearch(ctx context.Context, query string, cfg llm.ModelConfig) (string, error) {
	backend := nativeBackend(cfg)
	if backend != "" {
		text, err := nativeSearch(ctx, backend, query, cfg)
		if err == nil {
			return text, nil
		}
		ddg, ddgErr := ddgSearch(ctx, query)
		if ddgErr != nil {
			return "", fmt.Errorf("websearch: native %s: %w; ddg: %w", backend, err, ddgErr)
		}
		return fmt.Sprintf("Native search failed (%s), used DuckDuckGo.\n\n%s", trimErr(err, 80), ddg), nil
	}
	return ddgSearch(ctx, query)
}

func trimErr(err error, n int) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}
