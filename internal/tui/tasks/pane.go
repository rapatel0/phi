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
type taskRow struct {
	info       job.Info
	prefix     string
	metaPrefix string
	root       bool
}

type Pane struct {
	Theme     components.Theme
	Visible   bool
	forced    bool // user toggled on even with no jobs
	hidden    bool // user hid it; sticky until Toggle, even with live jobs
	Selected  int
	Attached  string // job id the TUI is currently talking to
	RootID    string // current session id shown above child jobs
	RootLabel string // label for the current session root
	rows      []taskRow
	OnOpen    func(jobID string) // view transcript
	OnSelect  func(jobID string) // selection moved; may swap an open view
	frameX    int
	frameY    int
	frameW    int
	frameH    int
}

// SetRoot sets the current session row shown above child jobs.
func (p *Pane) SetRoot(id, label string) {
	if p == nil {
		return
	}
	p.RootID = id
	p.RootLabel = label
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
	selectedID := p.SelectedID()
	seen := map[string]struct{}{}
	infos := make([]job.Info, 0, len(live)+len(recent))
	for _, inf := range append(append([]job.Info(nil), live...), recent...) {
		if inf.ID == "" {
			continue
		}
		if _, ok := seen[inf.ID]; ok {
			continue
		}
		seen[inf.ID] = struct{}{}
		infos = append(infos, inf)
	}
	p.rows = treeRows(infos)
	if p.RootID != "" && len(p.rows) > 0 {
		root := taskRow{
			info: job.Info{Meta: job.Meta{ID: p.RootID, Description: p.RootLabel, Status: job.StatusRunning}},
			root: true,
		}
		p.rows = append([]taskRow{root}, p.rows...)
	}
	p.Selected = 0
	if selectedID != "" {
		for i, row := range p.rows {
			if !row.root && row.info.ID == selectedID {
				p.Selected = i
				break
			}
		}
	} else if len(p.rows) > 1 && p.rows[0].root {
		p.Selected = 1
	}
	if p.hidden {
		p.Visible = false
		return
	}
	p.Visible = p.forced || len(p.rows) > 0
}

// SelectedID is the highlighted job, or empty.
func (p *Pane) SelectedID() string {
	if p == nil || p.Selected < 0 || p.Selected >= len(p.rows) || p.rows[p.Selected].root {
		return ""
	}
	return p.rows[p.Selected].info.ID
}

// Handle consumes sidebar keys/clicks. Returns true if handled.
func (p *Pane) Handle(ctx *components.EventContext, ev xui.Event) bool {
	if p == nil || !p.Visible {
		return false
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		km := components.Keys
		if km.Hit(e, km.TreeNext) {
			p.moveBy(1)
			ctx.ConsumeAndRedraw()
			return true
		}
		if km.Hit(e, km.TreePrev) {
			p.moveBy(-1)
			ctx.ConsumeAndRedraw()
			return true
		}
		if km.Hit(e, km.ChildEnter) && p.SelectedID() != "" && p.OnOpen != nil {
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
			if p.OnOpen != nil && !p.rows[idx].root {
				p.OnOpen(p.rows[idx].info.ID)
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
	if p.OnSelect != nil && !p.rows[next].root {
		p.OnSelect(p.rows[next].info.ID)
	}
}

func treeRows(infos []job.Info) []taskRow {
	if len(infos) == 0 {
		return nil
	}
	known := make(map[string]struct{}, len(infos))
	children := make(map[string][]job.Info)
	for _, inf := range infos {
		known[inf.ID] = struct{}{}
		children[inf.ParentID] = append(children[inf.ParentID], inf)
	}
	roots := make([]job.Info, 0, len(infos))
	for _, inf := range infos {
		if _, ok := known[inf.ParentID]; !ok {
			roots = append(roots, inf)
		}
	}
	rows := make([]taskRow, 0, len(infos))
	seen := make(map[string]struct{}, len(infos))
	var visit func([]job.Info, string)
	visit = func(siblings []job.Info, parentPrefix string) {
		for i, inf := range siblings {
			if _, ok := seen[inf.ID]; ok {
				continue
			}
			seen[inf.ID] = struct{}{}
			last := i == len(siblings)-1
			branch := "├─ "
			if last {
				branch = "└─ "
			}
			rows = append(rows, taskRow{info: inf, prefix: parentPrefix + branch, metaPrefix: parentPrefix + "│  └ "})
			childPrefix := parentPrefix + "│  "
			if last {
				childPrefix = parentPrefix + "   "
			}
			visit(children[inf.ID], childPrefix)
		}
	}
	visit(roots, "")
	return rows
}

func (p *Pane) hit(localY int) (int, bool) {
	if localY < headerRows || len(p.rows) == 0 {
		return 0, false
	}
	y := headerRows
	for idx, row := range p.rows {
		h := rowHeight
		if row.root {
			h = 1
		}
		if localY >= y && localY < y+h {
			return idx, true
		}
		y += h
	}
	return 0, false
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
	for _, row := range p.rows {
		if row.root {
			continue
		}
		inf := row.info
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
	for i, row := range p.rows {
		inf := row.info
		if y >= h {
			break
		}
		branch := row.prefix
		child := row.metaPrefix
		icon, st := jobIcon(inf.Status, th)
		label := branch + icon + " " + jobLabel(inf)
		if row.root {
			label = "● " + jobLabel(inf)
		}
		meta := child + jobMeta(inf)
		attached := p.Attached != "" && inf.ID == p.Attached
		selected := i == p.Selected

		fg := th.Foreground
		if attached {
			fg = th.Warning
		}
		if selected {
			s.Print(1, y, strings.Repeat(" ", w-1), th.SelectionBg, ctx.Method)
			s.Print(2, y, clip(label, w-3, ctx.Method), th.SelectedRow(), ctx.Method)
		} else {
			s.Print(2, y, clip(label, w-3, ctx.Method), fg, ctx.Method)
			if !attached {
				s.Print(2+len([]rune(branch)), y, icon, st, ctx.Method)
			}
		}
		y++
		if row.root {
			continue
		}
		if y >= h {
			break
		}
		if selected {
			s.Print(1, y, strings.Repeat(" ", w-1), th.SelectionBg, ctx.Method)
			s.Print(2, y, clip(meta, w-3, ctx.Method), th.SelectedRow(), ctx.Method)
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
