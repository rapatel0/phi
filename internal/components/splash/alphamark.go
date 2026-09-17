package splash

import (
	"math"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
)

// AlphaMark is the animated Alpha symbol for the welcome screen.
// Drive animation by advancing Time (seconds) each frame with App.Anim.
type AlphaMark struct {
	Width  int
	Height int
	Time   float64
	Fast   bool
}

// Handle keeps the mark passive while the parent handles terminal input.
func (*AlphaMark) Handle(_ *components.EventContext, _ xui.Event) {}

// Draw renders a framed alpha mark with a moving signal sweep and orbiting points.
func (o *AlphaMark) Draw(ctx components.DrawContext) components.Surface {
	o.ensureSize(ctx)
	s := components.NewSurface(o.Width, o.Height, o)

	muted := xui.Style{Fg: xui.RGBColor(0x1b, 0x78, 0x62)}
	line := xui.Style{Fg: xui.RGBColor(0x2b, 0xb9, 0x88)}
	accent := xui.Style{Fg: xui.RGBColor(0x52, 0xff, 0xa8), Bold: true}
	bright := xui.Style{Fg: xui.RGBColor(0xc9, 0xff, 0xe2), Bold: true}

	for x := 1; x < o.Width-1; x++ {
		s.SetCell(x, 0, glyph("─", muted))
		s.SetCell(x, o.Height-1, glyph("─", muted))
	}
	for y := 1; y < o.Height-1; y++ {
		s.SetCell(0, y, glyph("│", muted))
		s.SetCell(o.Width-1, y, glyph("│", muted))
	}
	s.SetCell(0, 0, glyph("╭", line))
	s.SetCell(o.Width-1, 0, glyph("╮", line))
	s.SetCell(0, o.Height-1, glyph("╰", line))
	s.SetCell(o.Width-1, o.Height-1, glyph("╯", line))

	cx := float64(o.Width-1) / 2
	cy := float64(o.Height-1) / 2
	radiusX := math.Max(3, float64(o.Width)/2-3)
	radiusY := math.Max(2, float64(o.Height)/2-2)
	phase := o.Time
	if o.Fast {
		phase *= 1.8
	}

	// The ellipse gives the mark a quiet instrument-panel feel. Its brighter
	// points move with time, which keeps the animation legible at low FPS.
	for y := 1; y < o.Height-1; y++ {
		for x := 1; x < o.Width-1; x++ {
			dx := (float64(x) - cx) / radiusX
			dy := (float64(y) - cy) / radiusY
			distance := math.Abs(math.Sqrt(dx*dx+dy*dy) - 1)
			if distance > 0.13 {
				continue
			}
			angle := math.Atan2(dy, dx)
			brightness := 0.5 + 0.5*math.Sin(angle*4-phase*2)
			style := muted
			char := "·"
			if brightness > 0.72 {
				style, char = line, "∘"
			}
			if brightness > 0.94 {
				style, char = accent, "◆"
			}
			s.SetCell(x, y, glyph(char, style))
		}
	}

	// A scan line moves through the panel without disturbing the central mark.
	scan := 1 + int(math.Mod(math.Max(0, phase)*4, float64(max(1, o.Height-2))))
	for x := 2; x < o.Width-2; x++ {
		s.SetCell(x, scan, glyph("┄", line))
	}

	// The Greek alpha remains the stable focal point while the surrounding
	// signal moves. It also works on terminals that do not support true color.
	alphaX := int(cx)
	alphaY := int(cy)
	s.SetCell(alphaX-1, alphaY, glyph("╲", accent))
	s.SetCell(alphaX, alphaY, glyph("α", bright))
	s.SetCell(alphaX+1, alphaY, glyph("╱", accent))
	if alphaY > 1 {
		s.SetCell(alphaX, alphaY-1, glyph("·", line))
	}
	if alphaY+1 < o.Height-1 {
		s.SetCell(alphaX, alphaY+1, glyph("─", accent))
	}
	return s
}

func (o *AlphaMark) ensureSize(ctx components.DrawContext) {
	if o.Width <= 0 {
		o.Width = 30
	}
	if o.Height <= 0 {
		o.Height = 15
	}
	if ctx.Max.Width > 0 && o.Width > ctx.Max.Width {
		o.Width = ctx.Max.Width
	}
	if ctx.Max.Height > 0 && o.Height > ctx.Max.Height {
		o.Height = ctx.Max.Height
	}
	o.Width = max(7, o.Width)
	o.Height = max(7, o.Height)
}

func glyph(char string, style xui.Style) xui.Cell {
	return xui.Cell{Char: char, Width: 1, Style: style}
}
