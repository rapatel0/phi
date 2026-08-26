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
	budget Budget
	led    ledger
}

func (*Plugin) Name() string { return "mediaguard" }

func (p *Plugin) Register(h *ext.Host) error {
	p.budget = DefaultBudget()

	h.OnBeforeProviderRequest("mediaguard", func(_ context.Context, req hooks.ProviderRequest) ([]llm.Message, error) {
		out, d := Apply(req.Messages, p.budget)
		p.led.record(d)
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
		if d, seen := p.led.snapshot(); seen && d.Applied() {
			return "media:" + d.Summary()
		}
		return ""
	})
	return nil
}

func (p *Plugin) run(_ context.Context, _ []string) (hooks.CommandResult, error) {
	d, seen := p.led.snapshot()
	if !seen {
		return hooks.CommandResult{Toast: "No model request has been made yet."}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Media budget: %s / %d images\nLast request: %s",
		humanBytes(p.budget.MaxBytes), p.budget.MaxImages, d.Summary())
	if d.Applied() {
		b.WriteString("\nTrimmed media stays in the session; only the request was reduced.")
	}
	return hooks.CommandResult{Toast: b.String()}, nil
}
