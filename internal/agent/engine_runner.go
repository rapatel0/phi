package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/permission"
	"github.com/rapatel0/alpha/internal/session"
	"github.com/rapatel0/alpha/internal/tools"
	"github.com/rapatel0/alpha/internal/tools/parentask"
)

// EngineRunner runs a child [Engine.Loop] as a [job.Runner].
//
// Each Run creates a fresh Engine with a persisted session under
// <job.Dir>/session/, ParentID from the job, and no Ask handler.
// Child engines do not receive Jobs, so they have no agent_* tools.
// Role (explore|worker|review) selects tools and default permission mode
// when Gate/Tools are nil.
//
// Hooks (or HooksFn) are inherited from the parent so org policy applies to
// sub-agents the same way. HooksFn wins when set (live reload).
type EngineRunner struct {
	Model     llm.ModelConfig
	ModelFn   func() llm.ModelConfig // if set, preferred over Model
	Gate      permission.Gate        // nil → SpecForRole(job.Role).Mode on WorkDir
	Tools     []tools.Tool           // nil → SpecForRole(job.Role).Tools
	MaxRounds int                    // 0 → Engine default
	Hooks     *hooks.Manager         // shared with parent; nil = no hooks
	HooksFn   func() *hooks.Manager  // if set, preferred over Hooks
	AuthFile  string
	// AuthFn is preferred over AuthFile, so a profile switch reaches
	// sub-agents started later instead of leaving them on the old account.
	AuthFn func() string
	Hub    ChildHub // optional; TUI live-attach. nil in headless runs
}

// authFile prefers the live getter, falling back to the fixed path.
func (r EngineRunner) authFile() string {
	if r.AuthFn != nil {
		if p := r.AuthFn(); p != "" {
			return p
		}
	}
	return r.AuthFile
}

// Run implements [job.Runner].
func (r EngineRunner) Run(ctx context.Context, env job.RunEnv) (string, error) {
	if env.Job.Dir == "" {
		return "", errors.New("agent: EngineRunner requires job Dir")
	}

	cwd := env.Job.WorkDir
	if cwd == "" {
		cwd = "."
	}

	spec := SpecForRole(env.Job.Role)

	gate := r.Gate
	if gate == nil {
		policy := permission.DefaultPolicy()
		policy.Mode = spec.Mode
		g, err := permission.NewGate(policy, cwd)
		if err != nil {
			return "", err
		}
		gate = g
	}

	toolList := r.Tools
	if toolList == nil {
		toolList = spec.Tools
	}
	if asker, ok := r.Hub.(ParentAsker); ok && asker != nil {
		jobID := env.Job.ID
		ask := parentask.Tool(func(ctx context.Context, q string) (string, error) {
			return asker.AskParent(ctx, jobID, q)
		})
		toolList = append(append([]tools.Tool(nil), toolList...), ask)
	}

	model := r.Model
	if r.ModelFn != nil {
		model = r.ModelFn()
	}

	hookMgr := r.Hooks
	if r.HooksFn != nil {
		hookMgr = r.HooksFn()
	}

	sessionDir := filepath.Join(env.Job.Dir, "session")
	engine, err := NewEngine(EngineOpts{
		Model:     model,
		Gate:      gate,
		Ask:       nil,
		Tools:     toolList,
		MaxRounds: r.MaxRounds,
		Hooks:     hookMgr,
		AuthFile:  r.authFile(),
		SessionOpts: SessionOpts{
			Cwd:        cwd,
			SessionDir: sessionDir,
			Persist:    true,
			ParentID:   env.Job.ParentID,
		},
	})
	if err != nil {
		return "", err
	}

	if r.Hub != nil {
		r.Hub.BindChild(env.Job, engine)
		defer r.Hub.FinishChild(env.Job.ID)
	}

	env.Log(fmt.Sprintf("sub-agent role=%s session=%s parent=%s", spec.Role, engine.SessionID(), env.Job.ParentID))

	prompt := env.Job.Prompt
	if env.Job.Description != "" {
		prompt = env.Job.Description + "\n\n" + prompt
	}
	prompt = prompt + "\n\n" + spec.Hint

	if r.Hub != nil {
		r.Hub.EmitChild(env.Job.ID, session.UserAppend{Text: prompt})
	}

	var (
		lastText string
		lastErr  error
	)
	for ev, loopErr := range engine.Loop(ctx, prompt, LoopOpts{}) {
		if loopErr != nil {
			lastErr = loopErr
			env.Log("error: " + loopErr.Error())
			break
		}
		if r.Hub != nil && ev != nil {
			r.Hub.EmitChild(env.Job.ID, ev)
		}
		switch e := ev.(type) {
		case session.AssistantMessageUpdate:
			if e.Message.State == session.StateComplete {
				if t := strings.TrimSpace(e.Message.FlatText()); t != "" {
					lastText = t
				}
			}
		case session.ToolData:
			detail := e.Run.Detail
			if detail == "" {
				detail = e.Run.Name
			}
			env.Log(fmt.Sprintf("tool %s %s: %s", e.Run.Name, e.Run.Status, detail))
			if env.OnProgress != nil {
				env.OnProgress(job.Progress{
					ToolUseID: e.Run.ToolUseID,
					Name:      e.Run.Name,
					Status:    e.Run.Status.String(),
					Detail:    detail,
				})
			}
		}
	}

	if path := engine.SessionFile(); path != "" {
		env.Log("session_file=" + path)
	}

	if lastErr != nil {
		if lastText != "" {
			_ = env.WriteResult(lastText)
		}
		return lastText, lastErr
	}
	if ctx.Err() != nil {
		return lastText, ctx.Err()
	}
	if lastText == "" {
		lastText = "sub-agent finished with no text reply"
	}
	return lastText, nil
}
