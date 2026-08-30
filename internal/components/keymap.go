package components

import (
	"runtime"
	"strings"
	"unicode"

	"github.com/pulseaiclub/xui"
)

// Chord is one concrete key combination.
type Chord struct {
	Mods xui.Modifiers
	Rune rune
	Code xui.KeyCode
}

// Match reports whether e carries at least these modifiers and this key.
func (c Chord) Match(e xui.KeyEvent) bool {
	if !e.Mods.Has(c.Mods) {
		return false
	}
	if c.Code != 0 && c.Code != xui.KeyRune {
		return e.Code == c.Code
	}
	if c.Rune == 0 || e.Code != xui.KeyRune {
		return false
	}
	return unicode.ToLower(e.Rune) == unicode.ToLower(c.Rune)
}

// Label is a title-case shortcut such as Cmd+I or Ctrl+Shift+C.
func (c Chord) Label() string {
	var parts []string
	if c.Mods.Has(xui.ModSuper) {
		parts = append(parts, "Cmd")
	}
	if c.Mods.Has(xui.ModCtrl) {
		parts = append(parts, "Ctrl")
	}
	if c.Mods.Has(xui.ModShift) {
		parts = append(parts, "Shift")
	}
	switch {
	case c.Code == xui.KeyEnter:
		parts = append(parts, "Enter")
	case c.Rune != 0:
		parts = append(parts, strings.ToUpper(string(c.Rune)))
	}
	return strings.Join(parts, "+")
}

// Keymap is the process shortcut table. MacKeymap and UnixKeymap fill it.
type Keymap struct {
	Name     string // "cmd" or "ctrl"
	Title    string // "Cmd" or "Ctrl"
	Primary  xui.Modifiers
	Fallback xui.Modifiers

	Palette    []Chord
	Sessions   []Chord
	AgentTree  []Chord
	AgentTreeT []Chord
	ChildView  []Chord
	ChildSteer []Chord
	TreeNext   []Chord
	TreePrev   []Chord
	ChildEnter []Chord
	ImagePaste []Chord
	Copy       []Chord
}

// Keys is the table for this process. Tests may overwrite it.
var Keys = KeymapFor(runtime.GOOS)

// KeymapFor returns the table for a GOOS value.
func KeymapFor(goos string) Keymap {
	if goos == "darwin" {
		return MacKeymap()
	}
	return UnixKeymap()
}

// Hit reports whether e matches any chord in the list.
func (Keymap) Hit(e xui.KeyEvent, chords []Chord) bool {
	for _, c := range chords {
		if c.Match(e) {
			return true
		}
	}
	return false
}

// Label is the primary (first) chord in title case.
func (Keymap) Label(chords []Chord) string {
	if len(chords) == 0 {
		return ""
	}
	return chords[0].Label()
}

// Hint is the primary chord in lowercase, for footer chrome.
func (m Keymap) Hint(chords []Chord) string {
	return strings.ToLower(m.Label(chords))
}

// Accel builds Title+key, for ad-hoc labels that are not table rows.
func (m Keymap) Accel(key string) string {
	return m.Title + "+" + key
}

// Accepts reports Ctrl or Cmd, according to this table.
func (m Keymap) Accepts(e xui.KeyEvent) bool {
	if e.Mods.Has(m.Primary) {
		return true
	}
	return m.Fallback != 0 && e.Mods.Has(m.Fallback)
}

func letter(mods xui.Modifiers, r rune) Chord {
	return Chord{Mods: mods, Rune: r, Code: xui.KeyRune}
}

func code(mods xui.Modifiers, k xui.KeyCode) Chord {
	return Chord{Mods: mods, Code: k}
}

func both(primary, fallback xui.Modifiers, r rune) []Chord {
	return []Chord{letter(primary, r), letter(fallback, r)}
}

// MacKeymap is Cmd-first. Ctrl remains a fallback except where the terminal
// claims the Cmd form (image paste, Ctrl+T).
func MacKeymap() Keymap {
	cmd, ctrl := xui.ModSuper, xui.ModCtrl
	return Keymap{
		Name:       "cmd",
		Title:      "Cmd",
		Primary:    cmd,
		Fallback:   ctrl,
		Palette:    both(cmd, ctrl, 'k'),
		Sessions:   both(cmd, ctrl, 'r'),
		AgentTree:  both(cmd, ctrl, 'b'),
		AgentTreeT: []Chord{letter(ctrl, 't')},
		ChildView:  both(cmd, ctrl, 'o'),
		ChildSteer: both(cmd, ctrl, 'i'),
		TreeNext:   both(cmd, ctrl, 'n'),
		TreePrev:   both(cmd, ctrl, 'p'),
		ChildEnter: []Chord{code(cmd, xui.KeyEnter), code(ctrl, xui.KeyEnter)},
		ImagePaste: []Chord{letter(ctrl, 'v')},
		Copy:       []Chord{letter(cmd, 'c'), letter(ctrl|xui.ModShift, 'c')},
	}
}

// UnixKeymap is Ctrl-first. Cmd is a fallback when the terminal delivers Super.
func UnixKeymap() Keymap {
	cmd, ctrl := xui.ModSuper, xui.ModCtrl
	return Keymap{
		Name:       "ctrl",
		Title:      "Ctrl",
		Primary:    ctrl,
		Fallback:   cmd,
		Palette:    both(ctrl, cmd, 'k'),
		Sessions:   both(ctrl, cmd, 'r'),
		AgentTree:  both(ctrl, cmd, 'b'),
		AgentTreeT: []Chord{letter(ctrl, 't')},
		ChildView:  both(ctrl, cmd, 'o'),
		ChildSteer: both(ctrl, cmd, 'i'),
		TreeNext:   both(ctrl, cmd, 'n'),
		TreePrev:   both(ctrl, cmd, 'p'),
		ChildEnter: []Chord{code(ctrl, xui.KeyEnter)},
		ImagePaste: []Chord{letter(ctrl, 'v')},
		Copy:       []Chord{letter(ctrl|xui.ModShift, 'c'), letter(cmd, 'c')},
	}
}
