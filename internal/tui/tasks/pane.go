package tasks

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/job"
)

const defaultWidth = 28

// Pane is the persistent TASKS sidebar.
type Pane struct {
	Theme    components.Theme
	Visible  bool
	forced   bool // user toggled on even with no jobs
	hidden   bool // user hid it; sticky until Toggle, even with live jobs
	Selected int
	Attached string // job id the TUI is currently talking to
	rows     []job.Info
	OnOpen   func(jobID string) // view popup
}

// Width is the sidebar column count when visible.
func (p *Pane) Width() int {
	if p == nil || !p.Visible {
		return 0
	}
	return defaultWidth
}

// Toggle shows or hides the sidebar. Hide is sticky while jobs are running
// (SetJobs must not immediately show it again).
func (p *Pane) Toggle() {
	if p == nil {
		return
	}
	p.Visible = !p.Visible
	p.hidden = !p.Visible
	p.forced = p.Visible
}

// SetJobs replaces the row list. Live jobs should come first.
func (p *Pane) SetJobs(live, recent []job.Info) {
	if p == nil {
		return
	}
	seen := map[string]struct{}{}
	var rows []job.Info
	for _, inf := range live {
		if inf.ID == "" {
			continue
		}
		seen[inf.ID] = struct{}{}
		rows = append(rows, inf)
	}
	for _, inf := range recent {
		if _, ok := seen[inf.ID]; ok {
			continue
		}
		rows = append(rows, inf)
		if len(rows) >= 24 {
			break
		}
	}
	p.rows = rows
	if p.Selected >= len(p.rows) {
		p.Selected = max(len(p.rows)-1, 0)
	}
	if p.hidden {
		p.Visible = false
		return
	}
	p.Visible = p.forced || len(p.rows) > 0
}

// SelectedID is the highlighted job, or empty.
func (p *Pane) SelectedID() string {
	if p == nil || p.Selected < 0 || p.Selected >= len(p.rows) {
		return ""
	}
	return p.rows[p.Selected].ID
}

// Handle consumes sidebar keys/clicks. Returns true if handled.
func (p *Pane) Handle(ctx *components.EventContext, ev xui.Event) bool {
	if p == nil || !p.Visible {
		return false
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		// Ctrl+N / Ctrl+P walk the list without stealing composer arrows.
		if components.IsChord(e, 'n', 'N') {
			if p.Selected+1 < len(p.rows) {
				p.Selected++
			}
			ctx.ConsumeAndRedraw()
			return true
		}
		if components.IsChord(e, 'p', 'P') {
			if p.Selected > 0 {
				p.Selected--
			}
			ctx.ConsumeAndRedraw()
			return true
		}
		// Ctrl-only: Ghostty binds Cmd+Enter to toggle_fullscreen.
		if components.CtrlOnly(e) && e.Code == xui.KeyEnter && p.Selected >= 0 && p.Selected < len(p.rows) &&
			p.OnOpen != nil {
			p.OnOpen(p.rows[p.Selected].ID)
			ctx.ConsumeAndRedraw()
			return true
		}
	}
	return false
}

// Draw renders the sidebar.
func (p *Pane) Draw(ctx components.DrawContext, height int) components.Surface {
	w := defaultWidth
	h := height
	if h < 1 {
		h = 1
	}
	s := components.NewSurface(w, h, nil)
	th := p.Theme
	border := th.Border
	title := th.Warning
	if title.Fg.Kind == 0 {
		title = th.Foreground
	}
	for y := 0; y < h; y++ {
		s.Print(0, y, "│", border, ctx.Method)
	}
	s.Print(2, 0, "TASKS", title, ctx.Method)
	if len(p.rows) == 0 {
		s.Print(2, 2, "no jobs", th.Muted, ctx.Method)
		return s
	}
	y := 2
	for i, inf := range p.rows {
		if y >= h {
			break
		}
		icon, st := jobIcon(inf.Status, th)
		label := jobLabel(inf)
		line := icon + " " + clip(label, w-4, ctx.Method)
		attached := p.Attached != "" && inf.ID == p.Attached
		if i == p.Selected {
			s.Print(1, y, strings.Repeat(" ", w-1), th.SelectionBg, ctx.Method)
			s.Print(2, y, line, th.SelectionFg, ctx.Method)
		} else if attached {
			s.Print(2, y, icon+" ", st, ctx.Method)
			mark := th.Warning
			s.Print(4, y, clip(label, w-5, ctx.Method), mark, ctx.Method)
		} else {
			s.Print(2, y, icon+" ", st, ctx.Method)
			s.Print(4, y, clip(label, w-5, ctx.Method), th.Foreground, ctx.Method)
		}
		y++
		if y >= h {
			break
		}
		meta := jobMeta(inf)
		if meta != "" {
			s.Print(4, y, clip(meta, w-5, ctx.Method), th.Muted, ctx.Method)
			y++
		}
		y++ // spacer
	}
	return s
}

func jobIcon(st job.Status, th components.Theme) (string, xui.Style) {
	switch st {
	case job.StatusCompleted:
		return "✓", th.Success
	case job.StatusFailed, job.StatusTimedOut:
		return "✗", th.Destructive
	case job.StatusCancelled:
		return "⊘", th.Muted
	default:
		return "●", th.ToolName
	}
}

func jobLabel(inf job.Info) string {
	if d := strings.TrimSpace(inf.Description); d != "" {
		return d
	}
	if inf.Role != "" {
		return string(inf.Role)
	}
	return inf.ID
}

func jobMeta(inf job.Info) string {
	var parts []string
	if inf.Role != "" {
		parts = append(parts, string(inf.Role))
	}
	if !inf.Status.Terminal() && !inf.StartedAt.IsZero() {
		parts = append(parts, time.Since(inf.StartedAt).Truncate(time.Second).String())
	} else if !inf.FinishedAt.IsZero() && !inf.StartedAt.IsZero() {
		parts = append(parts, inf.FinishedAt.Sub(inf.StartedAt).Truncate(time.Second).String())
	}
	return strings.Join(parts, " · ")
}

func clip(s string, cols int, method xui.WidthMethod) string {
	if cols <= 0 {
		return ""
	}
	if xui.StringWidth(s, method) <= cols {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		w := 1
		if r != utf8.RuneError {
			w = xui.StringWidth(string(r), method)
		}
		if n+w > cols-1 {
			break
		}
		b.WriteRune(r)
		n += w
	}
	b.WriteRune('…')
	return b.String()
}
