package ext

import (
	"errors"
	"testing"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools"
)

type stubPlugin struct{}

func (stubPlugin) Name() string { return "stub" }
func (stubPlugin) Register(h *Host) error {
	h.RegisterTool(tools.Tool{Definition: llm.ToolDefinition{Name: "stub_tool"}})
	h.AddFooter(func() string { return "hi" })
	return nil
}

func TestHostRegister(t *testing.T) {
	h := NewHost()
	if err := h.Add(stubPlugin{}); err != nil {
		t.Fatal(err)
	}
	if got := h.Names(); len(got) != 1 || got[0] != "stub" {
		t.Fatalf("names %v", got)
	}
	if len(h.Tools()) != 1 || h.Tools()[0].Definition.Name != "stub_tool" {
		t.Fatalf("tools %+v", h.Tools())
	}
	bits := h.FooterBits()
	if len(bits) != 1 || bits[0] != "hi" {
		t.Fatalf("footer %v", bits)
	}
}

// A loop that fires in a headless run has nowhere to send its prompt. Saying
// so is better than dropping the work silently.
func TestWakeWithoutTheShell(t *testing.T) {
	if err := NewHost().Wake("run the checks"); err == nil {
		t.Fatal("want an error when no shell is listening")
	}
}

func TestWakeReachesTheShell(t *testing.T) {
	h := NewHost()
	got := ""
	h.SetWake(func(text string) error { got = text; return nil })

	if err := h.Wake("run the checks"); err != nil {
		t.Fatalf("wake: %v", err)
	}
	if got != "run the checks" {
		t.Fatalf("got %q", got)
	}
}

// The shell refuses while a turn is already streaming, and that refusal has to
// reach the caller so the loop can retry instead of assuming it fired.
func TestWakeReportsTheShellRefusal(t *testing.T) {
	h := NewHost()
	h.SetWake(func(string) error { return errors.New("busy") })

	if err := h.Wake("now"); err == nil || err.Error() != "busy" {
		t.Fatalf("want the shell reason, got %v", err)
	}
}

func TestWakeOnNilHost(t *testing.T) {
	var h *Host
	if err := h.Wake("x"); err == nil {
		t.Fatal("want an error on a nil host")
	}
}
