package main

import "testing"

func TestFirstEnvUsesFirstNonEmptyValue(t *testing.T) {
	t.Setenv("ALPHA_TSNET_EMPTY", "")
	t.Setenv("ALPHA_TSNET_FIRST", "first")
	t.Setenv("ALPHA_TSNET_SECOND", "second")
	if got := firstEnv("ALPHA_TSNET_EMPTY", "ALPHA_TSNET_FIRST", "ALPHA_TSNET_SECOND"); got != "first" {
		t.Fatalf("firstEnv() = %q, want first", got)
	}
}

func TestFirstEnvReturnsEmptyWhenUnset(t *testing.T) {
	t.Setenv("ALPHA_TSNET_EMPTY", "")
	if got := firstEnv("ALPHA_TSNET_EMPTY"); got != "" {
		t.Fatalf("firstEnv() = %q, want empty", got)
	}
}
