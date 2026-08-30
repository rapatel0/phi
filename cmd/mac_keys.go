package main

import (
	"os"
	"sync"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
)

// enableMacKeyboard asks the terminal to report Cmd+letter as Super.
//
// xui already pushes Kitty flags 7 (disambiguate + events + alternates).
// Flag 8 (report all keys as escape codes) is what makes Cmd+B / Cmd+O /
// Cmd+I arrive. Push flags 15 so restoreMacKeyboard can pop them.
func enableMacKeyboard(vx *xui.XUI) {
	if vx == nil || components.Keys.Name != "cmd" {
		return
	}
	_, _ = vx.WriteRaw([]byte("\x1b[>15u"))
}

// restoreMacKeyboard pops Kitty keyboard mode. Ctrl+C otherwise leaves
// CSI-u sequences in the shell. Write to stdout so this still runs after
// xui.Close.
func restoreMacKeyboard() {
	if components.Keys.Name != "cmd" {
		return
	}
	_, _ = os.Stdout.WriteString("\x1b[<u\x1b[<u\x1b[=0u")
}

func closeTerminal(vx *xui.XUI) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			restoreMacKeyboard()
			if vx != nil {
				_ = vx.Close()
			}
		})
	}
}
