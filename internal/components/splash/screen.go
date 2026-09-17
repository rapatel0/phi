package splash

import (
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
)

// Screen is the splash screen: animated alpha mark + intro copy.
// Shown when the transcript is empty at startup.
type Screen struct {
	Mark   *AlphaMark
	Sphere *Sphere // compatibility for callers that still provide a sphere
	Theme  components.Theme
	Time   float64
	// Brand is the product name in the hero line (default "alpha").
	Brand string
	Hint  string // optional tip under the help line; empty uses the default
}

func (w *Screen) brand() string {
	if w.Brand == "" {
		return "Alpha"
	}
	return w.Brand
}

func (w *Screen) hintLines() []string {
	if w.Hint != "" {
		return wrapHint(w.Hint, 48)
	}
	return []string{
		"Type a message below and press Enter to start",
	}
}

func wrapHint(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	var cur string
	for _, word := range words {
		if cur == "" {
			cur = word
			continue
		}
		if len(cur)+1+len(word) > width {
			lines = append(lines, cur)
			cur = word
			continue
		}
		cur += " " + word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// Handle forwards events to the animated mark.
func (w *Screen) Handle(ctx *components.EventContext, ev xui.Event) {
	if w.Mark != nil {
		w.Mark.Handle(ctx, ev)
	}
	if w.Sphere != nil {
		w.Sphere.Handle(ctx, ev)
	}
}

// Draw renders the centered alpha mark plus brand, help, and hint copy.
func (w *Screen) Draw(ctx components.DrawContext) components.Surface {
	maxW, maxH := ctx.Max.Width, ctx.Max.Height
	if maxW <= 0 {
		maxW = 80
	}
	if maxH <= 0 {
		maxH = 24
	}
	root := components.Surface{Size: components.Size{Width: maxW, Height: maxH}, Widget: w}

	th := w.Theme
	if th == (components.Theme{}) {
		th = components.DefaultTheme()
	}

	markW, markH := 30, 15
	if maxW < markW+24 {
		markW = maxW / 3
	}
	if maxH < markH+4 {
		markH = maxH - 4
	}
	markW = max(7, markW)
	markH = max(7, markH)
	var markSurf components.Surface
	if w.Mark != nil {
		w.Mark.Width, w.Mark.Height, w.Mark.Time = markW, markH, w.Time
		markSurf = w.Mark.Draw(ctx.WithConstraints(components.Size{}, components.Size{Width: markW, Height: markH}))
	} else if w.Sphere != nil {
		// Keep the old field usable for callers that construct Screen directly.
		w.Sphere.Width, w.Sphere.Height = markW, markH
		markSurf = w.Sphere.Draw(ctx.WithConstraints(components.Size{}, components.Size{Width: markW, Height: markH}))
	} else {
		mark := &AlphaMark{Width: markW, Height: markH, Time: w.Time}
		markSurf = mark.Draw(ctx.WithConstraints(components.Size{}, components.Size{Width: markW, Height: markH}))
	}

	const gap = 2
	stacked := maxW < 88
	textW := maxW - 4
	if !stacked {
		textW = maxW - markW - gap - 4
		textW = max(textW, 20)
		textW = min(textW, 50)
	}

	// Brand near-white; only the palette shortcut / ! carry the accent punch.
	brand := xui.Style{Fg: xui.RGBColor(0xe8, 0xec, 0xf2), Bold: true}
	if th.Foreground.Fg.Kind == xui.ColorRGB {
		brand = xui.Style{Fg: th.Foreground.Fg, Bold: true}
	}
	helpKey := th.Success
	if helpKey == (xui.Style{}) {
		helpKey = th.Keybind
	}
	if helpKey == (xui.Style{}) {
		helpKey = xui.Style{Fg: xui.RGBColor(0x7d, 0xc3, 0xa0), Bold: true}
	}
	// Secondary copy: theme muted without Dim. ANSI Dim + bright-black
	// (Terminal IndexedColor 8) is nearly invisible on dark backgrounds.
	body := splashBodyStyle(th)

	lines := []struct {
		spans []components.Span
	}{
		{spans: []components.Span{{Text: w.brand(), Style: brand}}},
		{spans: []components.Span{{Text: "terminal coding agent", Style: body}}},
		{spans: nil}, // blank
		{spans: []components.Span{
			{Text: components.PaletteHint(), Style: helpKey},
			{Text: " command palette", Style: body},
			{Text: ", ", Style: body},
			{Text: "!", Style: helpKey},
			{Text: " run a shell command", Style: body},
		}},
	}
	for _, h := range w.hintLines() {
		lines = append(lines, struct{ spans []components.Span }{spans: []components.Span{{Text: h, Style: body}}})
	}

	textH := len(lines)
	textH = max(textH, 1)
	textSurf := components.NewSurface(textW, textH, nil)
	for y, line := range lines {
		if line.spans == nil {
			continue
		}
		components.PaintSpans(&textSurf, 0, y, line.spans, ctx.Method)
	}

	blockW := markW + gap + textW
	blockH := max(markH, textH)
	if stacked {
		blockW = max(markW, textW)
		blockH = markH + gap + textH
	}
	ox := (maxW - blockW) / 2
	oy := (maxH - blockH) / 2
	if ox < 0 {
		ox = 0
	}
	if oy < 0 {
		oy = 0
	}
	markOX := ox
	markOY := oy
	textOX := ox + markW + gap
	textOY := oy + (blockH-textH)/2
	if stacked {
		markOX = ox + (blockW-markW)/2
		textOX = ox + (blockW-textW)/2
		textOY = oy + markH + gap
	}
	markOX = max(markOX, 0)
	markOY = max(markOY, 0)
	textOX = max(textOX, 0)
	textOY = max(textOY, 0)

	root.Children = []components.SubSurface{
		{Origin: components.Point{X: markOX, Y: markOY}, Surface: markSurf},
		{Origin: components.Point{X: textOX, Y: textOY}, Surface: textSurf},
	}
	return root
}

// splashBodyStyle is readable secondary copy for the hero — theme muted
// without Dim, lifting ANSI bright-black to a mid gray.
func splashBodyStyle(th components.Theme) xui.Style {
	st := th.Muted
	st.Dim = false
	if st.Fg.Kind == xui.ColorIndex && st.Fg.Index <= 8 {
		st.Fg = xui.IndexedColor(245)
	}
	if st.Fg.Kind == 0 {
		st.Fg = xui.RGBColor(0xa8, 0xb2, 0xc0)
	}
	return st
}
