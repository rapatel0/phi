package ext

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rapatel0/alpha/internal/permission"
	"github.com/rapatel0/alpha/internal/tools"
)

// Plugin is a compiled-in extension.
type Plugin interface {
	Name() string
	Register(*Host) error
}

// Question is a structured prompt the model can put to the user.
type Question struct {
	Header  string
	Prompt  string
	Options []string // empty → free text (not yet used)
}

// Answer is the user's choice.
type Answer struct {
	Index int // 0-based option; -1 if cancelled
	Label string
}

// QuestionFunc blocks until the user answers or ctx is done.
type QuestionFunc func(ctx context.Context, q Question) (Answer, error)

// Host is the surface extensions mutate at startup (and a few live hooks).
type Host struct {
	mu      sync.Mutex
	tools   []tools.Tool
	footer  []func() string
	ask     QuestionFunc
	onUsage []func(promptTok, completionTok int, elapsed time.Duration)
	names   []string

	// Slash commands and hook subscriptions. See api.go: these become
	// hooks.Entry values so extensions and discovered hooks share one manager.
	commands     []Command
	onSession    []sessionSub
	onTool       []toolSub
	onToolResult []resultSub
	onPrompt     []PromptFunc
	side         SideFunc
	wake         WakeFunc
	compact      CompactFunc
	background   []Background
}

// WakeFunc starts a turn with text the user did not type.
//
// A scheduled loop that fires while nobody is watching has to reach the agent
// somehow, and only the shell owns the input path. The TUI supplies this, the
// same way it supplies the side channel.
type WakeFunc func(text string) error

// CompactFunc runs one compaction pass.
type CompactFunc func(ctx context.Context) error

// SideRequest asks for one side conversation: a sub-agent run that does not
// enter the main thread's context.
type SideRequest struct {
	Prompt string
	// Inherit includes the main conversation as background. A tangent starts
	// clean, which is the point of asking something unrelated.
	Inherit bool
	// Role picks the child's tool set. Empty means the read-only default.
	Role string
}

// SideResult is what the side conversation produced.
type SideResult struct {
	JobID   string
	Summary string
}

// SideFunc runs a side conversation. The TUI supplies it.
type SideFunc func(ctx context.Context, req SideRequest) (SideResult, error)

var errNoSideChannel = errors.New(
	"side conversations need the interactive shell with sub-agents enabled")

var errNoWake = errors.New(
	"scheduled work needs the interactive shell: nothing is listening for a turn")

var errNoCompact = errors.New(
	"compact needs the interactive shell")

var defaultHost = NewHost()

// Default is the process-wide host. cmd wires it into the engine and TUI.
func Default() *Host { return defaultHost }

// NewHost returns an empty host (tests).
func NewHost() *Host { return &Host{} }

// Background is implemented by an extension that owns work outliving a turn.
//
// The shell drives it: nothing may run before the permission gate exists, and
// nothing may outlive the session. An extension cannot arrange either on its
// own, because it is registered at init and knows nothing about the shell.
type Background interface {
	// SetGate supplies the permission gate. Work that runs commands must
	// refuse until it arrives.
	SetGate(gate permission.Gate)
	// Start begins background work.
	Start(ctx context.Context)
	// Stop halts it and waits.
	Stop()
}

// Backgrounds returns the registered extensions that own background work.
func (h *Host) Backgrounds() []Background {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Background(nil), h.background...)
}

// Register adds p to the default host. Panics on error (init-time).
func Register(p Plugin) {
	if err := defaultHost.Add(p); err != nil {
		panic("ext: " + p.Name() + ": " + err.Error())
	}
}

// Add registers p on this host.
func (h *Host) Add(p Plugin) error {
	if h == nil || p == nil {
		return nil
	}
	if err := p.Register(h); err != nil {
		return err
	}
	h.mu.Lock()
	h.names = append(h.names, p.Name())
	if bg, ok := p.(Background); ok {
		h.background = append(h.background, bg)
	}
	h.mu.Unlock()
	return nil
}

// RegisterTool adds a model-callable tool.
func (h *Host) RegisterTool(t tools.Tool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tools = append(h.tools, t)
}

// Tools returns a copy of extension tools.
func (h *Host) Tools() []tools.Tool {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]tools.Tool(nil), h.tools...)
}

// AddFooter appends a lazy footer fragment (empty string = skip).
func (h *Host) AddFooter(fn func() string) {
	if h == nil || fn == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.footer = append(h.footer, fn)
}

// FooterBits concatenates non-empty footer fragments.
func (h *Host) FooterBits() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	fns := append([]func() string(nil), h.footer...)
	h.mu.Unlock()
	var out []string
	for _, fn := range fns {
		if s := fn(); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// OnUsage records a stream's token counts (for tok/s etc).
func (h *Host) OnUsage(fn func(promptTok, completionTok int, elapsed time.Duration)) {
	if h == nil || fn == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onUsage = append(h.onUsage, fn)
}

// EmitUsage notifies extensions of a completed turn's usage.
func (h *Host) EmitUsage(promptTok, completionTok int, elapsed time.Duration) {
	if h == nil {
		return
	}
	h.mu.Lock()
	fns := append([]func(int, int, time.Duration){}, h.onUsage...)
	h.mu.Unlock()
	for _, fn := range fns {
		fn(promptTok, completionTok, elapsed)
	}
}

// SetQuestionAsker installs the TUI/headless question prompt.
// SetSideChannel installs the side-conversation runner. The TUI provides it,
// because only the shell owns the job manager and the popup. Without it,
// StartSide reports that side conversations are unavailable, which is the
// correct answer in a headless run.
func (h *Host) SetSideChannel(fn SideFunc) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.side = fn
}

// SetWake installs the turn starter. The TUI provides it, because only the
// shell owns the input path. Without it, Wake reports that nothing is
// listening, which is the correct answer in a headless run: a loop that cannot
// reach the agent must say so rather than discard the work.
func (h *Host) SetWake(fn WakeFunc) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.wake = fn
}

// SetCompact installs the compaction runner. The TUI provides it.
func (h *Host) SetCompact(fn CompactFunc) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.compact = fn
}

// Compact runs one compaction pass.
func (h *Host) Compact(ctx context.Context) error {
	if h == nil {
		return errNoCompact
	}
	h.mu.Lock()
	fn := h.compact
	h.mu.Unlock()
	if fn == nil {
		return errNoCompact
	}
	return fn(ctx)
}

// Wake starts a turn with text the user did not type.
func (h *Host) Wake(text string) error {
	if h == nil {
		return errNoWake
	}
	h.mu.Lock()
	fn := h.wake
	h.mu.Unlock()
	if fn == nil {
		return errNoWake
	}
	return fn(text)
}

// StartSide runs a side conversation and returns its summary.
func (h *Host) StartSide(ctx context.Context, req SideRequest) (SideResult, error) {
	if h == nil {
		return SideResult{}, errNoSideChannel
	}
	h.mu.Lock()
	fn := h.side
	h.mu.Unlock()
	if fn == nil {
		return SideResult{}, errNoSideChannel
	}
	return fn(ctx, req)
}

func (h *Host) SetQuestionAsker(fn QuestionFunc) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ask = fn
}

// AskQuestion runs the installed asker. Cancelled/missing asker returns Index -1.
func (h *Host) AskQuestion(ctx context.Context, q Question) (Answer, error) {
	if h == nil {
		return Answer{Index: -1}, nil
	}
	h.mu.Lock()
	fn := h.ask
	h.mu.Unlock()
	if fn == nil {
		return Answer{Index: -1}, nil
	}
	return fn(ctx, q)
}

// Names lists registered plugin names (tests / /ext).
func (h *Host) Names() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.names...)
}
