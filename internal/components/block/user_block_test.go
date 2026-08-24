package block

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/llm"
)

func TestUserBlockReservesKittyRows(t *testing.T) {
	t.Setenv("ALPHA_KITTY_GRAPHICS", "1")
	ub := &UserBlock{
		Text:  "see this",
		Theme: components.DefaultTheme(),
		Images: []llm.Image{{
			MIME: "image/png",
			Data: []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 1, 2, 3},
		}},
	}
	s := ub.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 40}, Method: xui.WidthUnicode})
	if len(s.Graphics) != 1 {
		t.Fatalf("graphics %d", len(s.Graphics))
	}
	if s.Size.Height <= 1 {
		t.Fatalf("height %d, want reserved image rows", s.Size.Height)
	}
}

func TestExtractUserMessageSkipsRuleChrome(t *testing.T) {
	ub := &UserBlock{Text: "13个技能 你把这个 skills挪动过去", Theme: components.DefaultTheme()}
	s := ub.Draw(components.DrawContext{Max: components.Size{Width: 60, Height: 5}, Method: xui.WidthUnicode})
	got := components.ExtractSurfaceText(s, 0, 0, 59, 0)
	if strings.Contains(got, "▎") {
		t.Fatalf("selection must not include rule chrome: %q", got)
	}
	if got != ub.Text {
		t.Fatalf("got %q, want %q", got, ub.Text)
	}
}
