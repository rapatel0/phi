package termimg

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/phi/internal/llm"
)

func TestSupportedEnv(t *testing.T) {
	t.Setenv("PHI_KITTY_GRAPHICS", "0")
	t.Setenv("KITTY_WINDOW_ID", "1")
	if Supported() {
		t.Fatal("explicit off")
	}
	t.Setenv("PHI_KITTY_GRAPHICS", "1")
	t.Setenv("TMUX", "1")
	if !Supported() {
		t.Fatal("explicit on")
	}
}

func TestSupportedKitty(t *testing.T) {
	t.Setenv("PHI_KITTY_GRAPHICS", "")
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_WINDOW_ID", "42")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")
	t.Setenv("GHOSTTY_BIN", "")
	t.Setenv("TERM", "xterm-256color")
	if !Supported() {
		t.Fatal("KITTY_WINDOW_ID")
	}
}

func TestEncodeTransmitPNG(t *testing.T) {
	s := EncodeTransmit(3, llm.Image{MIME: "image/png", Data: []byte{1, 2, 3, 4}})
	if !strings.Contains(s, "\x1b_Ga=t,f=100,i=3,m=0,q=2;") {
		t.Fatalf("header %q", s)
	}
	if !strings.HasSuffix(s, "\x1b\\") {
		t.Fatal("ST")
	}
}

func TestCellSize(t *testing.T) {
	c, r := CellSize(llm.Image{}, 40)
	if c < 1 || r < minRows {
		t.Fatalf("cols=%d rows=%d", c, r)
	}
}
