package tasks

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/job"
)

const defaultWidth = 36

const (
	headerRows = 2 // title + blank
	rowHeight  = 2 // title line + status line
)

// Pane is the persistent agent-tree sidebar.
type Pane struct {
	Theme    components.Theme
	Visible  bool
	forced   bool // user toggled on even with no jobs
	hidden   bool // user hid it; sticky until Toggle, even with live jobs
	Selected int
	Attached string // job id the TUI is currently talking to
	rows     []job.Info
	OnOpen   func(jobID string) // view transcript
	OnSelect func(jobID string) // selection moved; may swap an open view
	frameX   int
	frameY   int
	frameW   int
	frameH   int
}

// Width is the sidebar column count when visible.
func (p *Pane) Width() int {
	if p == nil || !p.Visible {
		return 0
	}
	return defaultWidth
}

// SetFrame records where Draw placed the sidebar, so mouse hits can be mapped.
func (p *Pane) SetFrame(x, y, w, h int) {
	if p == nil {
		return
	}
	p.frameX, p.frameY, p.frameW, p.frameH = x, y, w, h
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
		if components.IsChord(e, 'n', 'N') {
			p.moveBy(1)
			ctx.ConsumeAndRedraw()
			return true
		}
		if components.IsChord(e, 'p', 'P') {
			p.moveBy(-1)
			ctx.ConsumeAndRedraw()
			return true
		}
		if components.CtrlOnly(e) && e.Code == xui.KeyEnter && p.SelectedID() != "" && p.OnOpen != nil {
			p.OnOpen(p.SelectedID())
			ctx.ConsumeAndRedraw()
			return true
		}
	case xui.MouseEvent:
		if e.Action != xui.MousePress || e.Button != xui.MouseLeft {
			return false
		}
		if e.X < p.frameX || e.Y < p.frameY || e.X >= p.frameX+p.frameW || e.Y >= p.frameY+p.frameH {
			return false
		}
		if idx, ok := p.hit(e.Y - p.frameY); ok {
			p.Selected = idx
			if p.OnOpen != nil {
				p.OnOpen(p.rows[idx].ID)
			}
			ctx.ConsumeAndRedraw()
			return true
		}
	}
	return false
}

func (p *Pane) moveBy(delta int) {
	if len(p.rows) == 0 {
		return
	}
	next := max(p.Selected+delta, 0)
	if next >= len(p.rows) {
		next = len(p.rows) - 1
	}
	if next == p.Selected {
		return
	}
	p.Selected = next
	if p.OnSelect != nil {
		p.OnSelect(p.rows[next].ID)
	}
}

func (p *Pane) hit(localY int) (int, bool) {
	if localY < headerRows || len(p.rows) == 0 {
		return 0, false
	}
	idx := (localY - headerRows) / rowHeight
	if idx < 0 || idx >= len(p.rows) {
		return 0, false
	}
	return idx, true
}

// Draw renders the sidebar as a tree of sub-agents.
func (p *Pane) Draw(ctx components.DrawContext, height int) components.Surface {
	w := defaultWidth
	h := max(height, 1)
	s := components.NewSurface(w, h, nil)
	th := p.Theme
	border := th.Border
	title := th.Warning
	if title.Fg.Kind == 0 {
		title = th.Foreground
	}
	for y := range h {
		s.Print(0, y, "│", border, ctx.Method)
	}

	live := 0
	for _, inf := range p.rows {
		if !inf.Status.Terminal() {
			live++
		}
	}
	head := "Async agents"
	if live > 0 {
		head = fmt.Sprintf("Async agents · %d live", live)
	}
	s.Print(2, 0, clip(head, w-3, ctx.Method), title, ctx.Method)

	if len(p.rows) == 0 {
		s.Print(2, 2, "no jobs", th.Muted, ctx.Method)
		return s
	}

	y := headerRows
	last := len(p.rows) - 1
	for i, inf := range p.rows {
		if y >= h {
			break
		}
		branch := "├─ "
		child := "│  └ "
		if i == last {
			branch = "└─ "
			child = "   └ "
		}
		icon, st := jobIcon(inf.Status, th)
		label := branch + icon + " " + jobLabel(inf)
		meta := child + jobMeta(inf)
		attached := p.Attached != "" && inf.ID == p.Attached
		selected := i == p.Selected

		fg := th.Foreground
		if attached {
			fg = th.Warning
		}
		if selected {
			s.Print(1, y, strings.Repeat(" ", w-1), th.SelectionBg, ctx.Method)
			s.Print(2, y, clip(label, w-3, ctx.Method), th.SelectionFg, ctx.Method)
		} else {
			s.Print(2, y, clip(label, w-3, ctx.Method), fg, ctx.Method)
			if !attached {
				s.Print(2+len([]rune(branch)), y, icon, st, ctx.Method)
			}
		}
		y++
		if y >= h {
			break
		}
		if selected {
			s.Print(1, y, strings.Repeat(" ", w-1), th.SelectionBg, ctx.Method)
			s.Print(2, y, clip(meta, w-3, ctx.Method), th.SelectionFg, ctx.Method)
		} else {
			s.Print(2, y, clip(meta, w-3, ctx.Method), th.Muted, ctx.Method)
		}
		y++
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
	role := strings.TrimSpace(string(inf.Role))
	desc := strings.TrimSpace(inf.Description)
	switch {
	case role != "" && desc != "" && !strings.EqualFold(role, desc):
		return role + " · " + desc
	case desc != "":
		return desc
	case role != "":
		return role
	default:
		return inf.ID
	}
}

func jobMeta(inf job.Info) string {
	var parts []string
	parts = append(parts, statusLabel(inf.Status))
	if !inf.Status.Terminal() && !inf.StartedAt.IsZero() {
		parts = append(parts, time.Since(inf.StartedAt).Truncate(time.Second).String())
	} else if !inf.FinishedAt.IsZero() && !inf.StartedAt.IsZero() {
		parts = append(parts, inf.FinishedAt.Sub(inf.StartedAt).Truncate(time.Second).String())
	}
	return strings.Join(parts, " · ")
}

func statusLabel(st job.Status) string {
	switch st {
	case job.StatusCompleted:
		return "done"
	case job.StatusFailed:
		return "failed"
	case job.StatusCancelled:
		return "cancelled"
	case job.StatusTimedOut:
		return "timeout"
	case job.StatusStarting:
		return "starting"
	default:
		return "active now"
	}
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
