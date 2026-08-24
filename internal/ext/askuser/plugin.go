// Package askuser is the rpiv-ask-user-question analog.
package askuser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pulseaiclub/phi/internal/ext"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/tools"
)

func init() { ext.Register(Plugin{}) }

// Plugin registers ask_user_question.
type Plugin struct{}

func (Plugin) Name() string { return "askuser" }

func (Plugin) Register(h *ext.Host) error {
	h.RegisterTool(tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        "ask_user_question",
			Description: "Ask the user a multiple-choice question instead of guessing. Use when a preference, trade-off, or missing requirement would change the work.",
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"header": llm.Object{"type": "string", "description": "Short title"},
					"prompt": llm.Object{"type": "string", "description": "The question"},
					"options": llm.Object{
						"type":  "array",
						"items": llm.Object{"type": "string"},
					},
				},
				Required: []string{"prompt", "options"},
			},
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in struct {
				Prompt string `json:"prompt"`
			}
			_ = json.Unmarshal(raw, &in)
			return strings.TrimSpace(in.Prompt)
		},
		Run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			var in struct {
				Header  string   `json:"header"`
				Prompt  string   `json:"prompt"`
				Options []string `json:"options"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return tools.Result{}, err
			}
			if strings.TrimSpace(in.Prompt) == "" || len(in.Options) == 0 {
				return tools.Result{}, fmt.Errorf("ask_user_question: prompt and options are required")
			}
			ans, err := ext.Default().AskQuestion(ctx, ext.Question{
				Header:  in.Header,
				Prompt:  in.Prompt,
				Options: in.Options,
			})
			if err != nil {
				return tools.Result{}, err
			}
			if ans.Index < 0 {
				body := `{"cancelled":true}`
				return tools.Result{Content: body, Detail: "cancelled", Output: body}, nil
			}
			body, _ := json.Marshal(map[string]any{
				"index":  ans.Index,
				"label":  ans.Label,
				"prompt": in.Prompt,
			})
			return tools.Result{Content: string(body), Detail: ans.Label, Output: string(body)}, nil
		},
	})
	return nil
}
