package components

import "github.com/pulseaiclub/xui"

// Key modifier policy
//
// Shortcuts accept Ctrl or Cmd (Super). Cmd is what macOS users expect, and
// Ctrl is the only option over SSH, inside tmux, and on Linux. Binding both
// keeps one shortcut table for every platform.
//
// Terminals claim some Cmd combinations before the application sees them.
// Ghostty, for example, binds Cmd+K to clear_screen, Cmd+T to new_tab, and
// Cmd+Enter to toggle_fullscreen. Actions on those keys stay Ctrl-only, so
// nothing depends on a shortcut the terminal already ate. Cmd+Shift+K is free,
// so the palette uses it as the Cmd form.
//
// Clipboard paste is deliberately excluded. Terminals map Cmd+V to their own
// paste, which delivers text rather than a key event, so image paste stays on
// Ctrl+V.

// AcceptsCmd reports whether e carries Ctrl or Cmd (Super).
//
// Use this for actions whose Cmd form the terminal leaves alone. Use
// CtrlOnly for actions on a Cmd combination the terminal claims.
func AcceptsCmd(e xui.KeyEvent) bool {
	return e.Mods.Has(xui.ModCtrl) || e.Mods.Has(xui.ModSuper)
}

// CtrlOnly reports whether e carries Ctrl and not Cmd.
//
// Use this when the Cmd form is unavailable because the terminal binds it.
func CtrlOnly(e xui.KeyEvent) bool {
	return e.Mods.Has(xui.ModCtrl) && !e.Mods.Has(xui.ModSuper)
}

// IsChord reports whether e is Ctrl+key or Cmd+key for the given letter.
// The letter must be lowercase; the uppercase form is matched too, because
// terminals report a shifted rune in some modes.
func IsChord(e xui.KeyEvent, lower, upper rune) bool {
	return e.Code == xui.KeyRune && AcceptsCmd(e) && (e.Rune == lower || e.Rune == upper)
}
