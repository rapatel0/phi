package childview

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/session"
)

func TestViewEscCloses(t *testing.T) {
	v := Open(
		components.DefaultTheme(),
		job.Info{Meta: job.Meta{ID: "j1", Description: "Find auth"}},
		session.Snapshot{},
		nil,
	)
	ctx := &components.EventContext{}
	keep, steer := v.Handle(ctx, xui.KeyEvent{Code: xui.KeyEscape, Press: true})
	if keep || steer {
		t.Fatalf("keep=%v steer=%v", keep, steer)
	}
}

func TestViewCtrlISteers(t *testing.T) {
	v := Open(components.DefaultTheme(), job.Info{Meta: job.Meta{ID: "j1"}}, session.Snapshot{}, nil)
	ctx := &components.EventContext{}
	keep, steer := v.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'i', Press: true, Mods: xui.ModCtrl})
	if keep || !steer {
		t.Fatalf("keep=%v steer=%v", keep, steer)
	}
}

func TestViewCmdISteers(t *testing.T) {
	v := Open(components.DefaultTheme(), job.Info{Meta: job.Meta{ID: "j1"}}, session.Snapshot{}, nil)
	ctx := &components.EventContext{}
	keep, steer := v.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'i', Press: true, Mods: xui.ModSuper})
	if keep || !steer {
		t.Fatalf("keep=%v steer=%v", keep, steer)
	}
}

func TestViewDoesNotSwallowCtrlB(t *testing.T) {
	v := Open(components.DefaultTheme(), job.Info{Meta: job.Meta{ID: "j1"}}, session.Snapshot{}, nil)
	ctx := &components.EventContext{}
	keep, steer := v.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'b', Press: true, Mods: xui.ModCtrl})
	if !keep || steer || ctx.Consume {
		t.Fatalf("keep=%v steer=%v consume=%v", keep, steer, ctx.Consume)
	}
}

func TestViewDraw(t *testing.T) {
	v := Open(components.DefaultTheme(), job.Info{Meta: job.Meta{
		ID: "j1", Description: "Find auth", Role: job.RoleExplore, Status: job.StatusRunning,
	}}, session.Snapshot{}, nil)
	s := v.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 12}, Method: xui.WidthUnicode}, 40, 12)
	if s.Size.Width != 40 || s.Size.Height != 12 {
		t.Fatalf("size %+v", s.Size)
	}
}
