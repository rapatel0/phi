package agent

import (
	"github.com/pulseaiclub/phi/internal/job"
	"github.com/pulseaiclub/phi/internal/session"
)

// ChildHub is optional TUI wiring for live sub-agent attach.
//
// EngineRunner calls these from the job goroutine. Implementations must be
// non-blocking (or bounded) — a stuck Hub stalls the child loop.
type ChildHub interface {
	BindChild(meta job.Meta, eng *Engine)
	FinishChild(jobID string)
	EmitChild(jobID string, ev session.Event)
}
