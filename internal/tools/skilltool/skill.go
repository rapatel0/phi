package skilltool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/llm/skills"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

// SkillTool returns a tool that loads a SKILL.md by name and returns its body.
func SkillTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "skill",
			Description: `Load a skill's SKILL.md (instructions for a reusable workflow).

Call this when a listed skill matches the task, then follow that file. Do not invent a parallel workflow.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"name": llm.Object{
						"type":        "string",
						"description": "Skill name from the Available skills catalog. Example: review",
					},
				},
				Required: []string{"name"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Name)
		},
		Run: runSkill,
	}
}

func runSkill(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("skill args: %w", err)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return tooldef.Result{}, errors.New("skill: empty name")
	}
	list := loadAll(ctx)
	sk := skills.Find(list, name)
	if sk == nil {
		var names []string
		for _, s := range list {
			names = append(names, s.Name)
		}
		return tooldef.Result{}, fmt.Errorf("skill %q not found (have: %s)", name, strings.Join(names, ", "))
	}
	body := sk.Body
	if body == "" {
		body = sk.Description
	}
	text := fmt.Sprintf("# Skill: %s\nLocation: %s\n\n%s", sk.Name, sk.SkillFilePath, body)
	return tooldef.Result{Content: text, Output: text, Detail: sk.Name}, nil
}

// loadAll returns every skill in the search path. A directory that is missing
// or unreadable is skipped, so one bad entry cannot hide the rest.
func loadAll(ctx context.Context) []*skills.Skill {
	return skills.LoadDirs(skillDirs(ctx))
}

// skillDirs adapts the tool context to the shared search path, so the TUI
// picker and this tool always see the same catalog.
func skillDirs(ctx context.Context) []string {
	cwd, err := tooldef.Cwd(ctx)
	if err != nil {
		cwd = ""
	}
	return skills.SearchDirs(tooldef.Model(ctx).SkillPath, cwd)
}
