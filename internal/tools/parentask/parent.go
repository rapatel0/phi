package parentask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

// AskFunc answers a child question. The parent agent may reply itself or
// prompt the user.
type AskFunc func(ctx context.Context, question string) (string, error)

// Tool is a child-only tool: ask the parent agent a blocking question.
func Tool(ask AskFunc) tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "ask_parent",
			Description: "Ask the parent agent a question when you are blocked, " +
				"need a preference, or need a decision outside your scope. " +
				"The parent answers, or asks the user if it cannot.",
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"question": llm.Object{
						"type":        "string",
						"description": "The question for the parent agent",
					},
				},
				Required: []string{"question"},
			},
			Readable: true,
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in struct {
				Question string `json:"question"`
			}
			_ = json.Unmarshal(raw, &in)
			return strings.TrimSpace(in.Question)
		},
		Run: func(ctx context.Context, raw json.RawMessage) (tooldef.Result, error) {
			var in struct {
				Question string `json:"question"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return tooldef.Result{}, err
			}
			q := strings.TrimSpace(in.Question)
			if q == "" {
				return tooldef.Result{}, errors.New("ask_parent: question is required")
			}
			if ask == nil {
				return tooldef.Result{}, errors.New("ask_parent: no parent is listening")
			}
			ans, err := ask(ctx, q)
			if err != nil {
				return tooldef.Result{}, err
			}
			ans = strings.TrimSpace(ans)
			if ans == "" {
				ans = "(parent returned an empty answer)"
			}
			return tooldef.Result{Content: ans, Detail: "parent replied", Output: ans}, nil
		},
	}
}
