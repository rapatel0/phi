package components

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
)

func TestMacKeymapIsCmdFirst(t *testing.T) {
	km := MacKeymap()
	assert.Equal(t, "cmd", km.Name)
	assert.Equal(t, "Cmd+K", km.Label(km.Palette))
	assert.Equal(t, "cmd+i", km.Hint(km.ChildSteer))
	assert.True(t, km.Hit(key('i', xui.ModSuper), km.ChildSteer))
	assert.True(t, km.Hit(key('i', xui.ModCtrl), km.ChildSteer), "ctrl remains a fallback")
	assert.False(t, km.Hit(key('v', xui.ModSuper), km.ImagePaste), "cmd+v is the terminal paste")
	assert.True(t, km.Hit(key('v', xui.ModCtrl), km.ImagePaste))
	assert.True(t, km.Hit(key('c', xui.ModSuper), km.Copy))
}

func TestUnixKeymapIsCtrlFirst(t *testing.T) {
	km := UnixKeymap()
	assert.Equal(t, "ctrl", km.Name)
	assert.Equal(t, "Ctrl+K", km.Label(km.Palette))
	assert.Equal(t, "ctrl+i", km.Hint(km.ChildSteer))
	assert.True(t, km.Hit(key('k', xui.ModCtrl), km.Palette))
	assert.True(t, km.Hit(key('k', xui.ModSuper), km.Palette), "cmd remains a fallback")
	enter := xui.KeyEvent{Code: xui.KeyEnter, Mods: xui.ModCtrl, Press: true}
	assert.True(t, km.Hit(enter, km.ChildEnter))
}

func TestKeymapForPicksTheTable(t *testing.T) {
	assert.Equal(t, "cmd", KeymapFor("darwin").Name)
	assert.Equal(t, "ctrl", KeymapFor("linux").Name)
}
