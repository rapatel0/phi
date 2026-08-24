package skilltool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/llm/skills"
	"github.com/pulseaiclub/phi/internal/tools/tooldef"
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
	list, err := loadAll(ctx)
	if err != nil {
		return tooldef.Result{}, err
	}
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

func loadAll(ctx context.Context) ([]*skills.Skill, error) {
	var out []*skills.Skill
	seen := map[string]struct{}{}
	for _, dir := range skillDirs(ctx) {
		list, err := skills.LoadSkills(dir)
		if err != nil {
			return nil, err
		}
		for _, s := range list {
			key := strings.ToLower(s.Name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, s)
		}
	}
	return out, nil
}

func skillDirs(ctx context.Context) []string {
	var dirs []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		dirs = append(dirs, p)
	}
	if cfg := tooldef.Model(ctx); cfg.SkillPath != "" {
		add(cfg.SkillPath)
	}
	if v := os.Getenv("PHI_SKILL_PATH"); v != "" {
		add(v)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".phi", "skills"))
	}
	if cwd, err := tooldef.Cwd(ctx); err == nil && cwd != "" {
		add(filepath.Join(cwd, ".phi", "skills"))
	}
	return dirs
}
