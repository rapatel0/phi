// Package toolstats counts tool calls for the current session and reports them
// through /toolstats.
//
// It is also the reference user of the extension API: it registers a slash
// command, subscribes to session lifecycle events, and observes the tool loop,
// which is every method the API adds.
package toolstats

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

func init() { ext.Register(New()) }

// Plugin counts tool calls per tool name.
type Plugin struct {
	mu     sync.Mutex
	counts map[string]int
	errs   map[string]int
}

// New returns an empty plugin. Exported so tests can drive one directly
// instead of reaching for the process-wide host.
func New() *Plugin {
	return &Plugin{counts: map[string]int{}, errs: map[string]int{}}
}

// Name identifies the plugin to the host.
func (*Plugin) Name() string { return "toolstats" }

// Register wires the plugin into the host.
func (p *Plugin) Register(h *ext.Host) error {
	// Count every tool, so match is empty. post_tool carries the error text,
	// which pre_tool cannot know yet.
	h.OnTool("", nil, func(_ context.Context, ev hooks.Event) error {
		p.record(ev.Tool, ev.Err != "")
		return nil
	})

	// Counts describe one session. Resetting on start keeps a resumed or
	// switched session from inheriting the previous one's totals.
	h.OnSession(hooks.KindSessionStart, func(context.Context, hooks.SessionEvent) error {
		p.Reset()
		return nil
	})

	h.RegisterCommand(ext.Command{
		Name:        "toolstats",
		Description: "Show tool call counts for this session",
		Run: func(context.Context, []string) (hooks.CommandResult, error) {
			return hooks.CommandResult{Toast: p.Summary()}, nil
		},
	})
	return nil
}

func (p *Plugin) record(tool string, failed bool) {
	if tool == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counts[tool]++
	if failed {
		p.errs[tool]++
	}
}

// Reset clears the counters.
func (p *Plugin) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.counts)
	clear(p.errs)
}

// Summary renders the counts, busiest tool first. Ties break by name so the
// output does not reshuffle between calls.
func (p *Plugin) Summary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.counts) == 0 {
		return "no tool calls yet"
	}

	names := make([]string, 0, len(p.counts))
	for name := range p.counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if p.counts[names[i]] != p.counts[names[j]] {
			return p.counts[names[i]] > p.counts[names[j]]
		}
		return names[i] < names[j]
	})

	parts := make([]string, 0, len(names))
	total := 0
	for _, name := range names {
		total += p.counts[name]
		if e := p.errs[name]; e > 0 {
			parts = append(parts, fmt.Sprintf("%s %d (%d failed)", name, p.counts[name], e))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d", name, p.counts[name]))
	}
	return fmt.Sprintf("%d tool calls: %s", total, strings.Join(parts, ", "))
}
