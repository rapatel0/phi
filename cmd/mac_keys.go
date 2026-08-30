package main

import (
	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
)

// enableMacKeyboard asks the terminal to report Cmd+letter as Super.
//
// xui already pushes Kitty flags 7 (disambiguate + events + alternates).
// Flag 8 (report all keys as escape codes) is what makes Cmd+B / Cmd+O /
// Cmd+I arrive. `CSI = flags u` sets the current mode without an extra push,
// so Close still pops once.
func enableMacKeyboard(vx *xui.XUI) {
	if vx == nil || components.Keys.Name != "cmd" {
		return
	}
	_, _ = vx.WriteRaw([]byte("\x1b[=15u"))
}
