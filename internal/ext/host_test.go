package ext

import (
	"testing"

	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/tools"
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
