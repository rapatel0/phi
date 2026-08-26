package mediaguard

import (
	"context"
	"fmt"
	"strings"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/llm"
)

func init() { ext.Register(&Plugin{}) }

// Plugin applies an aggregate media budget to every model request.
type Plugin struct {
	led ledger
}

func (*Plugin) Name() string { return "mediaguard" }

func (p *Plugin) Register(h *ext.Host) error {
	h.OnBeforeProviderRequest("mediaguard", func(_ context.Context, req hooks.ProviderRequest) ([]llm.Message, error) {
		// The budget is chosen per request because the provider can change
		// between turns: the user can switch models mid-session, and the
		// documented limits differ by more than a factor of three.
		b := BudgetFor(req.Provider)
		out, d := Apply(req.Messages, b)
		p.led.record(d, b, req.Provider)
		return out, nil
	})

	h.RegisterCommand(ext.Command{
		Name:        "media",
		Description: "Show the media budget for the last request",
		Run:         p.run,
	})

	// Only speak up when the budget actually trimmed something: a footer
	// slot spent on "no media" is a footer slot wasted.
	h.AddFooter(func() string {
		if d, _, _, seen := p.led.snapshot(); seen && d.Applied() {
			return "media:" + d.Summary()
		}
		return ""
	})
	return nil
}

func (p *Plugin) run(_ context.Context, _ []string) (hooks.CommandResult, error) {
	d, budget, provider, seen := p.led.snapshot()
	if !seen {
		return hooks.CommandResult{Toast: "No model request has been made yet."}, nil
	}
	if provider == "" {
		provider = "unknown provider"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Media budget (%s): %s raw, about %s encoded / %d images\nLast request: %s",
		provider, humanBytes(budget.MaxBytes), humanBytes(budget.EncodedBytes()),
		budget.MaxImages, d.Summary())
	if d.Applied() {
		b.WriteString("\nTrimmed media stays in the session; only the request was reduced.")
	}
	return hooks.CommandResult{Toast: b.String()}, nil
}
