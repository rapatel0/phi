package btw

import (
	"context"
	"strconv"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

func init() { ext.Register(&Plugin{}) }

// Plugin adds /btw, a side conversation that stays out of the main context.
type Plugin struct {
	thread Thread
	host   *ext.Host
}

func (*Plugin) Name() string { return "btw" }

func (p *Plugin) Register(h *ext.Host) error {
	p.host = h
	h.RegisterCommand(ext.Command{
		Name:        "btw",
		Description: "Ask a side question without spending main context. /btw list, /btw clear, --tangent",
		Run:         p.run,
	})
	h.AddFooter(func() string {
		if n := p.thread.Len(); n > 0 {
			return "btw:" + plural(n)
		}
		return ""
	})
	return nil
}

func (p *Plugin) run(ctx context.Context, args []string) (hooks.CommandResult, error) {
	a := ParseArgs(args)

	switch a.Sub {
	case "list":
		return hooks.CommandResult{Toast: Render(p.thread.Turns())}, nil
	case "clear":
		p.thread.reset()
		return hooks.CommandResult{Toast: "Side conversation cleared.", StatusSet: true}, nil
	}

	if a.Prompt == "" {
		return hooks.CommandResult{}, errNoPrompt
	}

	res, err := p.host.StartSide(ctx, ext.SideRequest{
		Prompt: a.Prompt,
		// A tangent deliberately starts clean; the default carries the
		// main thread as background.
		Inherit: !a.Tangent,
	})
	if err != nil {
		return hooks.CommandResult{}, err
	}

	p.thread.add(Turn{Prompt: a.Prompt, Summary: res.Summary, JobID: res.JobID})
	answer := res.Summary
	if answer == "" {
		answer = "The side conversation produced no summary. Open the job to see its transcript."
	}
	return hooks.CommandResult{Toast: answer}, nil
}

// plural renders the footer count.
func plural(n int) string {
	if n == 1 {
		return "1 aside"
	}
	return strconv.Itoa(n) + " asides"
}
