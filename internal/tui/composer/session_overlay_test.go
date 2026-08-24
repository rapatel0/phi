package composer

import (
	"testing"
	"time"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/sessionpicker"
)

func testProjects() []sessionpicker.Project {
	now := time.Now()
	return []sessionpicker.Project{
		{
			Label:   "alpha",
			Current: true,
			Sessions: []sessionpicker.Session{
				{ID: "aaa11111", File: "/s/a1.jsonl", Preview: "fix oauth", Mtime: now},
				{ID: "bbb22222", File: "/s/a2.jsonl", Preview: "rename", Mtime: now},
			},
		},
	}
}

func ctrl(r rune) xui.KeyEvent {
	return xui.KeyEvent{Code: xui.KeyRune, Rune: r, Mods: xui.ModCtrl, Press: true}
}

func TestCtrlROpensAndClosesSessionDialog(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	opened := 0
	c.SetSessionOpener(func() {
		opened++
		c.ShowSessionPicker(testProjects(), nil)
	})

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrl('r'))
	require.Equal(t, 1, opened)
	assert.True(t, c.SessionPickerOpen())

	// The same binding closes it again.
	c.Handle(ctx, ctrl('r'))
	assert.False(t, c.SessionPickerOpen())
	assert.Equal(t, 1, opened, "closing must not re-open")
}

func TestSessionDialogSwallowsTypingForFilter(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	c.SetSessionOpener(func() { c.ShowSessionPicker(testProjects(), nil) })

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrl('r'))
	require.True(t, c.SessionPickerOpen())

	for _, r := range "oauth" {
		c.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: r, Press: true})
	}

	// Typing filters the dialog; it must not leak into the chat input.
	assert.Equal(t, "oauth", c.sessions.Query)
	assert.Empty(t, c.Chat.Value)
}

func TestSessionDialogEnterReturnsFilePath(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	var picked string
	c.SetSessionOpener(func() {
		c.ShowSessionPicker(testProjects(), func(file string) { picked = file })
	})

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrl('r'))
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnter, Press: true})

	assert.Equal(t, "/s/a1.jsonl", picked, "resume needs the jsonl path")
	assert.False(t, c.SessionPickerOpen())
}

func TestSessionDialogEscapeCloses(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	c.SetSessionOpener(func() { c.ShowSessionPicker(testProjects(), nil) })

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrl('r'))
	require.True(t, c.SessionPickerOpen())

	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyEscape, Press: true})
	assert.False(t, c.SessionPickerOpen())
}

func TestSessionOverlayOnlyDrawsWhenOpen(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	drawCtx := components.DrawContext{
		Max:    components.Size{Width: 100, Height: 30},
		Method: xui.WidthUnicode,
	}

	_, ok := c.SessionOverlay(drawCtx)
	assert.False(t, ok)

	c.SetSessionOpener(func() { c.ShowSessionPicker(testProjects(), nil) })
	c.Handle(&components.EventContext{}, ctrl('r'))

	sub, ok := c.SessionOverlay(drawCtx)
	require.True(t, ok)
	assert.Positive(t, sub.Surface.Size.Width)
}

func TestSessionDialogTakesPrecedenceOverPalette(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	c.SetSessionOpener(func() { c.ShowSessionPicker(testProjects(), nil) })

	ctx := &components.EventContext{}
	c.Handle(ctx, ctrl('k'))
	require.True(t, c.palette.Open)

	// Opening the session dialog closes the palette, so only one overlay shows.
	c.Handle(ctx, ctrl('r'))
	assert.True(t, c.SessionPickerOpen())
	assert.False(t, c.palette.Open)
}

func TestCtrlRWithoutOpenerFallsThrough(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	// No opener wired: Ctrl+R must not panic or open anything.
	c.Handle(&components.EventContext{}, ctrl('r'))
	assert.False(t, c.SessionPickerOpen())
}
