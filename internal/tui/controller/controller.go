package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rapatel0/alpha/internal/agent"
	"github.com/rapatel0/alpha/internal/debuglog"
	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/mcp"
	"github.com/rapatel0/alpha/internal/permission"
	"github.com/rapatel0/alpha/internal/project"
	"github.com/rapatel0/alpha/internal/session"
)

// Controller owns agent.Engine lifecycle and stream cancellation.
// It talks to the UI only by publishing Msg values onto the Bus.
//
// Construction: NewController(bus, proj, cwd). Callers (cmd) assemble
// collaborators; Controller does not call project.GetDefaultProject.
type Controller struct {
	engine *agent.Engine
	proj   *project.Project

	streamMu     sync.Mutex
	streamCancel context.CancelFunc
	streamGen    int
	lastUsage    hooks.SessionUsage // usage of the last completed turn (streamMu)

	bus *Bus

	sessionDir string
	cwd        string
	modelCfg   llm.ModelConfig
	jobs       *job.Manager
	unsubJobs  func()

	gate          permission.Gate
	askTimeoutSec int
	allowAll      atomic.Bool // session-wide allow-all for this process
	agentsEnabled atomic.Bool // when false, agent_* tools are not registered
	hooksManager  atomic.Pointer[hooks.Manager]
	mcpPool       *mcp.Pool

	// lastJobProgress dedupes identical Progress publishes (key → signature).
	lastJobProgress sync.Map

	children     *childRegistry
	attachedID   string   // guarded by streamMu; empty = parent focused
	attachedInfo job.Info // guarded by streamMu

	modelListMu sync.Mutex
	modelList   []string
	modelListAt time.Time
}

// NewController wires bus + project into a ready Controller with a live Engine.
// proj must be non-nil (typically already LoadConfig'd by cmd). On failure it
// returns (nil, err) — never a half-initialized Controller.
func NewController(bus *Bus, proj *project.Project, cwd string) (*Controller, error) {
	if bus == nil {
		return nil, errors.New("tui: nil bus")
	}
	if proj == nil {
		return nil, errors.New("tui: nil project")
	}
	if strings.TrimSpace(cwd) == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("tui: getwd: %w", err)
		}
	}

	if err := proj.LoadConfig(); err != nil {
		return nil, err
	}

	c := &Controller{
		bus:           bus,
		proj:          proj,
		cwd:           cwd,
		sessionDir:    proj.SessionDir(),
		askTimeoutSec: 120,
		modelCfg:      proj.Config().Model(),
	}
	// Default: no permission prompts. Toggle via command palette → settings → permissions.
	c.allowAll.Store(true)

	config := proj.Config()

	c.initGate(config.Permissions)
	c.agentsEnabled.Store(config.Agents.Enabled)

	hooksManager := loadHooksManager(proj)
	c.hooksManager.Store(hooksManager)

	c.children = newChildRegistry()
	jobs, err := agent.NewJobManager(proj.JobsDir(), c.modelCfg, func() llm.ModelConfig {
		return c.modelCfg
	}, c.Hooks, c.authFile, c)
	if err != nil {
		return nil, err
	}
	c.jobs = jobs

	if pool, err := mcp.LoadPool(proj.MCPConfigFile()); err != nil {
		debuglog.Logf("mcp: load: %v", err)
	} else {
		c.mcpPool = pool
	}

	eng, err := agent.NewEngine(agent.EngineOpts{
		Model: c.modelCfg,
		SessionOpts: agent.SessionOpts{
			Cwd:        cwd,
			SessionDir: c.sessionDir,
			Persist:    true,
		},
		Gate:        c.gate,
		Ask:         c.askPermission,
		ContinueAsk: c.askContinue,
		Jobs:        c.engineJobs(),
		Hooks:       hooksManager,
		MCP:         c.mcpPool,
		AuthFile:    proj.Global().AuthFile(),
	})
	if err != nil {
		return nil, err
	}
	c.engine = eng
	c.startJobProgress()
	ext.Default().SetQuestionAsker(c.askQuestion)
	ext.Default().SetSideChannel(c.startSide)
	ext.Default().SetWake(c.wake)
	ext.Default().SetCompact(c.compactNow)
	c.startBackgroundExtensions()
	c.emitSessionStart("startup", eng.SessionID(), "")
	return c, nil
}

func (c *Controller) askQuestion(ctx context.Context, q ext.Question) (ext.Answer, error) {
	reply := make(chan QuestionReply, 1)
	c.publish(QuestionAskMsg{Header: q.Header, Prompt: q.Prompt, Options: q.Options, Reply: reply})
	timeout := time.Duration(c.askTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-reply:
		return ext.Answer{Index: r.Index, Label: r.Label}, nil
	case <-ctx.Done():
		c.publish(QuestionDismissMsg{})
		return ext.Answer{Index: -1}, ctx.Err()
	case <-timer.C:
		c.publish(QuestionDismissMsg{})
		return ext.Answer{Index: -1}, nil
	}
}

func (c *Controller) initGate(policy permission.Policy) {
	if policy.AskTimeoutSec > 0 {
		c.askTimeoutSec = policy.AskTimeoutSec
	}
	if policy.Mode == "" {
		policy.Mode = permission.ModeInteractive
	}
	if policy.DangerouslyAllowAll {
		c.allowAll.Store(true)
	}
	// Do not clear allowAll when config omits dangerously_allow_all — TUI defaults
	// to bypass, and the palette toggle must survive SetModel / re-init.
	inner, err := permission.NewGate(policy, permission.WorkspaceRoot())
	if err != nil {
		inner, err = permission.NewGate(permission.DefaultPolicy(), permission.WorkspaceRoot())
		if err != nil {
			c.gate = &permission.BypassGate{Inner: permission.AllowAll{}, Enabled: &c.allowAll}
			return
		}
	}
	c.gate = &permission.BypassGate{Inner: inner, Enabled: &c.allowAll}
}

// AllowAll reports whether permission prompts are bypassed for this session.
func (c *Controller) AllowAll() bool {
	if c == nil {
		return true
	}
	return c.allowAll.Load()
}

// SetAllowAll enables or disables session-wide permission bypass.
func (c *Controller) SetAllowAll(v bool) {
	if c == nil {
		return
	}
	c.allowAll.Store(v)
}

// AgentsEnabled reports whether sub-agent tools are registered on the main engine.
func (c *Controller) AgentsEnabled() bool {
	if c == nil {
		return false
	}
	return c.agentsEnabled.Load()
}

// SetAgentsEnabled registers or removes agent_* tools for this session.
func (c *Controller) SetAgentsEnabled(v bool) {
	if c == nil {
		return
	}
	c.agentsEnabled.Store(v)
	if c.engine != nil {
		c.engine.SetJobs(c.engineJobs())
	}
}

// Hooks returns the currently loaded hooks manager (may be nil).
func (c *Controller) Hooks() *hooks.Manager {
	if c == nil {
		return nil
	}
	return c.hooksManager.Load()
}

// ReloadHooks re-discovers hooks from disk and swaps the manager on the engine
// (and on future sub-agents via Hooks()).
func (c *Controller) ReloadHooks() (loaded int, warns []hooks.Warning, err error) {
	if c == nil {
		return 0, nil, errors.New("controller not initialized")
	}
	mgr, loaded, warns, err := mergeHookEntries(c.proj)
	if err != nil {
		return 0, warns, err
	}
	hooks.LogWarnings(warns)
	c.hooksManager.Store(mgr)
	if c.engine != nil {
		c.engine.SetHooks(mgr)
	}
	return loaded, warns, nil
}

// ListHooks returns the current on-disk discovery (does not swap the manager).
func (c *Controller) ListHooks() ([]hooks.Discovered, []hooks.Warning, error) {
	if c == nil {
		return nil, nil, errors.New("controller not initialized")
	}
	proj := c.proj
	if proj == nil {
		return nil, nil, errors.New("project not available")
	}
	return hooks.DiscoverFrom(proj.UserHookDirs(), proj.ProjectHookDirs())
}

// loadHooksManager discovers ~/.agents/hooks and <cwd>/.agents/hooks,
// plus the older ~/.alpha/hooks trees, then appends compiled-in extensions.
// Without the extension merge, /btw and other slash commands never appear
// until the user reloads hooks.
// Load errors are non-fatal (fail-open: extensions still register).
func loadHooksManager(proj *project.Project) *hooks.Manager {
	if proj == nil {
		return nil
	}
	mgr, _, warns, err := mergeHookEntries(proj)
	if err != nil {
		debuglog.Logf("hooks: load failed: %v", err)
		hooks.LogWarnings(warns)
		return hooks.NewManager(ext.Default().HookEntries()...)
	}
	hooks.LogWarnings(warns)
	return mgr
}

// mergeHookEntries is the one path for startup, resume, and reload.
func mergeHookEntries(proj *project.Project) (*hooks.Manager, int, []hooks.Warning, error) {
	if proj == nil {
		return nil, 0, nil, errors.New("project not available")
	}
	found, warns, err := hooks.DiscoverFrom(proj.UserHookDirs(), proj.ProjectHookDirs())
	if err != nil {
		return nil, 0, warns, err
	}
	entries := hooks.EntriesFromDiscovered(found)
	entries = append(entries, ext.Default().HookEntries()...)
	return hooks.NewManager(entries...), len(found), warns, nil
}

// askPermission blocks until the confirmation UI answers.
func (c *Controller) askPermission(
	ctx context.Context,
	req permission.Request,
	reason string,
) (permission.AskResult, error) {
	if c.allowAll.Load() {
		return permission.AskResult{Approved: true}, nil
	}
	reply := make(chan AskReply, 1)
	c.publish(PermissionAskMsg{Request: req, Reason: reason, Reply: reply})

	timeout := time.Duration(c.askTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case r := <-reply:
		if r.AllowSession || r.AllowPersistent {
			c.allowAll.Store(true)
		}
		if r.AllowPersistent {
			if c.proj != nil {
				_ = project.SetDangerouslyAllowAll(c.proj.Global(), true)
			}
		}
		return permission.AskResult{Approved: r.Approved, Feedback: r.Feedback}, nil
	case <-ctx.Done():
		c.publish(PermissionDismissMsg{})
		return permission.AskResult{}, ctx.Err()
	case <-timer.C:
		c.publish(PermissionDismissMsg{})
		return permission.AskResult{}, nil
	}
}

// askContinue blocks until the user chooses to continue or stop after max rounds.
func (c *Controller) askContinue(ctx context.Context, maxRounds int) (bool, error) {
	reply := make(chan ContinueReply, 1)
	c.publish(ContinueAskMsg{MaxRounds: maxRounds, Reply: reply})

	timeout := time.Duration(c.askTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case r := <-reply:
		return r.Continue, nil
	case <-ctx.Done():
		c.publish(ContinueDismissMsg{})
		return false, ctx.Err()
	case <-timer.C:
		c.publish(ContinueDismissMsg{})
		return false, nil
	}
}

// StartPrompt cancels any in-flight stream and starts a new agent loop.
// When a sub-agent is attached, the prompt is queued onto that child instead.
func (c *Controller) StartPrompt(text string, pendingSkills []string, images []llm.Image) {
	if c.AttachedID() != "" {
		c.submitChild(text, pendingSkills, images)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.streamMu.Lock()
	if c.streamCancel != nil {
		c.streamCancel()
	}
	c.streamCancel = cancel
	c.streamGen++
	gen := c.streamGen
	c.streamMu.Unlock()

	go c.runLoop(ctx, gen, text, pendingSkills, images)
}

// Cancel aborts the current stream context (if any).
// When a sub-agent is attached, only that child's current turn is cancelled.
func (c *Controller) Cancel() {
	if c.AttachedID() != "" {
		c.cancelChild()
		return
	}
	c.streamMu.Lock()
	cancel := c.streamCancel
	c.streamMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Close cancels the stream and shuts down the job manager.
// wake starts a turn with text the user did not type.
//
// It refuses while a turn is already streaming, because two prompts in flight
// would interleave. A scheduled loop is told, so it can record the reason and
// try again on its next slot rather than assume it fired.
func (c *Controller) wake(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("wake: empty prompt")
	}
	if c.AttachedID() != "" {
		return errors.New("wake: a sub-agent is attached")
	}
	if c.children.anyFollow() {
		return errors.New("wake: a sub-agent is running a follow-up")
	}
	c.streamMu.Lock()
	streaming := c.streamCancel != nil
	c.streamMu.Unlock()
	if streaming {
		return errors.New("wake: a turn is already running")
	}
	c.publish(SubmitMsg{Text: text})
	return nil
}

func (c *Controller) compactNow(ctx context.Context) error {
	if c == nil || c.engine == nil {
		return errors.New("agent not configured")
	}
	return c.engine.CompactNow(ctx)
}

// startBackgroundExtensions hands the permission gate to extensions that own
// background work and lets them start.
//
// The gate goes first: an extension that runs commands must not run one before
// it can be judged.
func (c *Controller) startBackgroundExtensions() {
	for _, bg := range ext.Default().Backgrounds() {
		bg.SetGate(c.gate)
		bg.Start(context.Background())
	}
}

func (c *Controller) Close() {
	c.Detach()
	// Background work stops first, so a build cannot outlive the session
	// that started it.
	for _, bg := range ext.Default().Backgrounds() {
		bg.Stop()
	}
	if c.children != nil {
		c.children.cancelAll()
	}
	c.sessionShutdown("quit", c.SessionID())
	c.Cancel()
	if c.unsubJobs != nil {
		c.unsubJobs()
		c.unsubJobs = nil
	}
	if c.jobs != nil {
		_ = c.jobs.Close(context.Background())
	}
	if c.mcpPool != nil {
		_ = c.mcpPool.Close()
		c.mcpPool = nil
	}
}

// Alive reports whether the stream generation still matches gen.
func (c *Controller) Alive(gen int) bool {
	c.streamMu.Lock()
	ok := c.streamGen == gen
	c.streamMu.Unlock()
	return ok
}

func (c *Controller) waitOrDone(ctx context.Context, gen int, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
	}
	return c.Alive(gen)
}

func (c *Controller) publish(m Msg) {
	if c.bus != nil {
		c.bus.Publish(m)
	}
}

func (c *Controller) runLoop(
	ctx context.Context,
	gen int,
	prompt string,
	pendingSkills []string,
	images []llm.Image,
) string {
	defer c.clearStream(gen)
	var last string
	if !c.waitOrDone(ctx, gen, 120*time.Millisecond) {
		return ""
	}
	c.publish(SetActivityMsg{Activity: ActivityStreaming})

	if c.engine == nil {
		errText := "agent not configured"
		if !c.Alive(gen) {
			return ""
		}
		c.publish(SessionEventMsg{Event: session.AssistantMessageUpdate{Message: session.Message{
			ID:    fmt.Sprintf("assistant-error-%d", time.Now().UnixNano()),
			State: session.StateError,
			Text:  errText,
			Content: []session.ContentBlock{
				{Type: session.BlockText, Text: errText},
			},
		}}})
		return ""
	}

	for ev, err := range c.engine.Loop(ctx, prompt, agent.LoopOpts{PendingSkills: pendingSkills, Images: images}) {
		if !c.Alive(gen) {
			return last
		}
		if err != nil {
			errText := err.Error()
			c.publish(SessionEventMsg{Event: session.AssistantMessageUpdate{Message: session.Message{
				ID:    fmt.Sprintf("assistant-error-%d", time.Now().UnixNano()),
				State: session.StateError,
				Text:  errText,
				Content: []session.ContentBlock{
					{Type: session.BlockText, Text: errText},
				},
			}}})
			return last
		}
		if ev != nil {
			// Hook effects are not transcript content: they carry the toast
			// and status an engine-fired hook asked for, which the engine
			// cannot publish itself.
			if fx, ok := ev.(session.HookEffects); ok {
				c.publishSessionEffects(hooks.SessionOutcome{
					Toast:     fx.Toast,
					Status:    fx.Status,
					StatusSet: fx.StatusSet,
				})
				continue
			}
			c.publish(SessionEventMsg{Event: ev})
			if t := assistantTextFromEvent(ev); t != "" {
				last = t
			}
			if up, ok := ev.(session.AssistantMessageUpdate); ok && up.Message.State == session.StateComplete &&
				up.Message.Usage.Reported() {
				c.recordUsage(up.Message)
			}
		}
	}
	return last
}
