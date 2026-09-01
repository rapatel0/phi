package commands

import (
	"fmt"
	"strconv"

	"github.com/rapatel0/alpha/internal/components/palette"
	"github.com/rapatel0/alpha/internal/session/compaction"
)

var compactPercents = []int{80, 90, 95, 99}

var compactTokenCaps = []struct {
	n     int
	label string
}{
	{100_000, "100k"},
	{200_000, "200k"},
	{400_000, "400k"},
	{800_000, "800k"},
	{1_000_000, "1M"},
}

// CompactCommand returns settings → compact with percent and token presets.
func CompactCommand(notify func(string)) palette.PaletteCommand {
	return palette.PaletteCommand{
		ID:           "settings-compact",
		Noun:         "settings",
		Verb:         "compact",
		Keywords:     []string{"compact", "threshold", "tokens", "context", "400k", "percent"},
		SubmenuTitle: "Compact",
		SubmenuFn: func() []palette.PaletteCommand {
			return compactSubmenus(notify)
		},
	}
}

func compactSubmenus(notify func(string)) []palette.PaletteCommand {
	cfg := compaction.LoadConfig()
	return []palette.PaletteCommand{
		{
			ID:           "compact-percent",
			Verb:         fmt.Sprintf("window  (%d%%)", cfg.ThresholdPercent),
			Keywords:     []string{"percent", "window"},
			SubmenuTitle: "Compact at % of window",
			Submenu:      compactPercentItems(notify, cfg.ThresholdPercent),
		},
		{
			ID:           "compact-tokens",
			Verb:         fmt.Sprintf("tokens  (%s)", formatTokens(cfg.ThresholdTokens)),
			Keywords:     []string{"tokens", "400k"},
			SubmenuTitle: "Compact at token cap",
			Submenu:      compactTokenItems(notify, cfg.ThresholdTokens),
		},
	}
}

func compactPercentItems(notify func(string), current int) []palette.PaletteCommand {
	out := make([]palette.PaletteCommand, 0, len(compactPercents))
	for _, pct := range compactPercents {
		verb := fmt.Sprintf("%d%%", pct)
		if pct == current {
			verb += "  (current)"
		}
		out = append(out, palette.PaletteCommand{
			ID:   fmt.Sprintf("compact-percent-%d", pct),
			Verb: verb,
			Run: func() {
				if err := saveCompactPercent(pct); err != nil {
					if notify != nil {
						notify(err.Error())
					}
					return
				}
				if notify != nil {
					notify(fmt.Sprintf("compact at %d%% of the context window", pct))
				}
			},
		})
	}
	return out
}

func compactTokenItems(notify func(string), current int) []palette.PaletteCommand {
	out := make([]palette.PaletteCommand, 0, len(compactTokenCaps))
	for _, cap := range compactTokenCaps {
		verb := cap.label
		if cap.n == current {
			verb += "  (current)"
		}
		out = append(out, palette.PaletteCommand{
			ID:   fmt.Sprintf("compact-tokens-%d", cap.n),
			Verb: verb,
			Run: func() {
				if err := saveCompactTokens(cap.n); err != nil {
					if notify != nil {
						notify(err.Error())
					}
					return
				}
				if notify != nil {
					notify("compact at " + cap.label + " tokens")
				}
			},
		})
	}
	return out
}

func saveCompactPercent(pct int) error {
	cfg := compaction.LoadConfig()
	cfg.ThresholdPercent = pct
	return compaction.SaveConfig(cfg)
}

func saveCompactTokens(n int) error {
	cfg := compaction.LoadConfig()
	cfg.ThresholdTokens = n
	return compaction.SaveConfig(cfg)
}

func formatTokens(n int) string {
	switch {
	case n >= 1_000_000 && n%1_000_000 == 0:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1000 && n%1000 == 0:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return strconv.Itoa(n)
	}
}
