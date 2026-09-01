package compaction

import "testing"

func TestShouldCompactDefaultIs95Percent(t *testing.T) {
	settings := Settings{enabled: true, compactPercent: 95}

	if ShouldCompact(94, 100, settings) {
		t.Fatal("94% must not compact")
	}
	if !ShouldCompact(95, 100, settings) {
		t.Fatal("95% must compact")
	}
	if !ShouldCompact(96, 100, settings) {
		t.Fatal("above 95% must compact")
	}

	disabled := settings
	disabled.enabled = false
	if ShouldCompact(95, 100, disabled) {
		t.Fatal("disabled must not compact")
	}
	if ShouldCompact(95, 0, settings) {
		t.Fatal("zero window must not compact")
	}
}

func TestShouldCompactPercentIsConfigurable(t *testing.T) {
	settings := Settings{enabled: true, compactPercent: 80}
	if ShouldCompact(79, 100, settings) {
		t.Fatal("79% of 80% threshold must not compact")
	}
	if !ShouldCompact(80, 100, settings) {
		t.Fatal("80% of 80% threshold must compact")
	}
}

func TestShouldCompactTokenCap(t *testing.T) {
	settings := DefaultSettings() // 95% and 400k
	if ShouldCompact(399_999, 1_000_000, settings) {
		t.Fatal("under 400k of a 1M window must not compact")
	}
	if !ShouldCompact(400_000, 1_000_000, settings) {
		t.Fatal("400k of a 1M window must compact")
	}
	small := Settings{enabled: true, compactPercent: 95, thresholdTokens: 400_000}
	if ShouldCompact(94, 100, small) {
		t.Fatal("percent still wins on a small window")
	}
	if !ShouldCompact(95, 100, small) {
		t.Fatal("95% of 100 must compact")
	}
}
