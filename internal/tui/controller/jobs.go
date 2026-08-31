package controller

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/session"
)

func (c *Controller) startJobProgress() {
	if c.jobs == nil || c.bus == nil {
		return
	}
	ch, cancel := c.jobs.Subscribe()
	c.unsubJobs = cancel
	go func() {
		for p := range ch {
			if c.shouldPublishJobProgress(p) {
				c.publish(JobProgressMsg{Progress: p})
			}
		}
	}()
}

// shouldPublishJobProgress drops duplicate progress for the same child tool
// slot (same status/detail/name). Status transitions and new children still publish.
func (c *Controller) shouldPublishJobProgress(p job.Progress) bool {
	key := p.JobID + "\x00" + p.ToolUseID
	if p.ToolUseID == "" {
		key = p.JobID + "\x00" + p.Name + "\x00" + p.Detail
	}
	sig := p.Status + "\x00" + p.Name + "\x00" + p.Detail
	if prev, ok := c.lastJobProgress.Load(key); ok && prev.(string) == sig {
		return false
	}
	c.lastJobProgress.Store(key, sig)
	return true
}

// engineJobs returns the job manager only when sub-agents are enabled.
func (c *Controller) engineJobs() *job.Manager {
	if c == nil || !c.agentsEnabled.Load() {
		return nil
	}
	return c.jobs
}

func (c *Controller) LiveJobCount() int {
	if c == nil || c.jobs == nil {
		return 0
	}
	return c.jobs.LiveCount()
}

// ListJobs returns jobs for the current session, newest first.
// Other sessions' jobs stay on disk; they appear again on resume.
func (c *Controller) ListJobs(ctx context.Context) ([]job.Info, error) {
	if c == nil || c.jobs == nil {
		return nil, nil
	}
	return c.jobs.ListForParent(ctx, c.SessionID())
}

// LiveJobs returns in-process sub-agent jobs for the current session.
func (c *Controller) LiveJobs() []job.Info {
	if c == nil || c.jobs == nil {
		return nil
	}
	return job.ForParent(c.jobs.Live(), c.SessionID())
}

// ChildSnapshot loads a sub-agent's persisted session as a UI snapshot.
func (c *Controller) ChildSnapshot(jobID string) (session.Snapshot, job.Info, error) {
	var info job.Info
	if c == nil || c.jobs == nil {
		return session.Snapshot{}, info, errors.New("jobs disabled")
	}
	info, err := c.jobs.Get(context.Background(), jobID)
	if err != nil {
		return session.Snapshot{}, info, err
	}
	if slot := c.children.get(info.ID); slot != nil {
		if snap := slot.snapshot(); len(snap.Messages) > 0 {
			return snap, info, nil
		}
		if slot.engine != nil && slot.engine.Session() != nil {
			return session.SnapshotFromEntries(slot.engine.Session().PathEntries()), info, nil
		}
	}
	sessDir := filepath.Join(info.Dir, "session")
	list, err := session.ListSessions(sessDir)
	if err != nil {
		return session.Snapshot{}, info, err
	}
	if len(list) == 0 {
		return session.Snapshot{}, info, nil
	}
	mgr, err := session.OpenSession(list[0].File)
	if err != nil {
		return session.Snapshot{}, info, err
	}
	return session.SnapshotFromEntries(mgr.BuildContext()), info, nil
}

// startSide runs a /btw side conversation as a sub-agent job.
//
// The child is a normal job, so its transcript lands under ~/.alpha/jobs and
// the existing Ctrl+O popup can view it. Only the summary comes back, which is
// what keeps the side thread out of the main agent's context.
func (c *Controller) startSide(ctx context.Context, req ext.SideRequest) (ext.SideResult, error) {
	mgr := c.engineJobs()
	if mgr == nil {
		return ext.SideResult{}, errSideUnavailable
	}

	prompt := req.Prompt
	if req.Inherit {
		// The child has no access to the parent transcript, so the caller
		// must carry any needed context in the prompt itself.
		prompt = "Context from the main conversation follows the question.\n\n" + prompt
	}

	// An unknown role is a caller mistake. Fail rather than silently give
	// the child a different tool set than it asked for.
	role, err := job.ParseRole(req.Role)
	if err != nil {
		return ext.SideResult{}, err
	}

	info, err := mgr.Spawn(ctx, job.SpawnRequest{
		Prompt:      prompt,
		Description: "btw side conversation",
		ParentID:    c.SessionID(),
		Role:        role,
		WorkDir:     c.cwd,
	})
	if err != nil {
		return ext.SideResult{}, err
	}

	res, err := mgr.Wait(ctx, info.ID)
	if err != nil {
		// The job keeps running; the caller just stopped waiting for it.
		return ext.SideResult{JobID: info.ID}, err
	}
	return ext.SideResult{JobID: info.ID, Summary: res.Summary}, nil
}

var errSideUnavailable = errors.New("sub-agents are disabled, so /btw has nowhere to run")
