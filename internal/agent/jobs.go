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
// hub is optional (TUI live-attach); pass nil for headless runs.
func NewJobManager(
	root string,
	model llm.ModelConfig,
	modelFn func() llm.ModelConfig,
	hooksFn func() *hooks.Manager,
	authFile string,
	hub ChildHub,
) (*job.Manager, error) {
	if root == "" {
		return nil, errors.New("agent: jobs root is required")
	}
	return job.New(job.Options{
		Root: root,
		Runner: EngineRunner{
			Model:    model,
			ModelFn:  modelFn,
			HooksFn:  hooksFn,
			AuthFile: authFile,
			Hub:      hub,
		},
	})
}
