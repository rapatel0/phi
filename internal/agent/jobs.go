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
	modelFn func() llm.ModelConfig,
	hooksFn func() *hooks.Manager,
	authFn func() string,
	hub ChildHub,
) (*job.Manager, error) {
	if root == "" {
		return nil, errors.New("agent: jobs root is required")
	}
	return job.New(job.Options{
		Root: root,
		Runner: EngineRunner{
			Model:   model,
			ModelFn: modelFn,
			HooksFn: hooksFn,
			AuthFn:  authFn,
			Hub:     hub,
		},
	})
}
