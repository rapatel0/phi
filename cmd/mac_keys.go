package main

import (
	"os"
	"sync"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
)

// enableMacKeyboard asks the terminal to report Cmd+letter as Super.
//
// Call this after xui has pushed Kitty flags 7. SET flags 11
// (disambiguate + events + report-all-keys) without flag 4 (alternate keys).
// Flag 4 sends CSI-u as "97:65;2u". xui Atoi fails on the colon, so Shift+A
// inserts nothing. Flag 8 is still required for Cmd+letter to arrive.
func enableMacKeyboard(vx *xui.XUI) {
	if vx == nil || components.Keys.Name != "cmd" {
		return
	}
	_, _ = vx.WriteRaw([]byte("\x1b[=11u"))
}

// afterTerminalQuery runs once xui has pushed Kitty flags 7 and maybe
// Unicode mode 2027. Mode 2027 can give ASCII "_" width 0 on Ghostty, so
// the glyph combines with the previous letter. The renderer already
// measures width itself.
func afterTerminalQuery(vx *xui.XUI) {
	enableMacKeyboard(vx)
	if vx != nil {
		_, _ = vx.WriteRaw([]byte("\x1b[?2027l"))
	}
}

func writeTTY(seq string) {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		_, _ = os.Stdout.WriteString(seq)
		return
	}
	_, _ = f.WriteString(seq)
	_ = f.Close()
}

// restoreMacKeyboard turns Kitty keyboard mode off. Write to /dev/tty after
// xui.Close so a leftover flag 8 cannot leak CSI-u into the shell.
func restoreMacKeyboard() {
	if components.Keys.Name != "cmd" {
		return
	}
	writeTTY("\x1b[=0u")
}

func closeTerminal(vx *xui.XUI) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if vx != nil {
				_ = vx.Close()
			}
			restoreMacKeyboard()
		})
	}
}
