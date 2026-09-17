package splash

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
)

func TestScreenDrawLayout(t *testing.T) {
	w := Screen{
		Sphere: &Sphere{Time: 1},
		Theme:  components.DefaultTheme(),
		Brand:  "alpha",
	}
	surf := w.Draw(components.DrawContext{
		Max:    components.Size{Width: 100, Height: 40},
		Method: xui.WidthUnicode,
	})
	if len(surf.Children) != 2 {
		t.Fatalf("children = %d, want 2 (sphere + text)", len(surf.Children))
	}
}

func TestScreenDefaultUsesAlphaMark(t *testing.T) {
	w := Screen{Theme: components.DefaultTheme()}
	surf := w.Draw(components.DrawContext{
		Max:    components.Size{Width: 100, Height: 40},
		Method: xui.WidthUnicode,
	})
	if len(surf.Children) != 2 {
		t.Fatalf("children = %d, want 2", len(surf.Children))
	}
	for _, cell := range surf.Children[0].Surface.Buffer {
		if cell.Char == "α" {
			return
		}
	}
	t.Fatal("default mark does not contain the alpha symbol")
}
