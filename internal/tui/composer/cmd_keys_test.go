package composer

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/components"
)

func cmdKey(r rune, mods xui.Modifiers) xui.KeyEvent {
	return xui.KeyEvent{Code: xui.KeyRune, Rune: r, Mods: mods, Press: true}
}

func TestCmdROpensSessionDialog(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	c.SetSessionOpener(func() { c.ShowSessionPicker(testProjects(), nil) })

	ctx := &components.EventContext{}
	c.Handle(ctx, cmdKey('r', xui.ModSuper))
	assert.True(t, c.SessionPickerOpen(), "Cmd+R must open the session dialog")

	c.Handle(ctx, cmdKey('r', xui.ModSuper))
	assert.False(t, c.SessionPickerOpen(), "Cmd+R must toggle it closed")
}

func TestCmdShiftKOpensPalette(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")

	ctx := &components.EventContext{}
	c.Handle(ctx, cmdKey('k', xui.ModSuper|xui.ModShift))
	assert.True(t, c.palette.Open, "Cmd+Shift+K must open the palette")

	c.Handle(ctx, cmdKey('k', xui.ModSuper|xui.ModShift))
	assert.False(t, c.palette.Open, "the same chord must toggle it closed")
}

// Terminals bind plain Cmd+K themselves (Ghostty clears the screen). The app
// must not claim it, so the keystroke is not silently swallowed.
func TestPlainCmdKDoesNotOpenPalette(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	c.Handle(&components.EventContext{}, cmdKey('k', xui.ModSuper))
	assert.False(t, c.palette.Open)
}

func TestCtrlKStillOpensPalette(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	c.Handle(&components.EventContext{}, cmdKey('k', xui.ModCtrl))
	assert.True(t, c.palette.Open, "Ctrl+K must keep working for ssh and tmux")
}

// Image paste stays on Ctrl+V, because a terminal maps Cmd+V to its own paste
// and delivers text rather than a key event.
func TestCmdVIsNotImagePaste(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	ctx := &components.EventContext{}
	c.Handle(ctx, cmdKey('v', xui.ModSuper))
	assert.Empty(t, c.Chat.PendingImages)
	// The chord must not leak into the buffer as a literal "v" either.
	assert.Empty(t, c.Chat.Value)
}

func TestCmdChordsDoNotTypeIntoComposer(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	ctx := &components.EventContext{}
	for _, r := range []rune{'r', 'k', 'b', 'o', 'i', 'q', 'z'} {
		c.Handle(ctx, cmdKey(r, xui.ModSuper))
	}
	assert.Empty(t, c.Chat.Value, "a Cmd chord must never insert text")
}

func TestSessionDialogAcceptsCmdNavigation(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	var picked string
	c.SetSessionOpener(func() {
		c.ShowSessionPicker(testProjects(), func(f string) { picked = f })
	})

	ctx := &components.EventContext{}
	c.Handle(ctx, cmdKey('r', xui.ModSuper))
	require.True(t, c.SessionPickerOpen())

	// Cmd+N moves down, the same as Ctrl+N.
	c.Handle(ctx, cmdKey('n', xui.ModSuper))
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnter, Press: true})
	assert.Equal(t, "/s/a2.jsonl", picked, "Cmd+N must move the selection")
}
