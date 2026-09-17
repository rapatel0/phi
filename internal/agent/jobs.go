package agent

import (
	"errors"

	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/llm"
)

// NewJobManager creates a process-level job manager whose runner drives child Engines.
// modelFn may be nil; then model is used as a fixed snapshot.
// hooksFn supplies hooks for child engines (may return nil); prefer a live
// getter so TUI reload updates sub-agents too.
// authFn supplies the credential store; prefer a live getter so a profile
// switch reaches sub-agents started afterwards.
// hub is optional (TUI live-attach); pass nil for headless runs.
func NewJobManager(
	root string,
	model llm.ModelConfig,
	modelFn func(job.Role) llm.ModelConfig,
	hooksFn func() *hooks.Manager,
	authFn func() string,
	hub ChildHub,
	maxDepth ...int,
) (*job.Manager, error) {
	if root == "" {
		return nil, errors.New("agent: jobs root is required")
	}
	depth := 3
	if len(maxDepth) > 0 && maxDepth[0] > 0 {
		depth = maxDepth[0]
	}
	runner := &EngineRunner{
		Model:   model,
		ModelFn: modelFn,
		HooksFn: hooksFn,
		AuthFn:  authFn,
		Hub:     hub,
	}
	mgr, err := job.New(job.Options{Root: root, MaxDepth: depth, Runner: runner})
	if err != nil {
		return nil, err
	}
	runner.Jobs = mgr
	return mgr, nil
}
