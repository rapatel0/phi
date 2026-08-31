package compact

import (
	"context"
	"fmt"
	"strings"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/session/compaction"
)

func init() { ext.Register(&Plugin{}) }

// Plugin registers /compact and /recall.
type Plugin struct {
	sessionID string
}

func (*Plugin) Name() string { return "compact" }

func (p *Plugin) Register(h *ext.Host) error {
	h.RegisterCommand(ext.Command{
		Name:        "compact",
		Description: "Run hybrid compaction. Use status, config, or index rebuild.",
		Run:         p.runCompact,
	})
	h.RegisterCommand(ext.Command{
		Name:        "recall",
		Description: "Search compacted history. Usage: /recall <query>",
		Run:         p.runRecall,
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
			"compact: model=%s, transport=%s, threshold=%d%%, input=%d, output=%d, index=%t",
			model, cfg.Transport, cfg.ThresholdPercent, cfg.MaxInputTokens, cfg.MaxOutputTokens, cfg.Index.Enabled,
		)}, nil
	case "config":
		path := compaction.ConfigPath()
		if path == "" {
			path = "Unknown"
		}
		return hooks.CommandResult{Toast: "compact configuration: " + path}, nil
	case "index", "index rebuild":
		if err := compaction.EnsureGlobalConfig(); err != nil {
			return hooks.CommandResult{}, err
		}
		return hooks.CommandResult{Toast: "compact: configuration is ready. The next compaction writes the ledger."}, nil
	case "", "now":
		if err := ext.Default().Compact(ctx); err != nil {
			return hooks.CommandResult{}, err
		}
		return hooks.CommandResult{Toast: "compact: completed"}, nil
	default:
		return hooks.CommandResult{Toast: "compact accepts status, config, index rebuild, or no arguments."}, nil
	}
}

func (p *Plugin) runRecall(_ context.Context, args []string) (hooks.CommandResult, error) {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		return hooks.CommandResult{Toast: "recall: usage /recall <query>"}, nil
	}
	if p.sessionID == "" {
		return hooks.CommandResult{Toast: "recall: no session is active"}, nil
	}
	hits := compaction.SearchLedger(p.sessionID, query, compaction.LoadConfig().Index.MaxSearchResults)
	return hooks.CommandResult{Toast: compaction.FormatHits(hits)}, nil
}
