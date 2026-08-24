package editor

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/pulseaiclub/phi/internal/components"
	"github.com/pulseaiclub/phi/internal/job"
	"github.com/pulseaiclub/phi/internal/tui/tasks"
)

func TestEditorCtrlBHidesTasksWhileJobsRun(t *testing.T) {
	p := &tasks.Pane{Theme: components.DefaultTheme()}
	p.SetJobs([]job.Info{{
		Meta: job.Meta{ID: "j1", Description: "review", Status: job.StatusRunning},
	}}, nil)
	if !p.Visible {
		t.Fatal("auto-show")
	}
	e := &Editor{tasks: p}
	ctx := &components.EventContext{}
	e.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'b', Press: true, Mods: xui.ModCtrl})
	if p.Visible {
		t.Fatal("ctrl+b must hide even with live jobs")
	}
	if !ctx.Consume || !ctx.Redraw {
		t.Fatal("ctrl+b must consume and redraw")
	}
	// Draw → refreshTasks → SetJobs must not undo the hide.
	p.SetJobs([]job.Info{{
		Meta: job.Meta{ID: "j1", Description: "review", Status: job.StatusRunning},
	}}, nil)
	if p.Visible {
		t.Fatal("job refresh must not re-show after hide")
	}
}

func TestEditorCtrlTTogglesTasks(t *testing.T) {
	p := &tasks.Pane{Theme: components.DefaultTheme()}
	e := &Editor{tasks: p}
	ctx := &components.EventContext{}
	e.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 't', Press: true, Mods: xui.ModCtrl})
	if !p.Visible {
		t.Fatal("ctrl+t show")
	}
	e.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'T', Press: true, Mods: xui.ModCtrl})
	if p.Visible {
		t.Fatal("ctrl+t hide")
	}
}
