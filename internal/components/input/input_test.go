package input

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/layout"
)

func TestTextFieldCtrlEditing(t *testing.T) {
	f := &TextField{Value: "abc", Cursor: 1}
	ctx := &components.EventContext{}
	f.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'a', Mods: xui.ModCtrl, Press: true})
	if f.Cursor != 0 {
		t.Fatalf("Ctrl+A cursor=%d", f.Cursor)
	}
	f.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'e', Mods: xui.ModCtrl, Press: true})
	if f.Cursor != 3 {
		t.Fatalf("Ctrl+E cursor=%d", f.Cursor)
	}
	f.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'u', Mods: xui.ModCtrl, Press: true})
	if f.Value != "" || f.Cursor != 0 || !ctx.Consume {
		t.Fatalf("Ctrl+U value=%q cursor=%d consume=%v", f.Value, f.Cursor, ctx.Consume)
	}
}

func TestDiffBlock(t *testing.T) {
	d := &DiffBlock{Diff: "+added\n-removed\n context", Theme: components.DefaultTheme()}
	ds := d.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 10}})
	if ds.Size.Height != 3 {
		t.Fatalf("diff lines %d", ds.Size.Height)
	}
}

func TestModalMarkdown(t *testing.T) {
	md := &Markdown{Source: "# Hello\n- item `code`", Theme: components.DefaultTheme()}
	ms := md.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 10}})
	if ms.Size.Height < 2 {
		t.Fatalf("markdown h=%d", ms.Size.Height)
	}
	modal := &Modal{
		Title:  "Confirm",
		Body:   &layout.Text{Content: "Sure?"},
		Footer: "Esc close",
		Width:  40,
		Theme:  components.DefaultTheme(),
	}
	s := modal.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 24}})
	if len(s.Children) != 1 {
		t.Fatalf("modal children %d", len(s.Children))
	}
}
