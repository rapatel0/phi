package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rapatel0/alpha/internal/agent"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/session"
)

// SessionID returns the short-form-friendly session id.
func (c *Controller) SessionID() string {
	if c.engine == nil {
		return ""
	}
	return c.engine.SessionID()
}

// SessionDir returns the directory where session JSONL files are stored.
func (c *Controller) SessionDir() string {
	if c == nil {
		return ""
	}
	return c.sessionDir
}

// BrowseSessions lists every project under the session base with its saved
// sessions, so the picker can offer sessions from other working directories.
func (c *Controller) BrowseSessions() ([]session.ProjectSessions, error) {
	if c == nil || c.proj == nil {
		return nil, errors.New("project not available")
	}
	return session.BrowseSessions(c.proj.Global().SessionBase())
}

// SessionFile returns the JSONL path when persisting.
func (c *Controller) SessionFile() string {
	if c.engine == nil {
		return ""
	}
	return c.engine.SessionFile()
}

// Resume loads a prior session by id (exact or unique prefix).
// On success the engine session is replaced; caller should refresh the UI transcript.
// If the resumed session cwd differs from the process cwd, cwdWarning is non-empty.
func (c *Controller) Resume(id string) (cwdWarning string, err error) {
	if c.sessionDir == "" {
		return "", errors.New("session directory not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		latest, err := session.LatestSessionID(c.sessionDir, c.SessionID())
		if err != nil {
			return "", err
		}
		id = latest
	}

	c.Detach()
	prevID := c.SessionID()
	if out := c.sessionBeforeSwitch("resume", prevID, id); out.Denied {
		c.publishSessionEffects(out)
		reason := out.Reason
		if reason == "" {
			reason = "session switch denied by hook"
		}
		return "", errors.New(reason)
	}

	c.Cancel()
	c.sessionShutdown("resume", prevID)

	cfg := c.modelCfg
	if cfg.Name == "" {
		if c.proj == nil {
			return "", errors.New("project not available")
		}
		if err := c.proj.LoadConfig(); err != nil {
			return "", err
		}
		cfg = c.proj.Config().Model()
	}

	mgr := loadHooksManager(c.proj)
	c.hooksManager.Store(mgr)
	eng, err := agent.NewEngine(agent.EngineOpts{
		Model: cfg,
		SessionOpts: agent.SessionOpts{
			Cwd:        c.cwd,
			SessionDir: c.sessionDir,
			Persist:    true,
			ResumeID:   id,
		},
		Gate:        c.gate,
		Ask:         c.askPermission,
		ContinueAsk: c.askContinue,
		Jobs:        c.engineJobs(),
		Hooks:       mgr,
		MCP:         c.mcpPool,
		AuthFile:    c.authFile(),
	})
	if err != nil {
		return "", err
	}
	if sessCwd := eng.SessionCwd(); sessCwd != "" && c.cwd != "" && sessCwd != c.cwd {
		cwdWarning = fmt.Sprintf("session cwd is %s (current %s); not changing directory", sessCwd, c.cwd)
	}
	c.engine = eng
	c.modelCfg = cfg
	c.resetUsage()
	c.emitSessionStart("resume", eng.SessionID(), prevID)
	return cwdWarning, nil
}

// Clear starts a brand-new persisted session (empty transcript, new id).
// Caller must ensure no agent stream / local bash is in flight.
func (c *Controller) Clear() error {
	if c.sessionDir == "" {
		return errors.New("session directory not configured")
	}

	c.Detach()
	prevID := c.SessionID()
	if out := c.sessionBeforeSwitch("new", prevID, ""); out.Denied {
		c.publishSessionEffects(out)
		reason := out.Reason
		if reason == "" {
			reason = "session switch denied by hook"
		}
		return errors.New(reason)
	}
	c.sessionShutdown("new", prevID)

	cfg := c.modelCfg
	if cfg.Name == "" {
		if c.proj == nil {
			return errors.New("project not available")
		}
		if err := c.proj.LoadConfig(); err != nil {
			return err
		}
		cfg = c.proj.Config().Model()
	}

	hooksMgr := c.Hooks()
	engine, err := agent.NewEngine(agent.EngineOpts{
		Model: cfg,
		SessionOpts: agent.SessionOpts{
			Cwd:        c.cwd,
			SessionDir: c.sessionDir,
			Persist:    true,
		},
		Gate:        c.gate,
		Ask:         c.askPermission,
		ContinueAsk: c.askContinue,
		Jobs:        c.engineJobs(),
		Hooks:       hooksMgr,
		MCP:         c.mcpPool,
		AuthFile:    c.authFile(),
	})
	if err != nil {
		return err
	}
	c.engine = engine
	c.modelCfg = cfg
	c.resetUsage()
	c.emitSessionStart("new", engine.SessionID(), prevID)
	return nil
}

// ReplaySnapshot builds a UI transcript snapshot from the engine session
// (user/assistant text; tool rows simplified away).
func (c *Controller) ReplaySnapshot() session.Snapshot {
	if c.engine == nil || c.engine.Session() == nil {
		return session.Snapshot{}
	}
	return session.SnapshotFromEntries(c.engine.Session().PathEntries())
}

func (c *Controller) sessionBeforeSwitch(reason, fromID, targetID string) hooks.SessionOutcome {
	mgr := c.Hooks()
	if mgr == nil {
		return hooks.SessionOutcome{}
	}
	return mgr.SessionBeforeSwitch(context.Background(), hooks.SessionEvent{
		SessionID:       fromID,
		Cwd:             c.cwd,
		Reason:          reason,
		TargetSessionID: targetID,
		Usage:           c.sessionUsage(),
	})
}

func (c *Controller) sessionShutdown(reason, sessionID string) {
	mgr := c.Hooks()
	if mgr == nil {
		return
	}
	out := mgr.SessionShutdown(context.Background(), hooks.SessionEvent{
		SessionID: sessionID,
		Cwd:       c.cwd,
		Reason:    reason,
		Usage:     c.sessionUsage(),
	})
	c.publishSessionEffects(out)
}

func (c *Controller) emitSessionStart(reason, sessionID, previousID string) {
	mgr := c.Hooks()
	if mgr == nil {
		return
	}
	out := mgr.SessionStart(context.Background(), hooks.SessionEvent{
		SessionID:         sessionID,
		Cwd:               c.cwd,
		Reason:            reason,
		PreviousSessionID: previousID,
		Usage:             c.sessionUsage(),
	})
	c.publishSessionEffects(out)
}

// sessionUsage returns the token usage of the last completed turn observed by
// this controller's run loop; zero when no turn has completed (or the provider
// never reported usage). Usage comes from the stream, not the session store, so
// a resumed session reports zero until its first turn finishes.
func (c *Controller) sessionUsage() hooks.SessionUsage {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	return c.lastUsage
}

// recordUsage snapshots the completed turn's usage for session lifecycle hooks
// and fires post_turn audit hooks (cache metrics, etc.).
func (c *Controller) recordUsage(m session.Message) {
	usage := hooks.SessionUsage{
		PromptTokens:     m.Usage.PromptTokens,
		CompletionTokens: m.Usage.CompletionTokens,
		CachedTokens:     m.Usage.CachedTokens,
		TotalTokens:      m.Usage.TotalTokens,
	}
	c.streamMu.Lock()
	c.lastUsage = usage
	c.streamMu.Unlock()

	mgr := c.Hooks()
	if mgr == nil {
		return
	}
	out := mgr.PostTurn(context.Background(), hooks.SessionEvent{
		SessionID: c.SessionID(),
		Cwd:       c.cwd,
		MessageID: m.ID,
		Usage:     usage,
	})
	c.publishSessionEffects(out)
}

// resetUsage clears captured usage when switching sessions so a new or resumed
// session does not inherit the previous one's counts.
func (c *Controller) resetUsage() {
	c.streamMu.Lock()
	c.lastUsage = hooks.SessionUsage{}
	c.streamMu.Unlock()
}

func (c *Controller) publishSessionEffects(out hooks.SessionOutcome) {
	if out.Toast == "" && !out.StatusSet {
		return
	}
	c.publish(HookSessionEffectsMsg{
		Toast:     out.Toast,
		Status:    out.Status,
		StatusSet: out.StatusSet,
	})
}
