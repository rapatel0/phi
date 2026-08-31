package compaction

// Settings configures compaction: whether it is enabled and the token
// thresholds used to decide when to compact.
type Settings struct {
	enabled          bool
	compactPercent   int
	keepRecentTokens int
}

var defaultSettings = Settings{
	enabled:          true,
	compactPercent:   95,
	keepRecentTokens: 20000,
}

// DefaultSettings returns the 95% autocompact defaults.
func DefaultSettings() Settings {
	return defaultSettings
}

// WithThresholdPercent returns a copy that compacts at pct of the window.
func (s Settings) WithThresholdPercent(pct int) Settings {
	if pct > 0 {
		s.compactPercent = clampPercent(pct)
	}
	return s
}

func clampPercent(pct int) int {
	if pct < 1 {
		return 1
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// ShouldCompact reports whether contextTokens has reached compactPercent of
// the context window (default 95%).
func ShouldCompact(contextTokens, contextWindow int, settings Settings) bool {
	if !settings.enabled || contextWindow <= 0 {
		return false
	}
	pct := settings.compactPercent
	if pct <= 0 {
		pct = 95
	}
	pct = clampPercent(pct)
	threshold := contextWindow * pct / 100
	return contextTokens >= threshold
}
