package splash

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
)

func TestAlphaMarkDrawContainsAlpha(t *testing.T) {
	mark := &AlphaMark{Width: 21, Height: 11}
	surf := mark.Draw(components.DrawContext{Max: components.Size{Width: 21, Height: 11}, Method: xui.WidthUnicode})
	for _, cell := range surf.Buffer {
		if cell.Char == "α" {
			return
		}
	}
	t.Fatal("mark does not contain the alpha symbol")
}

func TestAlphaMarkMovesSignal(t *testing.T) {
	mark := &AlphaMark{Width: 21, Height: 11}
	first := mark.Draw(components.DrawContext{Max: components.Size{Width: 21, Height: 11}, Method: xui.WidthUnicode})
	mark.Time = 0.7
	second := mark.Draw(components.DrawContext{Max: components.Size{Width: 21, Height: 11}, Method: xui.WidthUnicode})
	if sameSurface(first, second) {
		t.Fatal("mark did not change between animation frames")
	}
}

func sameSurface(a, b components.Surface) bool {
	if a.Size != b.Size || len(a.Buffer) != len(b.Buffer) {
		return false
	}
	for i := range a.Buffer {
		if a.Buffer[i] != b.Buffer[i] {
			return false
		}
	}
	return true
}
