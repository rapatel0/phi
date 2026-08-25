package commands

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

// An extension command must reach the slash registry the same way a discovered
// hook command does, and keep the description the extension supplied.
func TestExtensionCommandReachesRegistry(t *testing.T) {
	h := ext.NewHost()
	h.RegisterCommand(ext.Command{
		Name:        "extcmd",
		Description: "from an extension",
		Run: func(context.Context, []string) (hooks.CommandResult, error) {
			return hooks.CommandResult{Toast: "ok"}, nil
		},
	})

	entries := h.HookEntries()
	require.Len(t, entries, 1)

	descriptions := map[string]string{}
	for _, cmd := range h.Commands() {
		descriptions[cmd.Name] = cmd.Description
	}

	r := NewBuiltinRegistry()
	hc := &HookCommands{Registry: r}
	for _, entry := range entries {
		name := entry.Hook.Name()
		require.True(t, r.registerHook(hc.slashCommand(name, descriptions[name])))
	}

	// Hook-sourced commands insert bare, without the builtin trailing space.
	assert.Equal(t, "/extcmd", r.LookupInsert("extcmd"))

	var found *Command
	for i := range r.SlashCommands() {
		if r.SlashCommands()[i].Name == "extcmd" {
			found = &r.SlashCommands()[i]
			break
		}
	}
	require.NotNil(t, found, "extension command must appear in the slash list")
	assert.Equal(t, "from an extension", found.Description,
		"the palette must show the extension's own description")
}

// A discovered hook has no description, so it keeps the generic label.
func TestHookCommandKeepsGenericDescription(t *testing.T) {
	hc := &HookCommands{Registry: NewBuiltinRegistry()}
	assert.Equal(t, "hook command", hc.slashCommand("discovered", "").Description)
}

// An extension must not shadow a builtin.
func TestExtensionCommandCannotReplaceBuiltin(t *testing.T) {
	h := ext.NewHost()
	h.RegisterCommand(ext.Command{
		Name:        "clear",
		Description: "hijack attempt",
		Run: func(context.Context, []string) (hooks.CommandResult, error) {
			return hooks.CommandResult{}, nil
		},
	})

	r := NewBuiltinRegistry()
	hc := &HookCommands{Registry: r}
	assert.False(t, r.registerHook(hc.slashCommand("clear", "hijack attempt")))
	assert.Equal(t, "/clear", r.LookupInsert("clear"))
}
