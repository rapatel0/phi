package block

import (
	"github.com/pulseaiclub/xui"

	components "github.com/pulseaiclub/phi/internal/components"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/termimg"
)

// UserBlock renders a user prompt with success left rule + italic.
type UserBlock struct {
	Text   string
	Images []llm.Image
	Theme  components.Theme
}

func (userBlock *UserBlock) theme() components.Theme {
	if userBlock.Theme.Success.Fg.Kind == 0 && userBlock.Theme.Foreground.Fg.Kind == 0 {
		return components.DefaultTheme()
	}
	return userBlock.Theme
}

// Handle is a no-op; the user prompt is not interactive.
func (*UserBlock) Handle(_ *components.EventContext, _ xui.Event) {}

// CopyText returns the prompt body (without the left rule).
func (userBlock *UserBlock) CopyText() string { return userBlock.Text }

// Draw renders the prompt text with a success left rule and italic body.
func (userBlock *UserBlock) Draw(ctx components.DrawContext) components.Surface {
	th := userBlock.theme()
	w := ctx.Max.Width
	if w <= 0 {
		w = 40
	}
	body := th.Foreground
	body.Italic = true
	rule := th.Success
	innerW := w - 2
	innerW = max(innerW, 1)
	lines := components.WrapSpans([]components.Span{{Text: userBlock.Text, Style: body}}, innerW, ctx.Method)
	type box struct {
		img        llm.Image
		cols, rows int
	}
	var boxes []box
	if termimg.Supported() {
		for _, img := range userBlock.Images {
			if len(img.Data) == 0 {
				continue
			}
			c, r := termimg.CellSize(img, innerW)
			boxes = append(boxes, box{img: img, cols: c, rows: r})
		}
	}
	imgH := 0
	for _, b := range boxes {
		imgH += b.rows
	}
	h := len(lines) + imgH
	h = max(h, 1)
	s := components.NewSurface(w, h, userBlock)
	y := 0
	for _, b := range boxes {
		for row := 0; row < b.rows; row++ {
			s.SetCell(0, y+row, xui.Cell{Char: "▎", Width: 1, Style: rule})
			for x := 2; x < w && x < 2+b.cols; x++ {
				s.SetCell(x, y+row, xui.Cell{Char: " ", Width: 1, Style: body})
			}
		}
		s.Graphics = append(s.Graphics, components.Graphic{
			X: 2, Y: y, Cols: b.cols, Rows: b.rows,
			MIME: b.img.MIME, Data: b.img.Data, Filename: b.img.Filename,
		})
		y += b.rows
	}
	for _, line := range lines {
		// ▎ tiles full cell height; "|" leaves gaps between wrapped rows.
		s.SetCell(0, y, xui.Cell{Char: "▎", Width: 1, Style: rule})
		components.PaintSpans(&s, 2, y, line, ctx.Method)
		y++
	}
	return s
}
