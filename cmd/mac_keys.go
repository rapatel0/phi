package main

import (
	"os"
	"sync"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
)

// enableMacKeyboard asks the terminal to report Cmd+letter as Super.
//
// Call this after xui has pushed Kitty flags 7. SET (CSI = flags u) replaces
// the current flags without another stack entry, so Close still pops once
// back to the original mode. A push here would leave flag 8 on after Close.
func enableMacKeyboard(vx *xui.XUI) {
	if vx == nil || components.Keys.Name != "cmd" {
		return
	}
	_, _ = vx.WriteRaw([]byte("\x1b[=15u"))
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
