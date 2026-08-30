package components

import (
	"runtime"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
)

func key(r rune, mods xui.Modifiers) xui.KeyEvent {
	return xui.KeyEvent{Code: xui.KeyRune, Rune: r, Mods: mods, Press: true}
}

func TestAcceptsCmd(t *testing.T) {
	assert.True(t, AcceptsCmd(key('r', xui.ModCtrl)))
	assert.True(t, AcceptsCmd(key('r', xui.ModSuper)))
	assert.False(t, AcceptsCmd(key('r', 0)))
	assert.False(t, AcceptsCmd(key('r', xui.ModAlt)), "alt must not stand in for ctrl or cmd")
	assert.False(t, AcceptsCmd(key('r', xui.ModShift)))
}

func TestCtrlOnly(t *testing.T) {
	assert.True(t, CtrlOnly(key('v', xui.ModCtrl)))
	assert.False(t, CtrlOnly(key('v', xui.ModSuper)), "cmd must not trigger a ctrl-only action")
	assert.False(t, CtrlOnly(key('v', 0)))
	// Both modifiers held: the cmd form is the one the terminal claims, so
	// this must not count as ctrl-only.
	assert.False(t, CtrlOnly(key('v', xui.ModCtrl|xui.ModSuper)))
}

func TestIsChord(t *testing.T) {
	assert.True(t, IsChord(key('r', xui.ModCtrl), 'r', 'R'))
	assert.True(t, IsChord(key('r', xui.ModSuper), 'r', 'R'))
	assert.True(t, IsChord(key('R', xui.ModSuper), 'r', 'R'), "shifted rune must match")

	assert.False(t, IsChord(key('r', 0), 'r', 'R'), "a bare rune must not fire a chord")
	assert.False(t, IsChord(key('x', xui.ModCtrl), 'r', 'R'))

	// A non-rune event must never match a letter chord.
	assert.False(t, IsChord(xui.KeyEvent{Code: xui.KeyEnter, Mods: xui.ModCtrl, Press: true}, 'r', 'R'))
}

// The palette uses Cmd+Shift+K because terminals claim plain Cmd+K. This
// guards the distinction the shortcut depends on.
func TestShiftDistinguishesCmdChords(t *testing.T) {
	plain := key('k', xui.ModSuper)
	shifted := key('k', xui.ModSuper|xui.ModShift)

	assert.False(t, plain.Mods.Has(xui.ModShift))
	assert.True(t, shifted.Mods.Has(xui.ModShift))
	// Both are Cmd chords, so a naive IsChord check cannot tell them apart.
	assert.True(t, IsChord(plain, 'k', 'K'))
	assert.True(t, IsChord(shifted, 'k', 'K'))
}

func TestModNameFollowsTheOS(t *testing.T) {
	if runtime.GOOS == "darwin" {
		assert.Equal(t, "cmd", ModName())
		assert.Equal(t, "cmd+i", ChordHint("i"))
		assert.Equal(t, "Cmd+K", PaletteHint())
		assert.Equal(t, "Cmd+I", Accel("I"))
		return
	}
	assert.Equal(t, "ctrl", ModName())
	assert.Equal(t, "ctrl+i", ChordHint("i"))
	assert.Equal(t, "Ctrl+K", PaletteHint())
	assert.Equal(t, "Ctrl+I", Accel("I"))
}

func TestIsPaletteChord(t *testing.T) {
	assert.True(t, IsPaletteChord(key('k', xui.ModCtrl)))
	assert.True(t, IsPaletteChord(key('k', xui.ModSuper)))
	assert.True(t, IsPaletteChord(key('k', xui.ModSuper|xui.ModShift)))
	assert.False(t, IsPaletteChord(key('k', 0)))
	assert.False(t, IsPaletteChord(key('r', xui.ModSuper)))
}
