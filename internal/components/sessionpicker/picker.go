package sessionpicker

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/layout"
)

// Picker is a tree dialog for choosing a persisted session.
//
// Projects are collapsible branches and sessions are leaves. Typing filters
// sessions by id, preview text, and project label; a project with no matching
// session is hidden.
type Picker struct {
	Open     bool
	Title    string
	Query    string
	Cursor   int // byte offset into Query
	Projects []Project
	Selected int // index into the visible rows
	Theme    components.Theme
	MaxItems int // visible rows; default 14
	Width    int // panel width; 0 = auto

	// OnAccept fires with the chosen session. It is not called for a project
	// header, which toggles instead.
	OnAccept func(Session)
	// OnClose fires after the dialog closes.
	OnClose func()
	// FocusReturn is focused when the dialog closes.
	FocusReturn components.Widget
	// Now is overridable in tests; defaults to time.Now.
	Now func() time.Time

	collapsed map[int]bool // project index -> collapsed
	rows      []row
	scroll    int
}

func (p *Picker) theme() components.Theme {
	if p.Theme.Border.Fg.Kind == 0 && p.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return p.Theme
}

func (p *Picker) title() string {
	if p.Title == "" {
		return "Sessions"
	}
	return p.Title
}

func (p *Picker) maxItems() int {
	if p.MaxItems < 1 {
		return 14
	}
	return p.MaxItems
}

func (p *Picker) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Show opens the dialog with projects. Every project except the current one
// starts collapsed, so the sessions you are most likely to want are visible
// without hiding the rest.
func (p *Picker) Show(projects []Project) {
	p.Open = true
	p.Projects = projects
	p.Query = ""
	p.Cursor = 0
	p.Selected = 0
	p.scroll = 0
	p.collapsed = make(map[int]bool, len(projects))
	for i, proj := range projects {
		p.collapsed[i] = !proj.Current
	}
	// With a single project there is nothing to choose between, so expand it.
	if len(projects) == 1 {
		p.collapsed[0] = false
	}
	p.rebuild()
	p.selectFirstSession()
}

// Hide closes the dialog.
func (p *Picker) Hide() {
	p.Open = false
	p.Query = ""
	p.Cursor = 0
	p.rows = nil
	if p.OnClose != nil {
		p.OnClose()
	}
}

// visibleSessions returns the sessions of a project that match the query.
func (p *Picker) visibleSessions(pi int) []Session {
	proj := p.Projects[pi]
	q := strings.TrimSpace(p.Query)
	if q == "" {
		return proj.Sessions
	}
	out := make([]Session, 0, len(proj.Sessions))
	for _, s := range proj.Sessions {
		if matchesFilter(s, proj.Label, q) {
			out = append(out, s)
		}
	}
	return out
}

// rebuild recomputes the flattened visible rows.
func (p *Picker) rebuild() {
	p.rows = p.rows[:0]
	filtering := strings.TrimSpace(p.Query) != ""
	for pi := range p.Projects {
		sessions := p.visibleSessions(pi)
		if len(sessions) == 0 {
			continue
		}
		p.rows = append(p.rows, row{kind: rowProject, project: pi, session: -1})
		// A filter implies the user wants to see hits, so ignore collapse.
		if p.collapsed[pi] && !filtering {
			continue
		}
		for si := range sessions {
			p.rows = append(p.rows, row{
				kind:    rowSession,
				project: pi,
				session: si,
				isLast:  si == len(sessions)-1,
			})
		}
	}
	if p.Selected >= len(p.rows) {
		p.Selected = len(p.rows) - 1
	}
	if p.Selected < 0 {
		p.Selected = 0
	}
}

// selectFirstSession moves the cursor to the first session row, so Enter is
// immediately useful instead of landing on a header.
func (p *Picker) selectFirstSession() {
	for i, r := range p.rows {
		if r.kind == rowSession {
			p.Selected = i
			return
		}
	}
	p.Selected = 0
}

// SelectedSession returns the highlighted session, if the cursor is on one.
func (p *Picker) SelectedSession() (Session, bool) {
	if p.Selected < 0 || p.Selected >= len(p.rows) {
		return Session{}, false
	}
	r := p.rows[p.Selected]
	if r.kind != rowSession {
		return Session{}, false
	}
	sessions := p.visibleSessions(r.project)
	if r.session < 0 || r.session >= len(sessions) {
		return Session{}, false
	}
	return sessions[r.session], true
}

// RowCount returns the number of visible rows (test/introspection helper).
func (p *Picker) RowCount() int { return len(p.rows) }

// toggle expands or collapses the project on the current row. A session row
// collapses its parent and moves the cursor there, which is the usual
// left-arrow behavior in a tree.
func (p *Picker) toggle(expand bool) bool {
	if p.Selected < 0 || p.Selected >= len(p.rows) {
		return false
	}
	// Collapse is disabled while filtering, since rows are forced open.
	if strings.TrimSpace(p.Query) != "" {
		return false
	}
	r := p.rows[p.Selected]
	if r.kind == rowSession {
		if expand {
			return false
		}
		p.collapsed[r.project] = true
		p.rebuild()
		p.selectProjectRow(r.project)
		return true
	}
	p.collapsed[r.project] = !expand
	p.rebuild()
	p.selectProjectRow(r.project)
	return true
}

func (p *Picker) selectProjectRow(pi int) {
	for i, r := range p.rows {
		if r.kind == rowProject && r.project == pi {
			p.Selected = i
			return
		}
	}
}

func (p *Picker) moveSelection(delta int) {
	if len(p.rows) == 0 {
		return
	}
	next := max(p.Selected+delta, 0)
	if next >= len(p.rows) {
		next = len(p.rows) - 1
	}
	p.Selected = next
}

// accept resumes the selected session, or toggles a project header.
// It reports whether the dialog stays open.
func (p *Picker) accept() (stillOpen bool) {
	if p.Selected < 0 || p.Selected >= len(p.rows) {
		return true
	}
	if p.rows[p.Selected].kind == rowProject {
		pi := p.rows[p.Selected].project
		p.collapsed[pi] = !p.collapsed[pi]
		p.rebuild()
		p.selectProjectRow(pi)
		return true
	}
	sess, ok := p.SelectedSession()
	if !ok {
		return true
	}
	if p.OnAccept != nil {
		p.OnAccept(sess)
	}
	p.Hide()
	return false
}

func (p *Picker) returnFocus(ctx *components.EventContext) {
	if p.FocusReturn != nil {
		ctx.RequestFocus(p.FocusReturn)
	}
}

func (p *Picker) insert(text string) {
	p.Query = p.Query[:p.Cursor] + text + p.Query[p.Cursor:]
	p.Cursor += len(text)
	p.rebuild()
	p.selectFirstSession()
}

// Handle drives dialog interaction: filter editing, tree navigation, accept
// on Enter, and close on Escape.
func (p *Picker) Handle(ctx *components.EventContext, ev xui.Event) {
	if !p.Open {
		return
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		if !e.Press {
			return
		}
		p.handleKey(ctx, e)
	case xui.PasteEvent:
		text := strings.ReplaceAll(e.Text, "\n", " ")
		text = strings.ReplaceAll(text, "\r", " ")
		p.insert(text)
		ctx.ConsumeAndRedraw()
	case xui.MouseEvent:
		if e.Action == xui.MousePress && e.Button == xui.MouseLeft {
			ctx.ConsumeAndRedraw()
		}
	}
}

func (p *Picker) handleKey(ctx *components.EventContext, e xui.KeyEvent) {
	switch e.Code {
	case xui.KeyEscape:
		p.Hide()
		p.returnFocus(ctx)
		ctx.ConsumeAndRedraw()
	case xui.KeyEnter:
		if !p.accept() {
			p.returnFocus(ctx)
		}
		ctx.ConsumeAndRedraw()
	case xui.KeyUp:
		p.moveSelection(-1)
		ctx.ConsumeAndRedraw()
	case xui.KeyDown:
		p.moveSelection(1)
		ctx.ConsumeAndRedraw()
	case xui.KeyLeft:
		// Left collapses when it can, otherwise it moves the text cursor.
		if !p.toggle(false) && p.Cursor > 0 {
			_, size := utf8.DecodeLastRuneInString(p.Query[:p.Cursor])
			p.Cursor -= size
		}
		ctx.ConsumeAndRedraw()
	case xui.KeyRight:
		if !p.toggle(true) && p.Cursor < len(p.Query) {
			_, size := utf8.DecodeRuneInString(p.Query[p.Cursor:])
			p.Cursor += size
		}
		ctx.ConsumeAndRedraw()
	case xui.KeyTab:
		if e.Mods.Has(xui.ModShift) {
			p.moveSelection(-1)
		} else {
			p.moveSelection(1)
		}
		ctx.ConsumeAndRedraw()
	case xui.KeyBackspace:
		if p.Cursor > 0 {
			_, size := utf8.DecodeLastRuneInString(p.Query[:p.Cursor])
			p.Query = p.Query[:p.Cursor-size] + p.Query[p.Cursor:]
			p.Cursor -= size
			p.rebuild()
			p.selectFirstSession()
		}
		ctx.ConsumeAndRedraw()
	case xui.KeyHome:
		p.Cursor = 0
		ctx.ConsumeAndRedraw()
	case xui.KeyEnd:
		p.Cursor = len(p.Query)
		ctx.ConsumeAndRedraw()
	case xui.KeyRune:
		p.handleRune(ctx, e)
	default:
		ctx.Consume = true
	}
}

func (p *Picker) handleRune(ctx *components.EventContext, e xui.KeyEvent) {
	if components.AcceptsCmd(e) {
		switch e.Rune {
		case 'n', 'N':
			p.moveSelection(1)
			ctx.ConsumeAndRedraw()
		case 'p', 'P':
			p.moveSelection(-1)
			ctx.ConsumeAndRedraw()
		case 'r', 'R':
			// Same binding that opens the dialog closes it.
			p.Hide()
			p.returnFocus(ctx)
			ctx.ConsumeAndRedraw()
		}
		return
	}
	if e.Mods.Has(xui.ModAlt) {
		return
	}
	if e.Rune >= 0x20 {
		p.insert(string(e.Rune))
		ctx.ConsumeAndRedraw()
	}
}

// Draw renders the bordered dialog with the filter prompt and the tree.
func (p *Picker) Draw(ctx components.DrawContext) components.Surface {
	th := p.theme()
	maxW, maxH := ctx.Max.Width, ctx.Max.Height
	if maxW <= 0 {
		maxW = 80
	}
	if maxH <= 0 {
		maxH = 24
	}
	if !p.Open {
		return components.Surface{Size: components.Size{Width: maxW, Height: maxH}, Widget: p}
	}
	if p.rows == nil {
		p.rebuild()
	}

	boxW := p.panelWidth(maxW)
	visible, boxH := p.panelHeight(maxH)
	p.syncScroll(visible)

	panel := components.NewSurface(boxW, boxH, p)
	fillStyle := xui.Style{Fg: th.Foreground.Fg}
	for y := range boxH {
		for x := range boxW {
			panel.SetCell(x, y, xui.Cell{Char: " ", Width: 1, Style: fillStyle})
		}
	}
	layout.DrawRoundedBorder(&panel, layout.BorderRounded, th.Border, nil, nil, nil, nil, ctx.Method)

	title := " " + p.title() + " "
	tw := xui.StringWidth(title, ctx.Method)
	tx := max((boxW-tw)/2, 1)
	titleSt := th.Warning
	titleSt.Bold = true
	panel.Print(tx, 0, title, titleSt, ctx.Method)

	p.drawPrompt(&panel, ctx, th, boxW)
	p.drawRows(&panel, ctx, th, boxW, visible)
	p.drawHint(&panel, ctx, th, boxW, boxH)

	out := components.Surface{Size: components.Size{Width: maxW, Height: maxH}, Widget: p}
	ox := max((maxW-boxW)/2, 0)
	oy := max((maxH-boxH)/3, 1)
	out.Children = []components.SubSurface{{
		Origin:  components.Point{X: ox, Y: oy},
		Surface: panel,
		Z:       10,
	}}
	return out
}

func (p *Picker) panelWidth(maxW int) int {
	boxW := p.Width
	if boxW <= 0 {
		boxW = maxW * 4 / 5
		boxW = min(boxW, 88)
		if boxW < 48 {
			boxW = min(maxW, 60)
		}
	}
	if boxW > maxW-2 {
		boxW = maxW - 2
		if boxW < 20 {
			boxW = maxW
		}
	}
	return boxW
}

// panelHeight returns the visible row count and the total panel height.
// Layout is: top border, prompt, rows, bottom border. The key hint is a
// caption on the bottom border, so it needs no row of its own.
func (p *Picker) panelHeight(maxH int) (visible, boxH int) {
	maxVisible := p.maxItems()
	avail := max(maxH-5, 3)
	if maxVisible > avail {
		maxVisible = avail
	}
	visible = min(max(len(p.rows), 1), maxVisible)
	boxH = visible + 3
	if boxH > maxH-2 {
		boxH = maxH - 2
		visible = boxH - 3
		if visible < 1 {
			visible = 1
			boxH = 4
		}
	}
	return visible, boxH
}

// syncScroll keeps the selected row inside the visible window.
func (p *Picker) syncScroll(visible int) {
	if p.Selected < p.scroll {
		p.scroll = p.Selected
	}
	if p.Selected >= p.scroll+visible {
		p.scroll = p.Selected - visible + 1
	}
	maxScroll := max(len(p.rows)-visible, 0)
	if p.scroll > maxScroll {
		p.scroll = maxScroll
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
}

func (p *Picker) drawPrompt(panel *components.Surface, ctx components.DrawContext, th components.Theme, boxW int) {
	const y = 1
	panel.Print(1, y, ">", th.Foreground, ctx.Method)
	avail := max(boxW-5, 1)
	panel.Print(3, y, layout.TruncateToWidth(p.Query, avail, ctx.Method), th.Foreground, ctx.Method)
	curCol := xui.StringWidth(p.Query[:min(p.Cursor, len(p.Query))], ctx.Method)
	curCol = max(min(curCol, avail-1), 0)
	panel.Cursor = &components.Point{X: 3 + curCol, Y: y}
}

func (p *Picker) drawRows(
	panel *components.Surface,
	ctx components.DrawContext,
	th components.Theme,
	boxW, visible int,
) {
	const listY = 2
	if len(p.rows) == 0 {
		msg := "No sessions found"
		if strings.TrimSpace(p.Query) != "" {
			msg = "No sessions match " + p.Query
		}
		panel.Print(2, listY, layout.TruncateToWidth(msg, boxW-3, ctx.Method), th.Muted, ctx.Method)
		return
	}
	now := p.now()
	for i := range visible {
		ri := i + p.scroll
		if ri < 0 || ri >= len(p.rows) {
			break
		}
		p.drawRow(panel, ctx, th, boxW, listY+i, ri, now)
	}
}

func (p *Picker) drawRow(
	panel *components.Surface,
	ctx components.DrawContext,
	th components.Theme,
	boxW, y, ri int,
	now time.Time,
) {
	r := p.rows[ri]
	sel := ri == p.Selected
	base := th.Foreground
	muted := th.Muted
	if sel {
		bg := th.SelectionBg.Bg
		for x := 1; x < boxW-1; x++ {
			panel.SetCell(x, y, xui.Cell{Char: " ", Width: 1, Style: xui.Style{Bg: bg}})
		}
		fg := th.SelectionFg
		fg.Bg = bg
		base = fg
		muted = fg
	}

	if r.kind == rowProject {
		p.drawProjectRow(panel, ctx, boxW, y, r, rowStyle{base: base, muted: muted, sel: sel, theme: th})
		return
	}

	sessions := p.visibleSessions(r.project)
	if r.session >= len(sessions) {
		return
	}
	s := sessions[r.session]

	connector := "├── "
	if r.isLast {
		connector = "╰── "
	}
	x := 2
	panel.Print(x, y, connector, muted, ctx.Method)
	x += xui.StringWidth(connector, ctx.Method)

	idSt := base
	idSt.Bold = true
	id := shortID(s.ID)
	panel.Print(x, y, id, idSt, ctx.Method)
	x += xui.StringWidth(id, ctx.Method) + 2

	when := formatMtime(s.Mtime, now)
	panel.Print(x, y, when, muted, ctx.Method)
	x += max(xui.StringWidth(when, ctx.Method), 9) + 2

	preview := s.Preview
	if strings.TrimSpace(preview) == "" {
		preview = "(no preview)"
	}
	avail := max(boxW-1-x, 1)
	panel.Print(x, y, layout.TruncateToWidth(preview, avail, ctx.Method), base, ctx.Method)
}

// rowStyle carries the resolved styling for one drawn row.
type rowStyle struct {
	base  xui.Style
	muted xui.Style
	sel   bool
	theme components.Theme
}

func (p *Picker) drawProjectRow(
	panel *components.Surface,
	ctx components.DrawContext,
	boxW, y int,
	r row,
	st rowStyle,
) {
	base, muted, sel, th := st.base, st.muted, st.sel, st.theme
	proj := p.Projects[r.project]
	marker := "▸ "
	if !p.collapsed[r.project] || strings.TrimSpace(p.Query) != "" {
		marker = "▾ "
	}
	x := 1
	panel.Print(x, y, marker, muted, ctx.Method)
	x += 2

	labelSt := base
	labelSt.Bold = true
	if !sel && proj.Current {
		labelSt = th.Keybind
	}
	panel.Print(x, y, proj.Label, labelSt, ctx.Method)
	x += xui.StringWidth(proj.Label, ctx.Method) + 1

	n := len(p.visibleSessions(r.project))
	tag := fmt.Sprintf("(%d)", n)
	if proj.Current {
		tag = fmt.Sprintf("(this project, %d)", n)
	}
	avail := max(boxW-1-x, 1)
	panel.Print(x, y, layout.TruncateToWidth(tag, avail, ctx.Method), muted, ctx.Method)
}

func (*Picker) drawHint(
	panel *components.Surface,
	ctx components.DrawContext,
	th components.Theme,
	boxW, boxH int,
) {
	// Rendered as a caption on the bottom border, like the title on top.
	hint := " ↑↓ move  ←→ fold  ⏎ resume  esc close "
	if xui.StringWidth(hint, ctx.Method) > boxW-2 {
		hint = " ⏎ resume  esc close "
	}
	y := boxH - 1
	w := xui.StringWidth(hint, ctx.Method)
	x := max((boxW-w)/2, 1)
	panel.Print(x, y, layout.TruncateToWidth(hint, boxW-2, ctx.Method), th.Muted, ctx.Method)
}
