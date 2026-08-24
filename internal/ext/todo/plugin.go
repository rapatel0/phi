// Package todo is the rpiv-todo analog: a model-owned checklist that survives
// compaction (stored in-process, not in the LLM transcript).
package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/pulseaiclub/phi/internal/ext"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/tools"
)

func init() { ext.Register(Plugin{}) }

// Plugin registers todo_write.
type Plugin struct{}

func (Plugin) Name() string { return "todo" }

type item struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"` // pending | in_progress | completed
}

var (
	mu    sync.Mutex
	items []item
)

func (Plugin) Register(h *ext.Host) error {
	h.RegisterTool(tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        "todo_write",
			Description: "Replace the session todo list. Use for multi-step work. Status: pending | in_progress | completed.",
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"todos": llm.Object{
						"type": "array",
						"items": llm.Object{
							"type": "object",
							"properties": llm.Object{
								"id":   llm.Object{"type": "string"},
								"text": llm.Object{"type": "string"},
								"status": llm.Object{
									"type": "string",
									"enum": []string{"pending", "in_progress", "completed"},
								},
							},
							"required": []string{"id", "text", "status"},
						},
					},
				},
				Required: []string{"todos"},
			},
		},
		DetailFromArgs: func(raw json.RawMessage) string {
			var in struct {
				Todos []item `json:"todos"`
			}
			_ = json.Unmarshal(raw, &in)
			return fmt.Sprintf("%d items", len(in.Todos))
		},
		Run: func(_ context.Context, raw json.RawMessage) (tools.Result, error) {
			var in struct {
				Todos []item `json:"todos"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return tools.Result{}, err
			}
			mu.Lock()
			items = in.Todos
			n := len(items)
			done := 0
			for _, it := range items {
				if it.Status == "completed" {
					done++
				}
			}
			mu.Unlock()
			body := mustJSON(map[string]any{"ok": true, "count": n, "completed": done})
			return tools.Result{Content: body, Detail: fmt.Sprintf("%d/%d done", done, n), Output: body}, nil
		},
	})
	h.AddFooter(func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(items) == 0 {
			return ""
		}
		done := 0
		for _, it := range items {
			if it.Status == "completed" {
				done++
			}
		}
		return fmt.Sprintf("%d/%d todos", done, len(items))
	})
	return nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Snapshot is for tests.
func Snapshot() []item {
	mu.Lock()
	defer mu.Unlock()
	return append([]item(nil), items...)
}

func reset() {
	mu.Lock()
	items = nil
	mu.Unlock()
}

// Labels joins todo texts (tests / debug).
func Labels() string {
	mu.Lock()
	defer mu.Unlock()
	var b strings.Builder
	for i, it := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(it.Text)
	}
	return b.String()
}
