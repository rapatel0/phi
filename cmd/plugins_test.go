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
