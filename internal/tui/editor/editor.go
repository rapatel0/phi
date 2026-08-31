// Package editor wires the TUI root widget and assembles domain panes.
package editor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/app"
	"github.com/rapatel0/alpha/internal/components/palette"
	"github.com/rapatel0/alpha/internal/components/toast"
	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/session"
	"github.com/rapatel0/alpha/internal/tui/childview"
	"github.com/rapatel0/alpha/internal/tui/commands"
	"github.com/rapatel0/alpha/internal/tui/composer"
	"github.com/rapatel0/alpha/internal/tui/controller"
	"github.com/rapatel0/alpha/internal/tui/footer"
	"github.com/rapatel0/alpha/internal/tui/overlays"
	"github.com/rapatel0/alpha/internal/tui/pathutil"
	"github.com/rapatel0/alpha/internal/tui/submit"
	"github.com/rapatel0/alpha/internal/tui/tasks"
	"github.com/rapatel0/alpha/internal/tui/transcript"
	"github.com/rapatel0/alpha/internal/util/update"
	"github.com/rapatel0/alpha/internal/version"
)

// Editor is the TUI root widget: layout composition and the UI-goroutine
// message loop. Cross-component work goes through controller.Bus — producers Publish,
// Draw drains and Update applies. Agent lifecycle lives in controller.Controller;
// session→widget projection lives in TranscriptPane (Mapper/SubagentStore).
// Sub-agent attach swaps the main transcript/composer onto that child's engine.
//
// Construction: cmd assembles App, controller.Bus, controller.Controller, CommandRegistry and passes
// them into NewEditor. Editor does not create controller.Controller or fetch the project singleton.
type Editor struct {
	vx    *xui.XUI
	App   *app.App
	theme components.Theme
	bus   *controller.Bus
	cwd   string

	transcript *transcript.TranscriptPane
	composer   *composer.ComposerPane
	footer     *footer.FooterChrome
	overlays   *overlays.Overlays
	toast      toast.Toast
	tasks      *tasks.Pane
	child      *childview.View

	ctrl *controller.Controller

	parentSnap  session.Snapshot
	parentStore *transcript.SubagentStore
	parentMsgs  []controller.Msg

	commands   *commands.CommandRegistry
	modelNames []string
	skillPath  string

	sessions *commands.SessionCommands
	hookCmds *commands.HookCommands

	recentJobs  []job.Info
	lastLiveN   int
	tasksLoaded bool
	submitter   *submit.Submitter
}

// NewEditor builds the TUI panes and wires injected collaborators.
// application, bus, and ctrl must be non-nil. registry may be nil (builtins used).
func NewEditor(
	application *app.App,
	bus *controller.Bus,
	ctrl *controller.Controller,
	registry *commands.CommandRegistry,
	vx *xui.XUI,
	theme components.Theme,
	cwd, model, skillPath string,
	contextWindow int,
	modelNames []string,
) *Editor {
	if registry == nil {
		registry = commands.NewBuiltinRegistry()
	}
	e := &Editor{
		vx:         vx,
		App:        application,
		theme:      theme,
		cwd:        cwd,
		bus:        bus,
		ctrl:       ctrl,
		modelNames: append([]string(nil), modelNames...),
		skillPath:  skillPath,
		commands:   registry,
		toast:      toast.Toast{Theme: theme},
		composer:   composer.NewComposerPane(theme, model, cwd),
		footer:     footer.NewFooterChrome(theme, contextWindow),
	}
	e.transcript = transcript.NewTranscriptPane(theme, e.footer.Spinner(), "Alpha "+version.Version)
	e.transcript.SetUsageCallback(e.footer.UpdateTokenDisplay)
	e.tasks = &tasks.Pane{Theme: theme, OnOpen: e.viewChild, OnSelect: e.followChild}
	e.transcript.SetOnOpenJob(e.viewChild)
	e.footer.BindComposer(e.composer)
	e.footer.SetLabelContext(e.transcript.Snapshot)
	e.footer.SetProfile(func() string {
		if e.ctrl != nil {
			return e.ctrl.Profile()
		}
		return ""
	})
	e.footer.SetLiveJobs(func() int {
		if e.ctrl != nil {
			return e.ctrl.LiveJobCount()
		}
		return 0
	})
	e.overlays = overlays.NewOverlays(
		theme,
		e.footer.Activity(),
		e.composer,
		func() {
			if e.App != nil {
				e.App.RequestFocus(e)
			}
		},
		func() {
			if e.App != nil {
				e.composer.FocusChat()
			}
		},
	)
	e.transcript.SetCopyHandlers(
		func(text string) bool {
			return e.vx != nil && e.vx.CopyToClipboard(text) == nil
		},
		func(msg string, kind toast.ToastKind, d time.Duration) {
			e.toast.Show(msg, kind, d)
		},
	)
	e.hookCmds = &commands.HookCommands{
		Registry: e.commands,
		Ctrl:     e.ctrl,
		CWD:      e.cwd,
		Composer: e.composer,
		Footer:   e.footer,
		Toast:    &e.toast,
		Publish:  e.Publish,
	}
	e.sessions = commands.NewSessionCommands(
		e.ctrl,
		e.transcript,
		e.footer,
		&e.toast,
		e.hookCmds.Sync,
	)
	e.sessions.OnAbandonAttach = e.abandonAttach
	e.sessions.OnSessionChange = e.invalidateTaskCache
	e.sessions.ShowPicker = e.showSessionPicker
	e.composer.SetSessionOpener(e.sessions.Show)

	var bridge *commandBridge
	bashRunner := submit.NewBashRunner(
		e.transcript,
		e.composer,
		func(msg string, kind toast.ToastKind, d time.Duration) {
			e.toast.Show(msg, kind, d)
		},
		e.Publish,
	)
	e.submitter = submit.NewSubmitter(
		e.ctrl,
		e.commands,
		e.transcript,
		e.footer.Activity(),
		e.composer,
		bashRunner,
		func() commands.CommandContext {
			if bridge == nil {
				return commands.CommandContext{}
			}
			return bridge.context()
		},
		e.Publish,
		e.overlays.PermissionActive,
		e.overlays.ContinueActive,
		e.overlays.ResolvePermission,
		e.overlays.ResolveContinue,
	)
	e.hookCmds.Submitter = e.submitter
	bridge = newCommandBridge(
		&e.toast,
		e.composer,
		e.transcript,
		e.ctrl,
		e.submitter,
		e.sessions,
		e.reloadHooks,
		e.listHooks,
		e.setModel,
		e.applyTheme,
		e.setPermissions,
		e.setAgents,
		e.addPendingSkill,
		e.copyLastMessage,
		e.modelNames,
		e.skillPath,
	)
	bridge.cwd = e.cwd
	e.hookCmds.CommandCtx = bridge.context
	e.composer.SetSkillPath(e.skillPath)
	e.composer.Wire(
		e.transcript,
		e.submitter,
		e.commands,
		e.cwd,
		e.Publish,
		e.drainBus,
		func() {
			if e.vx != nil {
				e.vx.QueueRefresh()
			}
		},
		e.overlays.BlocksComposer,
		e.overlays.HandlePermissionKey,
		e.overlays.HandleContinueKey,
		e.overlays.HandleQuestionKey,
		e.handleCopyKey,
		func() {
			if e.App != nil {
				e.App.RequestFocus(e)
			}
		},
		func(w components.Widget) {
			if e.App != nil {
				e.App.RequestFocus(w)
			}
		},
		func() {
			if e.ctrl != nil {
				e.ctrl.Close()
			}
		},
	)
	e.composer.SetOnIdleEscape(e.idleDetach)
	e.composer.SetOnNotice(func(msg string, kind toast.ToastKind) {
		e.toast.Show(msg, kind, 3*time.Second)
	})

	e.hookCmds.Sync()
	return e
}

// Publish sends a message onto the bus from any goroutine / widget callback.
func (e *Editor) Publish(m controller.Msg) {
	if e.bus == nil {
		return
	}
	e.bus.Publish(m)
}

// Update applies one message on the UI goroutine.
func (e *Editor) Update(m controller.Msg) {
	switch msg := m.(type) {
	case controller.SubmitMsg:
		e.submitter.Submit(msg.Text)
	case controller.CancelStreamMsg:
		e.submitter.Cancel()
	case controller.MentionResultsMsg:
		e.composer.ApplyMentionResults(msg)
	case controller.PermissionAskMsg, controller.PermissionDismissMsg,
		controller.ContinueAskMsg, controller.ContinueDismissMsg,
		controller.QuestionAskMsg, controller.QuestionDismissMsg:
		e.overlays.Apply(m)
	case controller.SetActivityMsg, controller.ClearIfActivityMsg, controller.UpdateAvailableMsg:
		e.footer.Apply(m)
	case controller.HookSessionEffectsMsg:
		e.footer.Apply(m)
		if msg.Toast != "" {
			e.toast.Show(msg.Toast, toast.ToastSuccess, 3*time.Second)
		}
	case controller.BranchLabelMsg:
		e.composer.SetBranchLabel(msg.Text)
		if e.vx != nil {
			e.vx.QueueRefresh()
		}
	case controller.HookCommandResultMsg:
		if e.hookCmds != nil {
			e.hookCmds.Apply(msg)
		}
	case controller.JobProgressMsg:
		// Applied in drainBus so we can skip Sync when the tree is unchanged.
	case controller.RedrawMsg:
		// no state change; drain already requested redraw
	}
}

func (e *Editor) drainBus() {
	batch := e.bus.Drain()
	if len(batch) == 0 {
		return
	}
	atBottom := e.transcript.AtBottom()
	agentEvent := false
	attached := ""
	if e.ctrl != nil {
		attached = e.ctrl.AttachedID()
	}
	for _, m := range batch {
		switch msg := m.(type) {
		case controller.SessionEventMsg:
			if e.child != nil && msg.JobID == e.child.JobID() {
				e.child.Apply(msg.Event)
				continue
			}
			if attached != "" {
				if msg.JobID != attached {
					// Only parent (empty JobID) events replay after detach.
					// Sibling child text must not land on the parent transcript.
					if msg.JobID == "" {
						e.parentMsgs = append(e.parentMsgs, m)
					}
					continue
				}
			} else if msg.JobID != "" {
				continue
			}
			agentEvent = true
			e.transcript.ApplySession(msg.Event)
		case controller.JobProgressMsg:
			if attached != "" {
				e.parentMsgs = append(e.parentMsgs, m)
				continue
			}
			if e.transcript.ApplyJobProgress(msg.Progress) {
				agentEvent = true
			}
		case controller.SetActivityMsg:
			if attached != "" {
				continue
			}
			e.Update(m)
		default:
			e.Update(m)
		}
	}
	if agentEvent {
		e.transcript.Sync()
		e.footer.SyncFromSnap(e.transcript.Snapshot())
		e.refreshTasks()
		if atBottom {
			e.transcript.StickToBottom()
		}
	}
}

func (e *Editor) Handle(ctx *components.EventContext, ev xui.Event) {
	if e.child != nil {
		if ke, ok := ev.(xui.KeyEvent); ok && ke.CtrlC() {
			e.composer.Handle(ctx, ev)
			return
		}
		keep, steer := e.child.Handle(ctx, ev)
		if steer {
			id := e.child.JobID()
			e.closeChildView()
			e.attachChild(id)
			return
		}
		if !keep {
			e.closeChildView()
			ctx.ConsumeAndRedraw()
			return
		}
		if ctx.Consume {
			return
		}
		// Unhandled keys (Ctrl+B/K, typing, …) fall through to the parent UI.
	}
	if ke, ok := ev.(xui.KeyEvent); ok && ke.Press {
		km := components.Keys
		if km.Hit(ke, km.AgentTree) || km.Hit(ke, km.AgentTreeT) {
			if e.tasks != nil {
				e.tasks.Toggle()
			}
			ctx.ConsumeAndRedraw()
			return
		}
		if km.Hit(ke, km.ChildEnter) {
			if id := e.peekJobID(); id != "" {
				e.viewChild(id)
			}
			ctx.ConsumeAndRedraw()
			return
		}
		if km.Hit(ke, km.ChildView) {
			if id := e.peekJobID(); id == "" {
				e.toast.Show("No sub-agent jobs", toast.ToastWarning, 2*time.Second)
			} else {
				e.viewChild(id)
			}
			ctx.ConsumeAndRedraw()
			return
		}
		if km.Hit(ke, km.ChildSteer) {
			if id := e.peekJobID(); id == "" {
				e.toast.Show("No sub-agent jobs", toast.ToastWarning, 2*time.Second)
			} else {
				e.attachChild(id)
			}
			ctx.ConsumeAndRedraw()
			return
		}
	}
	if e.tasks != nil && e.tasks.Visible {
		if e.tasks.Handle(ctx, ev) {
			return
		}
	}
	e.composer.Handle(ctx, ev)
}

func (e *Editor) followChild(jobID string) {
	if e == nil || e.child == nil {
		return
	}
	if strings.TrimSpace(jobID) == "" || e.child.JobID() == jobID {
		return
	}
	e.showChild(jobID, false)
}

func (e *Editor) viewChild(jobID string) {
	e.showChild(jobID, true)
}

func (e *Editor) showChild(jobID string, toggle bool) {
	if e == nil || e.ctrl == nil || strings.TrimSpace(jobID) == "" {
		return
	}
	if e.ctrl.AttachedID() != "" {
		e.toast.Show("Already steering a sub-agent — esc first", toast.ToastWarning, 2*time.Second)
		return
	}
	if e.child != nil && e.child.JobID() == jobID {
		if toggle {
			e.closeChildView()
		}
		return
	}
	snap, info, err := e.ctrl.ChildSnapshot(jobID)
	if err != nil {
		e.toast.Show(err.Error(), toast.ToastWarning, 3*time.Second)
		return
	}
	e.child = childview.Open(e.theme, info, snap, e.footer.Spinner())
	if e.footer != nil {
		e.footer.SetAttachHint("esc close · " + components.Keys.Hint(components.Keys.ChildSteer) + " steer")
	}
	e.requestRedraw()
}

func (e *Editor) closeChildView() {
	if e == nil {
		return
	}
	e.child = nil
	if e.ctrl != nil && e.ctrl.AttachedID() == "" && e.footer != nil {
		e.footer.SetAttachHint("")
	}
}

func (e *Editor) peekJobID() string {
	if e.tasks != nil {
		if id := e.tasks.SelectedID(); id != "" {
			return id
		}
	}
	if e.ctrl == nil {
		return ""
	}
	if live := e.ctrl.LiveJobs(); len(live) > 0 {
		return live[0].ID
	}
	recent, _ := e.ctrl.ListJobs(context.Background())
	if len(recent) > 0 {
		return recent[0].ID
	}
	return ""
}

func (e *Editor) attachChild(jobID string) {
	if e == nil || e.ctrl == nil || strings.TrimSpace(jobID) == "" {
		return
	}
	e.closeChildView()
	if e.ctrl.AttachedID() == jobID {
		return
	}
	if e.ctrl.AttachedID() != "" {
		e.restoreParent()
	}
	e.freezeParent()
	snap, info, err := e.ctrl.Attach(jobID)
	if err != nil {
		e.restoreParent()
		e.toast.Show(err.Error(), toast.ToastWarning, 3*time.Second)
		return
	}
	e.transcript.LoadReplay(snap)
	e.transcript.Sync()
	e.transcript.StickToBottom()
	if e.footer != nil {
		e.footer.SyncFromSnap(snap)
	}
	e.setAttachChrome(info)
	e.toast.Show("Steering sub-agent · esc parent", toast.ToastSuccess, 2*time.Second)
	e.requestRedraw()
}

func (e *Editor) idleDetach() bool {
	if e == nil || e.ctrl == nil || e.ctrl.AttachedID() == "" {
		return false
	}
	e.restoreParent()
	e.toast.Show("Back to parent", toast.ToastSuccess, 2*time.Second)
	return true
}

func (e *Editor) abandonAttach() {
	if e == nil {
		return
	}
	e.closeChildView()
	if e.ctrl != nil {
		e.ctrl.Detach()
	}
	e.parentSnap = session.Snapshot{}
	e.parentStore = nil
	e.parentMsgs = nil
	e.setAttachChrome(job.Info{})
}

func (e *Editor) freezeParent() {
	if e == nil || e.transcript == nil {
		return
	}
	e.parentSnap = e.transcript.Snapshot()
	e.parentStore = e.transcript.TakeSubagents()
	e.parentMsgs = nil
}

func (e *Editor) restoreParent() {
	if e == nil {
		return
	}
	if e.ctrl != nil {
		e.ctrl.Detach()
	}
	if e.transcript != nil {
		e.transcript.LoadReplay(e.parentSnap)
		e.transcript.RestoreSubagents(e.parentStore)
		for _, m := range e.parentMsgs {
			switch msg := m.(type) {
			case controller.SessionEventMsg:
				e.transcript.ApplySession(msg.Event)
			case controller.JobProgressMsg:
				e.transcript.ApplyJobProgress(msg.Progress)
			}
		}
		e.transcript.Sync()
		e.transcript.StickToBottom()
		if e.footer != nil {
			e.footer.SyncFromSnap(e.transcript.Snapshot())
		}
	}
	e.parentSnap = session.Snapshot{}
	e.parentStore = nil
	e.parentMsgs = nil
	e.setAttachChrome(job.Info{})
}

func (e *Editor) setAttachChrome(info job.Info) {
	label := attachChromeLabel(info)
	if e.composer != nil {
		e.composer.SetAttachLabel(label)
	}
	if e.footer != nil {
		if info.ID == "" {
			e.footer.SetAttachHint("")
		} else {
			e.footer.SetAttachHint("esc parent")
		}
	}
	if e.tasks != nil {
		e.tasks.Attached = info.ID
	}
}

func attachChromeLabel(info job.Info) string {
	if info.ID == "" {
		return ""
	}
	title := strings.TrimSpace(info.Description)
	if title == "" {
		title = string(info.Role)
	}
	if title == "" {
		title = info.ID
		if len(title) > 8 {
			title = title[:8]
		}
	}
	if info.Role != "" && title != string(info.Role) {
		return "↳ " + string(info.Role) + " · " + title
	}
	return "↳ " + title
}

func (e *Editor) invalidateTaskCache() {
	if e == nil {
		return
	}
	e.tasksLoaded = false
}

func (e *Editor) reloadRecentJobs() {
	if e == nil || e.ctrl == nil {
		return
	}
	recent, _ := e.ctrl.ListJobs(context.Background())
	e.recentJobs = recent
}

func (e *Editor) jobInfoCached(id string) (job.Info, bool) {
	if e == nil || e.ctrl == nil || id == "" {
		return job.Info{}, false
	}
	for _, inf := range e.ctrl.LiveJobs() {
		if inf.ID == id {
			return inf, true
		}
	}
	for _, inf := range e.recentJobs {
		if inf.ID == id {
			return inf, true
		}
	}
	return job.Info{}, false
}

func (e *Editor) refreshTasks() {
	if e == nil || e.tasks == nil || e.ctrl == nil {
		return
	}
	live := e.ctrl.LiveJobs()
	n := len(live)
	if !e.tasksLoaded || n != e.lastLiveN {
		e.reloadRecentJobs()
		e.tasksLoaded = true
		e.lastLiveN = n
	}
	e.tasks.SetJobs(live, e.recentJobs)
	e.tasks.Attached = e.ctrl.AttachedID()
}

func (e *Editor) handleCopyKey(ctx *components.EventContext, ke xui.KeyEvent) bool {
	return e.transcript.HandleCopyKey(ctx, ke)
}

// Draw renders the editor surface for the given draw context.
func (e *Editor) Draw(ctx components.DrawContext) components.Surface {
	e.drainBus()

	if e.footer != nil {
		e.footer.AdvanceTick()
	}
	_ = e.toast.Visible()

	maxSize := ctx.Max
	root := components.Surface{Size: maxSize, Widget: e}

	footerH := 1
	var chatH int
	if askH, overlay := e.overlays.PreferredBottomHeight(maxSize.Width, ctx.Method); overlay {
		chatH = askH
		maxChatH := maxSize.Height - footerH - 3
		if chatH > maxChatH {
			chatH = maxChatH
		}
		if chatH < 8 {
			chatH = 8
		}
	} else {
		chatH = e.composer.PreferredHeight(maxSize.Width, ctx.Method)
		minChatH := 5
		if len(e.composer.Chat.PendingSkills) > 0 {
			minChatH++
		}
		if len(e.composer.Chat.PendingImages) > 0 {
			minChatH++
		}
		if chatH < minChatH {
			chatH = minChatH
		}
		maxChatH := maxSize.Height - footerH - 3
		maxChatH = max(maxChatH, minChatH)
		if chatH > maxChatH {
			chatH = maxChatH
		}
	}
	listH := maxSize.Height - chatH - footerH
	if listH < 3 {
		listH = 3
		chatH = maxSize.Height - listH - footerH
		chatH = max(chatH, 5)
	}

	e.refreshTasks()
	sideW := 0
	if e.tasks != nil {
		sideW = e.tasks.Width()
	}
	listW := maxSize.Width - sideW
	if listW < 20 {
		listW = maxSize.Width
		sideW = 0
	}

	listSurf := e.transcript.Draw(ctx, listW, listH)
	listH = e.transcript.ListHeight()
	if e.child != nil {
		if info, ok := e.jobInfoCached(e.child.JobID()); ok {
			e.child.SetInfo(info)
		}
		listSurf = e.child.Draw(ctx, listW, listH)
	}

	var chatSurf components.Surface
	if surf, ok := e.overlays.DrawBottom(ctx, maxSize.Width, chatH); ok {
		chatSurf = surf
	} else {
		chatSurf = e.composer.DrawChat(ctx, maxSize.Width, chatH)
	}
	footerSurf := e.footer.Draw(ctx, maxSize.Width)

	root.Children = []components.SubSurface{
		{Origin: components.Point{X: 0, Y: 0}, Surface: listSurf},
		{Origin: components.Point{X: 0, Y: listH}, Surface: chatSurf, Z: 1},
		{Origin: components.Point{X: 0, Y: maxSize.Height - footerH}, Surface: footerSurf, Z: 2},
	}
	if sideW > 0 && e.tasks != nil {
		side := e.tasks.Draw(ctx, listH)
		e.tasks.SetFrame(listW, 0, sideW, listH)
		root.Children = append(root.Children, components.SubSurface{
			Origin:  components.Point{X: listW, Y: 0},
			Surface: side,
			Z:       1,
		})
	}
	if !e.overlays.Active() {
		root.Children = append(root.Children, e.composer.PickerOverlays(ctx, listH, maxSize.Width)...)
	}
	if pal, ok := e.composer.PaletteOverlay(ctx); ok {
		root.Children = append(root.Children, pal)
	}
	if sess, ok := e.composer.SessionOverlay(ctx); ok {
		root.Children = append(root.Children, sess)
	}
	if e.toast.Visible() {
		toastSurf := e.toast.Draw(ctx)
		root.Children = append(root.Children, components.SubSurface{
			Origin:  components.Point{X: 0, Y: 0},
			Surface: toastSurf,
			Z:       40,
		})
	}
	return root
}

func (e *Editor) requestRedraw() {
	if e.App != nil {
		e.App.RequestRedraw()
	}
}

// RequestRedraw asks the app to repaint (safe to bind onto controller.RedrawRelay / controller.Bus).
func (e *Editor) RequestRedraw() {
	e.requestRedraw()
}

// NeedsAnim is true while a spinner should move. Idle frames skip the 60fps
// redraw so the caret does not blink from hide/show cursor each tick.
func (e *Editor) NeedsAnim() bool {
	return e != nil && e.footer != nil && e.footer.ShowSpinner()
}

func (e *Editor) addPendingSkill(name string) {
	e.composer.AddPendingSkill(name)
	if e.vx != nil {
		e.vx.QueueRefresh()
	}
}

// StartUpdateCheck queries GitHub for a newer release in the background and
// surfaces a footer hint when one is available. cacheDir is where the version
// check may store its cache (e.g. project global root); empty disables disk cache.
func (e *Editor) StartUpdateCheck(cacheDir string) {
	ch := update.CheckAsync(update.CheckOptions{
		Current:  version.Version,
		CacheDir: cacheDir,
	})
	go func() {
		info, ok := <-ch
		if !ok || !info.Available {
			return
		}
		e.Publish(controller.UpdateAvailableMsg{Latest: info.Latest, Current: info.Current})
	}()
}

// StartBranchWatch hot-reloads the git branch in the path label when the
// repo HEAD changes (checkout from another terminal, editor, …). Polling
// HEAD is a file read; the git process only runs after a real switch.
func (e *Editor) StartBranchWatch() {
	if e.cwd == "" {
		return
	}
	stop := make(chan struct{}) // lives for the process; Close is process exit
	go (&branchWatch{dir: e.cwd, interval: branchPollInterval}).run(stop, func(label string) {
		e.Publish(controller.BranchLabelMsg{Text: label})
	})
}

func (e *Editor) applyTheme(name string) {
	th, ok := components.ThemeByName(name)
	if !ok {
		return
	}
	e.theme = th
	e.composer.SetTheme(th)
	e.toast.Theme = th
	e.transcript.SetTheme(th)
	e.footer.SetTheme(th)
	e.overlays.SetTheme(th)
	e.toast.Show("Theme: "+name, toast.ToastSuccess, 2*time.Second)
	if e.vx != nil {
		e.vx.QueueRefresh()
	}
}

func (e *Editor) setModel(name string) {
	if err := e.ctrl.SetModel(name); err != nil {
		e.toast.Show(err.Error(), toast.ToastError, 3*time.Second)
		return
	}
	e.composer.SetModelLabel(name)
	e.toast.Show("Model: "+name, toast.ToastSuccess, 2*time.Second)
	if e.vx != nil {
		e.vx.QueueRefresh()
	}
}

func (e *Editor) setPermissions(bypass bool) {
	e.ctrl.SetAllowAll(bypass)
	kind := toast.ToastWarning
	msg := "Permissions: on (ask)"
	if bypass {
		kind = toast.ToastSuccess
		msg = "Permissions: off (allow all)"
	}
	e.toast.Show(msg, kind, 3*time.Second)
}

func (e *Editor) setAgents(enabled bool) {
	e.ctrl.SetAgentsEnabled(enabled)
	msg := "Sub-agents: off"
	if enabled {
		msg = "Sub-agents: on"
	}
	e.toast.Show(msg, toast.ToastSuccess, 2*time.Second)
}

func (e *Editor) reloadHooks() {
	n, warns, err := e.ctrl.ReloadHooks()
	if err != nil {
		e.toast.Show("Hooks reload: "+err.Error(), toast.ToastError, 3*time.Second)
		return
	}
	e.hookCmds.Sync()
	msg := fmt.Sprintf("Hooks: reloaded %d", n)
	if len(warns) > 0 {
		msg = fmt.Sprintf("Hooks: reloaded %d (%d warning(s))", n, len(warns))
		e.toast.Show(msg, toast.ToastWarning, 3*time.Second)
		return
	}
	e.toast.Show(msg, toast.ToastSuccess, 2*time.Second)
}

func (e *Editor) listHooks() []palette.PaletteCommand {
	found, warns, err := e.ctrl.ListHooks()
	return commands.HookListEntries(found, warns, err)
}

func (e *Editor) copyLastMessage() {
	e.transcript.CopyBlock(e.transcript.LastCopyText())
}

// showSessionPicker opens the session tree dialog. currentDir marks which
// project the TUI is running in so it can be expanded and labeled.
func (e *Editor) showSessionPicker(projects []session.ProjectSessions, currentDir string) {
	e.composer.ShowSessionPicker(
		commands.SessionPickerProjects(projects, currentDir),
		func(file string) {
			// Resume by file path, so a session from another project opens
			// without switching the session directory.
			e.sessions.Resume(file)
		},
	)
}

// SubmitPrompt publishes a user prompt onto the bus.
func (e *Editor) SubmitPrompt(text string) {
	e.Publish(controller.SubmitMsg{Text: text})
}

type commandBridge struct {
	toast      *toast.Toast
	composer   *composer.ComposerPane
	transcript *transcript.TranscriptPane
	ctrl       *controller.Controller
	submitter  *submit.Submitter
	sessions   *commands.SessionCommands

	reloadHooks     func()
	listHooks       func() []palette.PaletteCommand
	setModel        func(string)
	applyTheme      func(string)
	setPermissions  func(bool)
	setAgents       func(bool)
	addSkill        func(string)
	copyLastMessage func()

	modelNames []string
	skillPath  string
	cwd        string
}

func newCommandBridge(
	toast *toast.Toast,
	composer *composer.ComposerPane,
	transcript *transcript.TranscriptPane,
	ctrl *controller.Controller,
	submitter *submit.Submitter,
	sessions *commands.SessionCommands,
	reloadHooks func(),
	listHooks func() []palette.PaletteCommand,
	setModel func(string),
	applyTheme func(string),
	setPermissions func(bool),
	setAgents func(bool),
	addSkill func(string),
	copyLastMessage func(),
	modelNames []string,
	skillPath string,
) *commandBridge {
	return &commandBridge{
		toast:           toast,
		composer:        composer,
		transcript:      transcript,
		ctrl:            ctrl,
		submitter:       submitter,
		sessions:        sessions,
		reloadHooks:     reloadHooks,
		listHooks:       listHooks,
		setModel:        setModel,
		applyTheme:      applyTheme,
		setPermissions:  setPermissions,
		setAgents:       setAgents,
		addSkill:        addSkill,
		copyLastMessage: copyLastMessage,
		modelNames:      append([]string(nil), modelNames...),
		skillPath:       skillPath,
	}
}

// setProfile switches the credential set the session uses.
//
// A refusal is reported and nothing changes: the usual cause is a profile that
// is not logged in to the model in use.
func (b *commandBridge) setProfile(name string) error {
	if b == nil || b.ctrl == nil {
		return errors.New("agent not configured")
	}
	if err := b.ctrl.SetProfile(name); err != nil {
		b.toast.Show(err.Error(), toast.ToastError, 8*time.Second)
		return err
	}
	b.toast.Show("Profile: "+name, toast.ToastSuccess, 3*time.Second)
	return nil
}

func (b *commandBridge) context() commands.CommandContext {
	if b == nil {
		return commands.CommandContext{}
	}
	return commands.CommandContext{
		Toast: func(msg string, kind toast.ToastKind, d time.Duration) {
			b.toast.Show(msg, kind, d)
		},
		PushSubmenu: func(title string, cmds []palette.PaletteCommand) {
			b.composer.PushPalette(title, cmds)
		},
		ShowSessions:  b.sessions.Show,
		ResumeSession: b.sessions.Resume,
		ClearSession: func() {
			if b.submitter != nil && b.submitter.StreamActive() {
				b.toast.Show("Cannot clear while a reply or command is running", toast.ToastWarning, 3*time.Second)
				return
			}
			b.sessions.Clear()
		},
		SetModel: b.setModel,
		Profile: func() string {
			if b.ctrl == nil {
				return ""
			}
			return b.ctrl.Profile()
		},
		Profiles: func() []string {
			if b.ctrl == nil {
				return nil
			}
			return b.ctrl.Profiles()
		},
		SetProfile: b.setProfile,
		CreateProfile: func(name string) error {
			if b.ctrl == nil {
				return errors.New("agent not configured")
			}
			return b.ctrl.CreateProfile(name)
		},
		Prefill: func(text string) {
			if b.composer != nil {
				b.composer.Prefill(text)
			}
		},
		ListModels: func() []string {
			if b.ctrl == nil {
				return b.modelNames
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			names := b.ctrl.RefreshModelCatalog(ctx)
			if len(names) > 0 {
				b.modelNames = names
			}
			return b.modelNames
		},
		ApplyTheme:     b.applyTheme,
		SetPermissions: b.setPermissions,
		SetAgents:      b.setAgents,
		ReloadHooks:    b.reloadHooks,
		ListHooks:      b.listHooks,
		AddSkill:       b.addSkill,
		PasteImage: func() {
			if b.composer != nil {
				b.composer.AttachClipboard()
			}
		},
		AttachImagePath: func(path string) {
			if b.composer != nil {
				b.composer.AttachPath(path)
			}
		},
		CopyLastMessage: b.copyLastMessage,
		ModelNames:      b.modelNames,
		SkillPath:       b.skillPath,
		Cwd:             b.cwd,
	}
}

const branchPollInterval = time.Second

type branchWatch struct {
	dir      string
	interval time.Duration
}

func (b *branchWatch) run(stop <-chan struct{}, publish func(label string)) {
	if b.interval <= 0 {
		b.interval = branchPollInterval
	}
	last := branchState(b.dir)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		if cur := branchState(b.dir); cur != last {
			last = cur
			publish(pathutil.PathWithBranch(b.dir))
		}
	}
}

func branchState(dir string) string {
	gitDir := resolveGitDir(dir)
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "missing"
	}
	return strings.TrimSpace(string(data))
}

func resolveGitDir(dir string) string {
	dotGit := filepath.Join(dir, ".git")
	if data, err := os.ReadFile(dotGit); err == nil {
		if target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:"); ok {
			target = strings.TrimSpace(target)
			if !filepath.IsAbs(target) {
				target = filepath.Join(dir, target)
			}
			return target
		}
	}
	return dotGit
}
