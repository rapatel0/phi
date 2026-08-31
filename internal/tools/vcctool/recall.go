package vcctool

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/session"
	"github.com/rapatel0/alpha/internal/session/compaction"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

// EntriesFunc returns the current session path for recall.
type EntriesFunc func() []session.MessageEntry

// Tool searches raw session history after compaction.
func Tool(entries EntriesFunc) tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "vcc_recall",
			Description: `Search raw session history after compaction.

Use a phrase to find matching messages. Use #N to expand hit N from a prior search.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"query": llm.Object{
						"type":        "string",
						"description": "Text to find, or #N to expand that history index.",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": "Max hits (default 10).",
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
		Run: func(_ context.Context, input json.RawMessage) (tooldef.Result, error) {
			var in struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return tooldef.Result{}, err
			}
			query := strings.TrimSpace(in.Query)
			if query == "" {
				return tooldef.Result{}, nil
			}
			var list []session.MessageEntry
			if entries != nil {
				list = entries()
			}
			body := compaction.FormatHits(compaction.SearchHistory(list, query, in.Limit))
			return tooldef.Result{Content: body, Detail: query, Output: body}, nil
		},
	}
}
