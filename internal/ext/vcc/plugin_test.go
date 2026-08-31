package vcc

import (
	"context"
	"testing"

	"github.com/rapatel0/alpha/internal/ext"
)

func TestCommandsStatusAndIndex(t *testing.T) {
	t.Setenv("ALPHA_VCC_DIR", t.TempDir())
	h := ext.NewHost()
	p := &Plugin{}
	if err := p.Register(h); err != nil {
		t.Fatal(err)
	}
	res, err := p.runCompact(context.Background(), []string{"status"})
	if err != nil || res.Toast == "" {
		t.Fatalf("status: %+v %v", res, err)
	}
	res, err = p.runIndex(context.Background(), []string{"rebuild"})
	if err != nil || res.Toast == "" {
		t.Fatalf("index: %+v %v", res, err)
	}
}

func TestRecallNeedsSession(t *testing.T) {
	p := &Plugin{}
	res, err := p.runRecall(context.Background(), []string{"toast"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Toast == "" {
		t.Fatal("expected toast")
	}
}
