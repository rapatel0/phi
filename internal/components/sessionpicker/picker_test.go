package sessionpicker

import (
	"strings"
	"testing"
	"time"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
)

var testNow = time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

// samplePicker builds a two-project picker: the current project with two
// sessions, and an older project with one.
func samplePicker() *Picker {
	return &Picker{
		Theme: components.DefaultTheme(),
		Now:   func() time.Time { return testNow },
		Projects: []Project{
			{
				Label:   "alpha",
				Cwd:     "/Users/me/alpha",
				Current: true,
				Sessions: []Session{
					{ID: "4f2a1b3c11", File: "/s/a1.jsonl", Preview: "fix the oauth tool names", Mtime: testNow},
					{
						ID:      "9c7a363822",
						File:    "/s/a2.jsonl",
						Preview: "rename phi to alpha",
						Mtime:   testNow.Add(-3 * time.Hour),
					},
				},
			},
			{
				Label: "repos",
				Cwd:   "/Users/me/repos",
				Sessions: []Session{
					{ID: "33a2c34733", File: "/s/r1.jsonl", Preview: "scratch notes", Mtime: testNow.AddDate(0, 0, -2)},
				},
			},
		},
	}
}

func press(p *Picker, ctx *components.EventContext, code xui.KeyCode) {
	p.Handle(ctx, xui.KeyEvent{Code: code, Press: true})
}

func typeText(p *Picker, ctx *components.EventContext, s string) {
	for _, r := range s {
		p.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: r, Press: true})
	}
}

func TestShowExpandsCurrentProjectOnly(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	// alpha header + 2 sessions + repos header = 4 rows.
	if p.RowCount() != 4 {
		t.Fatalf("rows=%d want 4", p.RowCount())
	}
	if p.collapsed[0] {
		t.Fatal("current project must start expanded")
	}
	if !p.collapsed[1] {
		t.Fatal("other project must start collapsed")
	}
}

func TestShowSelectsFirstSessionNotHeader(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	s, ok := p.SelectedSession()
	if !ok {
		t.Fatal("cursor must start on a session row")
	}
	if s.ID != "4f2a1b3c11" {
		t.Fatalf("selected %q", s.ID)
	}
}

func TestSingleProjectStartsExpanded(t *testing.T) {
	p := samplePicker()
	only := []Project{p.Projects[1]} // not marked Current
	p.Show(only)

	if p.collapsed[0] {
		t.Fatal("a lone project must be expanded")
	}
	if p.RowCount() != 2 {
		t.Fatalf("rows=%d want header + 1 session", p.RowCount())
	}
}

func TestEnterResumesSelectedSession(t *testing.T) {
	p := samplePicker()
	var got Session
	p.OnAccept = func(s Session) { got = s }
	p.Show(p.Projects)

	ctx := &components.EventContext{}
	press(p, ctx, xui.KeyEnter)

	if got.ID != "4f2a1b3c11" {
		t.Fatalf("accepted %q", got.ID)
	}
	if got.File != "/s/a1.jsonl" {
		t.Fatalf("file %q — resume needs the path", got.File)
	}
	if p.Open {
		t.Fatal("dialog must close after accept")
	}
}

func TestEnterOnProjectHeaderTogglesInsteadOfResuming(t *testing.T) {
	p := samplePicker()
	accepted := false
	p.OnAccept = func(Session) { accepted = true }
	p.Show(p.Projects)

	ctx := &components.EventContext{}
	press(p, ctx, xui.KeyUp) // onto the alpha header
	press(p, ctx, xui.KeyEnter)

	if accepted {
		t.Fatal("a header must not resume")
	}
	if !p.Open {
		t.Fatal("dialog must stay open")
	}
	if !p.collapsed[0] {
		t.Fatal("header Enter must collapse")
	}
}

func TestExpandCollapsedProjectRevealsSessions(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)
	before := p.RowCount()

	ctx := &components.EventContext{}
	// Move to the repos header (last row) and expand it.
	for range p.RowCount() {
		press(p, ctx, xui.KeyDown)
	}
	press(p, ctx, xui.KeyRight)

	if p.RowCount() != before+1 {
		t.Fatalf("rows=%d want %d after expand", p.RowCount(), before+1)
	}
}

func TestLeftOnSessionCollapsesParent(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	ctx := &components.EventContext{}
	press(p, ctx, xui.KeyLeft)

	if !p.collapsed[0] {
		t.Fatal("left on a session must collapse its project")
	}
	if _, ok := p.SelectedSession(); ok {
		t.Fatal("cursor must land on the project header")
	}
}

func TestFilterMatchesPreview(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	ctx := &components.EventContext{}
	typeText(p, ctx, "oauth")

	// One project header plus its single matching session.
	if p.RowCount() != 2 {
		t.Fatalf("rows=%d want 2", p.RowCount())
	}
	s, ok := p.SelectedSession()
	if !ok || s.ID != "4f2a1b3c11" {
		t.Fatalf("selected %+v ok=%v", s, ok)
	}
}

func TestFilterMatchesSessionID(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	ctx := &components.EventContext{}
	typeText(p, ctx, "33a2")

	s, ok := p.SelectedSession()
	if !ok || s.ID != "33a2c34733" {
		t.Fatalf("selected %+v ok=%v", s, ok)
	}
}

func TestFilterRevealsCollapsedProjects(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)
	if !p.collapsed[1] {
		t.Fatal("precondition: repos starts collapsed")
	}

	ctx := &components.EventContext{}
	typeText(p, ctx, "scratch")

	// The hit lives in a collapsed project, so filtering must show it anyway.
	s, ok := p.SelectedSession()
	if !ok || s.ID != "33a2c34733" {
		t.Fatalf("filter must reach collapsed projects, got %+v ok=%v", s, ok)
	}
}

func TestFilterMatchesProjectLabel(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	ctx := &components.EventContext{}
	typeText(p, ctx, "repos")

	if p.RowCount() != 2 {
		t.Fatalf("rows=%d want repos header + 1 session", p.RowCount())
	}
}

func TestFilterNoMatches(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	ctx := &components.EventContext{}
	typeText(p, ctx, "zzzznope")

	if p.RowCount() != 0 {
		t.Fatalf("rows=%d want 0", p.RowCount())
	}
	if _, ok := p.SelectedSession(); ok {
		t.Fatal("no session may be selected")
	}
	// Enter on an empty list must not crash or close.
	press(p, &components.EventContext{}, xui.KeyEnter)
	if !p.Open {
		t.Fatal("dialog must stay open with no matches")
	}
}

func TestBackspaceRestoresRows(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)
	before := p.RowCount()

	ctx := &components.EventContext{}
	typeText(p, ctx, "oauth")
	for range len("oauth") {
		press(p, ctx, xui.KeyBackspace)
	}

	if p.Query != "" {
		t.Fatalf("query %q", p.Query)
	}
	if p.RowCount() != before {
		t.Fatalf("rows=%d want %d", p.RowCount(), before)
	}
}

func TestEscapeCloses(t *testing.T) {
	p := samplePicker()
	closed := false
	p.OnClose = func() { closed = true }
	p.Show(p.Projects)

	press(p, &components.EventContext{}, xui.KeyEscape)

	if p.Open || !closed {
		t.Fatalf("open=%v closed=%v", p.Open, closed)
	}
}

func TestCtrlRTogglesClosed(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	p.Handle(&components.EventContext{}, xui.KeyEvent{
		Code: xui.KeyRune, Rune: 'r', Mods: xui.ModCtrl, Press: true,
	})

	if p.Open {
		t.Fatal("Ctrl+R must close the dialog")
	}
}

func TestNavigationClampsAtEdges(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	ctx := &components.EventContext{}
	for range 20 {
		press(p, ctx, xui.KeyUp)
	}
	if p.Selected != 0 {
		t.Fatalf("selected=%d want 0", p.Selected)
	}
	for range 50 {
		press(p, ctx, xui.KeyDown)
	}
	if p.Selected != p.RowCount()-1 {
		t.Fatalf("selected=%d want %d", p.Selected, p.RowCount()-1)
	}
}

func TestEmptyPickerDraws(t *testing.T) {
	p := &Picker{Theme: components.DefaultTheme(), Now: func() time.Time { return testNow }}
	p.Show(nil)

	s := p.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode})
	if len(s.Children) != 1 {
		t.Fatalf("children=%d", len(s.Children))
	}
	if !strings.Contains(panelText(s.Children[0].Surface), "No sessions found") {
		t.Fatal("empty state must be shown")
	}
}

func TestDrawShowsProjectsAndSessions(t *testing.T) {
	p := samplePicker()
	p.Show(p.Projects)

	s := p.Draw(components.DrawContext{Max: components.Size{Width: 90, Height: 24}, Method: xui.WidthUnicode})
	text := panelText(s.Children[0].Surface)

	for _, want := range []string{"Sessions", "alpha", "this project", "4f2a1b3c", "fix the oauth tool names", "repos"} {
		if !strings.Contains(text, want) {
			t.Fatalf("panel missing %q\n%s", want, text)
		}
	}
	// A collapsed project must not leak its sessions.
	if strings.Contains(text, "scratch notes") {
		t.Fatal("collapsed project must hide its sessions")
	}
}

func TestDrawScrollsToSelection(t *testing.T) {
	many := make([]Session, 40)
	for i := range many {
		many[i] = Session{
			ID:      strings.Repeat("a", 7) + string(rune('0'+i%10)),
			Preview: "session " + string(rune('0'+i%10)),
			Mtime:   testNow,
		}
	}
	p := &Picker{
		Theme:    components.DefaultTheme(),
		Now:      func() time.Time { return testNow },
		MaxItems: 6,
		Projects: []Project{{Label: "big", Current: true, Sessions: many}},
	}
	p.Show(p.Projects)

	ctx := &components.EventContext{}
	for range 30 {
		press(p, ctx, xui.KeyDown)
	}
	p.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode})

	if p.scroll == 0 {
		t.Fatal("list must scroll to keep the selection visible")
	}
	if p.Selected < p.scroll || p.Selected >= p.scroll+6 {
		t.Fatalf("selected=%d outside window [%d,%d)", p.Selected, p.scroll, p.scroll+6)
	}
}

func TestFormatMtime(t *testing.T) {
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"today", testNow.Add(-2 * time.Hour), "10:00"},
		{"yesterday", testNow.AddDate(0, 0, -1), "Yesterday"},
		{"this year", testNow.AddDate(0, 0, -30), "Jul 25"},
		{"older", testNow.AddDate(-1, 0, 0), "2025-08-24"},
		{"zero", time.Time{}, ""},
	}
	for _, c := range cases {
		if got := formatMtime(c.in, testNow); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("4f2a1b3c11223344"); got != "4f2a1b3c" {
		t.Fatalf("got %q", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
}

// panelText flattens a surface into newline-joined rows.
func panelText(s components.Surface) string {
	var b strings.Builder
	for y := 0; y < s.Size.Height; y++ {
		for x := 0; x < s.Size.Width; x++ {
			ch := s.Buffer[y*s.Size.Width+x].Char
			if ch == "" {
				ch = " "
			}
			b.WriteString(ch)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
