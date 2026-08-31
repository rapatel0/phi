package controller

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rapatel0/alpha/internal/agent"
	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/permission"
	"github.com/rapatel0/alpha/internal/session"
)

type queuedPrompt struct {
	text   string
	skills []string
	images []llm.Image
}

// childSlot is one sub-agent engine the TUI can attach to.
type childSlot struct {
	mu           sync.Mutex
	meta         job.Meta
	engine       *agent.Engine
	snap         session.Snapshot
	spawnLooping bool
	followGen    int
	followCancel context.CancelFunc
	inbox        []queuedPrompt
}

func (s *childSlot) apply(ev session.Event) {
	if s == nil || ev == nil {
		return
	}
	s.mu.Lock()
	s.snap = session.Apply(s.snap, ev)
	s.mu.Unlock()
}

func (s *childSlot) snapshot() session.Snapshot {
	if s == nil {
		return session.Snapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

func (s *childSlot) popInboxLocked() (queuedPrompt, bool) {
	if len(s.inbox) == 0 {
		return queuedPrompt{}, false
	}
	p := s.inbox[0]
	s.inbox = s.inbox[1:]
	return p, true
}

type childRegistry struct {
	mu   sync.Mutex
	byID map[string]*childSlot
}

func newChildRegistry() *childRegistry {
	return &childRegistry{byID: make(map[string]*childSlot)}
}

func (r *childRegistry) anyFollow() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.byID {
		s.mu.Lock()
		busy := s.followCancel != nil
		s.mu.Unlock()
		if busy {
			return true
		}
	}
	return false
}

func (r *childRegistry) get(id string) *childSlot {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id]
}

func (r *childRegistry) put(s *childSlot) {
	if r == nil || s == nil || s.meta.ID == "" {
		return
	}
	r.mu.Lock()
	r.byID[s.meta.ID] = s
	r.mu.Unlock()
}

func (r *childRegistry) cancelAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	slots := make([]*childSlot, 0, len(r.byID))
	for _, s := range r.byID {
		slots = append(slots, s)
	}
	r.mu.Unlock()
	for _, s := range slots {
		s.mu.Lock()
		cancel := s.followCancel
		s.followGen++
		s.followCancel = nil
		s.inbox = nil
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

// BindChild implements [agent.ChildHub].
func (c *Controller) BindChild(meta job.Meta, eng *agent.Engine) {
	if c == nil {
		return
	}
	if c.children == nil {
		c.children = newChildRegistry()
	}
	slot := c.children.get(meta.ID)
	if slot == nil {
		slot = &childSlot{}
	}
	slot.mu.Lock()
	slot.meta = meta
	slot.engine = eng
	slot.spawnLooping = true
	slot.mu.Unlock()
	c.children.put(slot)
}

// FinishChild implements [agent.ChildHub].
func (c *Controller) FinishChild(jobID string) {
	if c == nil {
		return
	}
	slot := c.children.get(jobID)
	if slot == nil {
		return
	}
	slot.mu.Lock()
	slot.spawnLooping = false
	if info, err := c.jobInfo(jobID); err == nil {
		slot.meta = info.Meta
	}
	next, ok := slot.popInboxLocked()
	slot.mu.Unlock()
	if ok {
		go c.runChildLoop(slot, next.text, next.skills, next.images)
	}
}

// EmitChild implements [agent.ChildHub].
func (c *Controller) EmitChild(jobID string, ev session.Event) {
	if c == nil || ev == nil {
		return
	}
	slot := c.children.get(jobID)
	if slot != nil {
		slot.apply(ev)
	}
	c.publish(SessionEventMsg{Event: ev, JobID: jobID})
}

// AttachedID is the job the TUI is currently talking to (empty = parent).
func (c *Controller) AttachedID() string {
	if c == nil {
		return ""
	}
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	return c.attachedID
}

// AttachedInfo is the attached job snapshot (zero Info if none).
func (c *Controller) AttachedInfo() job.Info {
	if c == nil {
		return job.Info{}
	}
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	return c.attachedInfo
}

// CanEnqueue reports whether composer submit should queue onto an attached child
// even if that child is currently streaming.
func (c *Controller) CanEnqueue() bool {
	return c.AttachedID() != ""
}

// Attach switches the focused conversation to a sub-agent.
// The parent engine keeps running; follow-up prompts go to the child.
func (c *Controller) Attach(jobID string) (session.Snapshot, job.Info, error) {
	if c == nil {
		return session.Snapshot{}, job.Info{}, errors.New("controller not initialized")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return session.Snapshot{}, job.Info{}, errors.New("empty job id")
	}
	info, err := c.jobInfo(jobID)
	if err != nil {
		return session.Snapshot{}, info, err
	}
	slot, err := c.ensureChild(info)
	if err != nil {
		return session.Snapshot{}, info, err
	}
	c.wireChildAsk(slot, true)
	c.streamMu.Lock()
	c.attachedID = jobID
	c.attachedInfo = info
	c.streamMu.Unlock()
	return slot.snapshot(), info, nil
}

// Detach returns focus to the parent engine. Child follow-ups already in
// flight keep running; new composer input goes to the parent.
func (c *Controller) Detach() {
	if c == nil {
		return
	}
	id := c.AttachedID()
	if id == "" {
		return
	}
	if slot := c.children.get(id); slot != nil {
		c.wireChildAsk(slot, false)
	}
	c.streamMu.Lock()
	c.attachedID = ""
	c.attachedInfo = job.Info{}
	c.streamMu.Unlock()
}

func (c *Controller) jobInfo(jobID string) (job.Info, error) {
	if c.jobs == nil {
		return job.Info{}, errors.New("jobs disabled")
	}
	return c.jobs.Get(context.Background(), jobID)
}

func (c *Controller) ensureChild(info job.Info) (*childSlot, error) {
	if c.children == nil {
		c.children = newChildRegistry()
	}
	if slot := c.children.get(info.ID); slot != nil {
		slot.mu.Lock()
		slot.meta = info.Meta
		eng := slot.engine
		spawning := slot.spawnLooping
		slot.mu.Unlock()
		if eng != nil || spawning {
			return slot, nil
		}
	}
	eng, err := c.openChildEngine(info)
	if err != nil {
		return nil, err
	}
	if existing := c.children.get(info.ID); existing != nil {
		existing.mu.Lock()
		if existing.engine != nil || existing.spawnLooping {
			existing.meta = info.Meta
			existing.mu.Unlock()
			return existing, nil
		}
		existing.engine = eng
		existing.meta = info.Meta
		if len(existing.snap.Messages) == 0 && eng.Session() != nil {
			existing.snap = session.SnapshotFromEntries(eng.Session().PathEntries())
		}
		existing.mu.Unlock()
		return existing, nil
	}
	slot := &childSlot{meta: info.Meta, engine: eng}
	if eng.Session() != nil {
		slot.snap = session.SnapshotFromEntries(eng.Session().PathEntries())
	}
	c.children.put(slot)
	return slot, nil
}

func (c *Controller) openChildEngine(info job.Info) (*agent.Engine, error) {
	cwd := info.WorkDir
	if cwd == "" {
		cwd = c.cwd
	}
	spec := agent.SpecForRole(info.Role)
	sessDir := filepath.Join(info.Dir, "session")
	opts := agent.SessionOpts{
		Cwd:        cwd,
		SessionDir: sessDir,
		Persist:    true,
		ParentID:   info.ParentID,
	}
	if list, err := session.ListSessions(sessDir); err == nil && len(list) > 0 {
		opts.ParentID = ""
		opts.ResumePath = list[0].File
	}
	cfg := c.modelCfg
	eng, err := agent.NewEngine(agent.EngineOpts{
		Model:       cfg,
		SessionOpts: opts,
		Gate:        c.childDetachGate(spec, cwd),
		Ask:         nil,
		ContinueAsk: nil,
		Tools:       c.childTools(info.ID, spec.Tools),
		Hooks:       c.Hooks(),
		AuthFile:    c.authFile(),
	})
	if err != nil {
		return nil, fmt.Errorf("open child engine: %w", err)
	}
	return eng, nil
}

func (c *Controller) childDetachGate(spec agent.ChildSpec, cwd string) permission.Gate {
	policy := permission.DefaultPolicy()
	policy.Mode = spec.Mode
	g, err := permission.NewGate(policy, cwd)
	if err != nil {
		return permission.AllowAll{}
	}
	return g
}

func (c *Controller) wireChildAsk(slot *childSlot, attached bool) {
	if slot == nil || slot.engine == nil {
		return
	}
	if attached {
		slot.engine.SetPermission(c.gate, c.askPermission)
		slot.engine.SetContinueAsk(c.askContinue)
		return
	}
	cwd := slot.meta.WorkDir
	if cwd == "" {
		cwd = c.cwd
	}
	spec := agent.SpecForRole(slot.meta.Role)
	slot.engine.SetPermission(c.childDetachGate(spec, cwd), nil)
	slot.engine.SetContinueAsk(nil)
}

func (c *Controller) submitChild(text string, skills []string, images []llm.Image) {
	id := c.AttachedID()
	if id == "" {
		return
	}
	info := c.AttachedInfo()
	slot, err := c.ensureChild(info)
	if err != nil {
		c.publish(SessionEventMsg{Event: session.AssistantMessageUpdate{Message: session.Message{
			ID:    fmt.Sprintf("assistant-error-%d", time.Now().UnixNano()),
			State: session.StateError,
			Text:  err.Error(),
			Content: []session.ContentBlock{
				{Type: session.BlockText, Text: err.Error()},
			},
		}}, JobID: id})
		return
	}
	slot.apply(session.UserAppend{Text: text, Images: imageLabels(images), ImageData: images})
	slot.mu.Lock()
	busy := slot.spawnLooping || slot.followCancel != nil
	if busy {
		slot.inbox = append(slot.inbox, queuedPrompt{text: text, skills: skills, images: images})
		slot.mu.Unlock()
		return
	}
	slot.mu.Unlock()
	go c.runChildLoop(slot, text, skills, images)
}

func (c *Controller) cancelChild() {
	id := c.AttachedID()
	if id == "" {
		return
	}
	slot := c.children.get(id)
	if slot == nil {
		return
	}
	slot.mu.Lock()
	spawning := slot.spawnLooping
	cancel := slot.followCancel
	slot.followGen++
	slot.followCancel = nil
	slot.inbox = nil
	slot.mu.Unlock()
	if spawning && c.jobs != nil {
		_ = c.jobs.Cancel(context.Background(), id)
	}
	if cancel != nil {
		cancel()
	}
}

func (c *Controller) runChildLoop(slot *childSlot, prompt string, pendingSkills []string, images []llm.Image) {
	if slot == nil || slot.engine == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	slot.mu.Lock()
	if slot.spawnLooping || slot.followCancel != nil {
		slot.inbox = append([]queuedPrompt{{text: prompt, skills: pendingSkills, images: images}}, slot.inbox...)
		slot.mu.Unlock()
		cancel()
		return
	}
	slot.followCancel = cancel
	slot.followGen++
	gen := slot.followGen
	jobID := slot.meta.ID
	slot.mu.Unlock()

	defer func() {
		cancel()
		slot.mu.Lock()
		if slot.followGen == gen {
			slot.followCancel = nil
		}
		next, ok := slot.popInboxLocked()
		slot.mu.Unlock()
		if ok {
			go c.runChildLoop(slot, next.text, next.skills, next.images)
		}
	}()

	for ev, err := range slot.engine.Loop(ctx, prompt, agent.LoopOpts{PendingSkills: pendingSkills, Images: images}) {
		slot.mu.Lock()
		live := slot.followGen == gen
		slot.mu.Unlock()
		if !live {
			return
		}
		if err != nil {
			errText := err.Error()
			ev = session.AssistantMessageUpdate{Message: session.Message{
				ID:    fmt.Sprintf("assistant-error-%d", time.Now().UnixNano()),
				State: session.StateError,
				Text:  errText,
				Content: []session.ContentBlock{
					{Type: session.BlockText, Text: errText},
				},
			}}
			c.EmitChild(jobID, ev)
			return
		}
		if ev != nil {
			c.EmitChild(jobID, ev)
		}
	}
}

func imageLabels(images []llm.Image) []string {
	if len(images) == 0 {
		return nil
	}
	out := make([]string, 0, len(images))
	for _, img := range images {
		out = append(out, img.Label())
	}
	return out
}
