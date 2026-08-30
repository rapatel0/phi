package components

import "github.com/pulseaiclub/xui"

// Shortcut matching reads [Keys], filled by MacKeymap or UnixKeymap at init.
//
// Terminals claim some Cmd combinations before the application sees them.
// Ghostty binds Cmd+T to new_tab and Cmd+V to paste. Those rows stay Ctrl-only
// in the table. Image paste is Ctrl+V on every platform.

// AcceptsCmd reports whether e carries the primary or fallback modifier.
func AcceptsCmd(e xui.KeyEvent) bool {
	return Keys.Accepts(e)
}

// CtrlOnly reports whether e carries Ctrl and not Cmd.
func CtrlOnly(e xui.KeyEvent) bool {
	return e.Mods.Has(xui.ModCtrl) && !e.Mods.Has(xui.ModSuper)
}

// IsChord reports whether e is a letter chord with the active modifiers.
func IsChord(e xui.KeyEvent, lower, upper rune) bool {
	return e.Code == xui.KeyRune && Keys.Accepts(e) && (e.Rune == lower || e.Rune == upper)
}

// ModName is the lowercase modifier in on-screen hints.
func ModName() string { return Keys.Name }

// ChordHint is a lowercase hint such as cmd+i or ctrl+i.
func ChordHint(key string) string { return Keys.Name + "+" + key }

// PaletteHint is the command-palette shortcut shown in the UI.
func PaletteHint() string { return Keys.Label(Keys.Palette) }

// Accel is a title-case shortcut such as Cmd+I or Ctrl+I.
func Accel(key string) string { return Keys.Accel(key) }

// IsPaletteChord reports a palette toggle chord.
func IsPaletteChord(e xui.KeyEvent) bool { return Keys.Hit(e, Keys.Palette) }
