package tasks

import (
	"testing"
	"time"

	"github.com/pulseaiclub/xui"

	"github.com/pulseaiclub/phi/internal/components"
	"github.com/pulseaiclub/phi/internal/job"
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
