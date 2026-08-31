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
