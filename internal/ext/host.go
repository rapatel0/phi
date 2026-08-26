package ext

import (
	"context"
	"sync"
	"time"

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
	commands  []Command
	onSession []sessionSub
	onTool    []toolSub
	onPrompt  []PromptFunc
}

var defaultHost = NewHost()

// Default is the process-wide host. cmd wires it into the engine and TUI.
func Default() *Host { return defaultHost }

// NewHost returns an empty host (tests).
func NewHost() *Host { return &Host{} }

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
