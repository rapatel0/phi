package components

import (
	"unicode"
	"unicode/utf8"

	"github.com/pulseaiclub/xui"
)

// TypedRune is the character to insert for a key. Kitty CSI-u with flag 8
// reports Shift+minus as '-' plus Shift, not '_'. Use Text when the
// terminal sent the produced character. Private-use Shift/Cmd codes are 0.
func TypedRune(e xui.KeyEvent) rune {
	r := e.Rune
	if e.Text != "" {
		if tr, _ := utf8.DecodeRuneInString(e.Text); tr >= 0x20 {
			r = tr
		}
	}
	if unicode.Is(unicode.Co, r) || unicode.IsControl(r) {
		return 0
	}
	if !e.Mods.Has(xui.ModShift) {
		return r
	}
	if s, ok := usShift[r]; ok {
		return s
	}
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

var usShift = map[rune]rune{
	'`':  '~',
	'1':  '!',
	'2':  '@',
	'3':  '#',
	'4':  '$',
	'5':  '%',
	'6':  '^',
	'7':  '&',
	'8':  '*',
	'9':  '(',
	'0':  ')',
	'-':  '_',
	'=':  '+',
	'[':  '{',
	']':  '}',
	'\\': '|',
	';':  ':',
	'\'': '"',
	',':  '<',
	'.':  '>',
	'/':  '?',
}
