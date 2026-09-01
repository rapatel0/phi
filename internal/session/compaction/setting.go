package compaction

// Settings configures compaction: whether it is enabled and the token
// thresholds used to decide when to compact.
type Settings struct {
	enabled          bool
	compactPercent   int
	thresholdTokens  int
	keepRecentTokens int
}

var defaultSettings = Settings{
	enabled:          true,
	compactPercent:   95,
	thresholdTokens:  400_000,
	keepRecentTokens: 20000,
}

// DefaultSettings returns the 95% / 400k autocompact defaults.
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

// WithThresholdTokens returns a copy that also compact at this token count.
func (s Settings) WithThresholdTokens(n int) Settings {
	if n > 0 {
		s.thresholdTokens = n
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

// compactThreshold is the first of: percent of the window, or the token cap.
// A tiny window can round the percent down to 0; treat that as 1 so any
// usage still compact. The token cap only applies when it is smaller than
// the percent threshold, so a 128k model is not delayed until 400k.
func compactThreshold(contextWindow int, settings Settings) int {
	pct := settings.compactPercent
	if pct <= 0 {
		pct = 95
	}
	pct = clampPercent(pct)
	threshold := max(contextWindow*pct/100, 1)
	if cap := settings.thresholdTokens; cap > 0 && cap < threshold {
		threshold = cap
	}
	return threshold
}

// ShouldCompact reports whether contextTokens has reached the compact
// threshold: 95% of the window, or 400k tokens, whichever is first.
func ShouldCompact(contextTokens, contextWindow int, settings Settings) bool {
	if !settings.enabled || contextWindow <= 0 {
		return false
	}
	return contextTokens >= compactThreshold(contextWindow, settings)
}
