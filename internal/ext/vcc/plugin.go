package vcc

import (
	"context"
	"fmt"
	"strings"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/session/compaction"
)

func init() { ext.Register(&Plugin{}) }

// Plugin registers /vcc-compact, /vcc-recall, and /vcc-index.
type Plugin struct {
	sessionID string
}

func (*Plugin) Name() string { return "vcc" }

func (p *Plugin) Register(h *ext.Host) error {
	h.RegisterCommand(ext.Command{
		Name:        "vcc-compact",
		Description: "Run VCC compaction. Use status or config for details.",
		Run:         p.runCompact,
	})
	h.RegisterCommand(ext.Command{
		Name:        "vcc-recall",
		Description: "Search the VCC ledger. Usage: /vcc-recall <query>",
		Run:         p.runRecall,
	})
	h.RegisterCommand(ext.Command{
		Name:        "vcc-index",
		Description: "Prepare the global VCC config. Use rebuild.",
		Run:         p.runIndex,
	})
	h.OnSession(hooks.KindSessionStart, func(_ context.Context, ev hooks.SessionEvent) error {
		p.sessionID = ev.SessionID
		return nil
	})
	return nil
}

func (p *Plugin) runCompact(ctx context.Context, args []string) (hooks.CommandResult, error) {
	cmd := strings.TrimSpace(strings.Join(args, " "))
	cfg := compaction.LoadConfig()
	switch cmd {
	case "status":
		model := cfg.Compactor.Name
		if model == "" {
			model = "session model"
		}
		return hooks.CommandResult{Toast: fmt.Sprintf(
			"vcc-compact: model=%s, transport=%s, input=%d, output=%d, index=%t",
			model, cfg.Transport, cfg.MaxInputTokens, cfg.MaxOutputTokens, cfg.Index.Enabled,
		)}, nil
	case "config":
		path := compaction.ConfigPath()
		if path == "" {
			path = "Unknown"
		}
		return hooks.CommandResult{Toast: "vcc-compact configuration: " + path}, nil
	case "":
		if err := ext.Default().Compact(ctx); err != nil {
			return hooks.CommandResult{}, err
		}
		return hooks.CommandResult{Toast: "vcc-compact: compaction completed"}, nil
	default:
		return hooks.CommandResult{Toast: "vcc-compact accepts no arguments. Use status or config."}, nil
	}
}

func (p *Plugin) runRecall(_ context.Context, args []string) (hooks.CommandResult, error) {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		return hooks.CommandResult{Toast: "vcc-recall: usage /vcc-recall <query>"}, nil
	}
	if p.sessionID == "" {
		return hooks.CommandResult{Toast: "vcc-recall: no session is active"}, nil
	}
	hits := compaction.SearchLedger(p.sessionID, query, compaction.LoadConfig().Index.MaxSearchResults)
	text := compaction.FormatHits(hits)
	return hooks.CommandResult{Toast: text}, nil
}

func (*Plugin) runIndex(_ context.Context, args []string) (hooks.CommandResult, error) {
	if strings.TrimSpace(strings.Join(args, " ")) != "rebuild" {
		return hooks.CommandResult{Toast: "vcc-index accepts only rebuild."}, nil
	}
	if err := compaction.EnsureGlobalConfig(); err != nil {
		return hooks.CommandResult{}, err
	}
	return hooks.CommandResult{
		Toast: "vcc-index: configuration is ready. The next compaction writes the current ledger.",
	}, nil
}
