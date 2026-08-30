package tasks

import (
	"testing"
	"time"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/job"
)

func TestPaneAutoShowsWithJobs(t *testing.T) {
	p := &Pane{Theme: components.DefaultTheme()}
	if p.Width() != 0 {
		t.Fatal("hidden by default")
	}
	p.SetJobs([]job.Info{{
		Meta: job.Meta{ID: "j1", Description: "Find auth", Status: job.StatusRunning, StartedAt: time.Now()},
	}}, nil)
	if !p.Visible || p.Width() != defaultWidth {
		t.Fatalf("expected visible sidebar, visible=%v width=%d", p.Visible, p.Width())
	}
}

func TestPaneToggle(t *testing.T) {
	p := &Pane{Theme: components.DefaultTheme()}
	p.Toggle()
	if !p.Visible {
		t.Fatal("toggle on")
	}
	p.Toggle()
	if p.Visible {
		t.Fatal("toggle off with no jobs")
	}
}

func TestPaneToggleHidesWhileJobsRun(t *testing.T) {
	p := &Pane{Theme: components.DefaultTheme()}
	p.SetJobs([]job.Info{{
		Meta: job.Meta{ID: "j1", Description: "Find auth", Status: job.StatusRunning},
	}}, nil)
	if !p.Visible {
		t.Fatal("auto-show")
	}
	p.Toggle()
	if p.Visible {
		t.Fatal("toggle must hide even with live jobs")
	}
	p.SetJobs([]job.Info{{
		Meta: job.Meta{ID: "j1", Description: "Find auth", Status: job.StatusRunning},
	}}, nil)
	if p.Visible {
		t.Fatal("SetJobs must not re-show after user hide")
	}
	p.Toggle()
	if !p.Visible {
		t.Fatal("toggle on again")
	}
}

func TestPaneDraw(t *testing.T) {
	p := &Pane{Theme: components.DefaultTheme()}
	p.SetJobs([]job.Info{
		{
			Meta: job.Meta{
				ID:          "j1",
				Description: "Find auth",
				Role:        job.RoleExplore,
				Status:      job.StatusRunning,
				StartedAt:   time.Now(),
			},
		},
	}, nil)
	s := p.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 12}, Method: xui.WidthUnicode}, 12)
	if s.Size.Width != defaultWidth || s.Size.Height != 12 {
		t.Fatalf("surface size %+v", s.Size)
	}
}

func TestPaneTreeHeader(t *testing.T) {
	p := &Pane{Theme: components.DefaultTheme()}
	p.SetJobs([]job.Info{{
		Meta: job.Meta{
			ID: "j1", Role: job.RoleWorker, Description: "voice",
			Status: job.StatusRunning, StartedAt: time.Now(),
		},
	}}, nil)
	s := p.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 12}, Method: xui.WidthUnicode}, 12)
	if s.Size.Width != defaultWidth {
		t.Fatalf("width %d", s.Size.Width)
	}
}

func TestPaneMouseOpens(t *testing.T) {
	var opened string
	p := &Pane{Theme: components.DefaultTheme(), OnOpen: func(id string) { opened = id }}
	p.SetJobs([]job.Info{{
		Meta: job.Meta{ID: "j1", Role: job.RoleWorker, Status: job.StatusRunning},
	}}, nil)
	p.SetFrame(10, 0, defaultWidth, 12)
	ctx := &components.EventContext{}
	handled := p.Handle(ctx, xui.MouseEvent{
		X: 12, Y: 2, Button: xui.MouseLeft, Action: xui.MousePress,
	})
	if !handled {
		t.Fatal("click should hit a row")
	}
	if opened != "j1" {
		t.Fatalf("opened %q", opened)
	}
	if p.SelectedID() != "j1" {
		t.Fatalf("selected %q", p.SelectedID())
	}
}

func TestPaneSelectCallback(t *testing.T) {
	var got string
	p := &Pane{Theme: components.DefaultTheme(), OnSelect: func(id string) { got = id }}
	p.SetJobs([]job.Info{
		{Meta: job.Meta{ID: "a", Status: job.StatusRunning}},
		{Meta: job.Meta{ID: "b", Status: job.StatusRunning}},
	}, nil)
	p.Selected = 0
	p.moveBy(1)
	if got != "b" {
		t.Fatalf("OnSelect=%q", got)
	}
}
