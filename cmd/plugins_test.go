package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

// Extensions register from init, so this only holds in a binary that links
// plugins.go. A missing blank import is invisible to every other package's
// tests, which is exactly the mistake worth catching here.
func TestBundledExtensionsRegister(t *testing.T) {
	names := ext.Default().Names()
	for _, want := range []string{"askuser", "todo", "tokenspeed", "toolstats"} {
		assert.Contains(t, names, want, "missing blank import in plugins.go?")
	}
}

// The bundled extension must be dispatchable, not merely registered.
func TestBundledExtensionCommandDispatches(t *testing.T) {
	mgr := hooks.NewManager(ext.Default().HookEntries()...)

	res, err := mgr.RunCommand(t.Context(), "toolstats", hooks.CommandEvent{})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Toast, "/toolstats must report something")
}

// toolstats subscribes to the tool loop, session lifecycle, and the palette,
// so the default host must carry all three kinds.
func TestBundledExtensionHookKinds(t *testing.T) {
	kinds := map[hooks.Kind]int{}
	for _, e := range ext.Default().HookEntries() {
		kinds[e.Kind]++
	}
	assert.Positive(t, kinds[hooks.KindCommand], "slash commands")
	assert.Positive(t, kinds[hooks.KindPostTool], "tool loop observation")
	assert.Positive(t, kinds[hooks.KindSessionStart], "session lifecycle")
}

// The blank import in plugins.go is what puts the loop tools on the model.
// Without it the extension compiles and does nothing.
func TestLoopExtensionIsWired(t *testing.T) {
	found := map[string]bool{}
	for _, tool := range ext.Default().Tools() {
		found[tool.Definition.Name] = true
	}
	for _, name := range []string{
		"LoopCreate", "LoopList", "LoopUpdate", "LoopDelete",
		"MonitorCreate", "MonitorList", "MonitorLogs", "MonitorStop",
	} {
		assert.True(t, found[name], "%s is not registered: the blank import in plugins.go is missing", name)
	}
	assert.Contains(t, ext.Default().Names(), "loop")
}

// The shell has to be able to find background work to stop it at shutdown.
func TestLoopExtensionIsBackgroundWork(t *testing.T) {
	require.NotEmpty(t, ext.Default().Backgrounds(),
		"no background extension registered: the shell cannot stop it at shutdown")
}
