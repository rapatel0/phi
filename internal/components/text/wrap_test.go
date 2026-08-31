package text

import (
	"testing"

	"github.com/pulseaiclub/xui"
)

func TestOffsetAtVisualWrapsWithoutNewline(t *testing.T) {
	s := "abcdefghij"
	// width 4 → visual lines abcd efgh ij
	off := OffsetAtVisual(s, 1, 0, 4, xui.WidthUnicode)
	if off != 4 {
		t.Fatalf("line 1 col 0 = %d, want 4", off)
	}
	off = OffsetAtVisual(s, 0, 2, 4, xui.WidthUnicode)
	if off != 2 {
		t.Fatalf("line 0 col 2 = %d, want 2", off)
	}
}
