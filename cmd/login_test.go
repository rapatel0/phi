package main

import "testing"

func TestParseLoginArgs(t *testing.T) {
	opts, err := parseLoginArgs([]string{"anthropic"})
	if err != nil || opts.provider != "anthropic" || opts.profile != "" {
		t.Fatalf("%+v %v", opts, err)
	}
	opts, err = parseLoginArgs([]string{"--profile", "work", "anthropic"})
	if err != nil || opts.provider != "anthropic" || opts.profile != "work" {
		t.Fatalf("%+v %v", opts, err)
	}
	opts, err = parseLoginArgs([]string{"codex", "--profile=home"})
	if err != nil || opts.provider != "codex" || opts.profile != "home" {
		t.Fatalf("%+v %v", opts, err)
	}
	if _, err := parseLoginArgs([]string{"--profile"}); err == nil {
		t.Fatal("expected error for missing profile name")
	}
}
