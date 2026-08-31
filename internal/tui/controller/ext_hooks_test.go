package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/project"
)

// ReloadHooks must merge extension hooks with the discovered ones. Without the
// merge, a compiled-in extension registers a command that nothing dispatches.
func TestNewControllerIncludesExtensionCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ALPHA_MODEL", "test-model")
	t.Setenv("ALPHA_API_KEY", "test-key")
	t.Setenv("ALPHA_BASE_URL", "http://127.0.0.1:9")

	cwd := t.TempDir()
	proj, err := project.Discover(cwd)
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())

	const name = "startup-probe"
	ext.Default().RegisterCommand(ext.Command{
		Name:        name,
		Description: "probe",
		Run: func(context.Context, []string) (hooks.CommandResult, error) {
			return hooks.CommandResult{Toast: "started"}, nil
		},
	})

	ctrl, err := NewController(NewBus(nil), proj, cwd)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, entry := range ctrl.Hooks().CommandEntries() {
		names[entry.Hook.Name()] = true
	}
	assert.True(t, names[name],
		"an extension command must be present at startup, got %v", names)

	res, err := ctrl.Hooks().RunCommand(t.Context(), name, hooks.CommandEvent{})
	require.NoError(t, err)
	assert.Equal(t, "started", res.Toast)
}

func TestReloadHooksIncludesExtensionCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ALPHA_MODEL", "test-model")
	t.Setenv("ALPHA_API_KEY", "test-key")
	t.Setenv("ALPHA_BASE_URL", "http://127.0.0.1:9")

	cwd := t.TempDir()
	proj, err := project.Discover(cwd)
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())

	ctrl, err := NewController(NewBus(nil), proj, cwd)
	require.NoError(t, err)

	// Bundled extensions register from cmd/plugins.go, which this test binary
	// does not link. Register on the same process-wide host they use, so the
	// path under test is identical.
	const name = "reload-probe"
	ext.Default().RegisterCommand(ext.Command{
		Name:        name,
		Description: "probe",
		Run: func(context.Context, []string) (hooks.CommandResult, error) {
			return hooks.CommandResult{Toast: "reloaded"}, nil
		},
	})

	_, _, err = ctrl.ReloadHooks()
	require.NoError(t, err)

	names := map[string]bool{}
	for _, entry := range ctrl.Hooks().CommandEntries() {
		names[entry.Hook.Name()] = true
	}
	assert.True(t, names[name],
		"an extension command must survive ReloadHooks, got %v", names)

	res, err := ctrl.Hooks().RunCommand(t.Context(), name, hooks.CommandEvent{})
	require.NoError(t, err)
	assert.Equal(t, "reloaded", res.Toast,
		"the command must dispatch through the controller's manager")
}

// The default host is what the controller reads, so a command registered on it
// becomes dispatchable. This is the seam a third-party extension uses.
func TestDefaultHostCommandsAreEntries(t *testing.T) {
	// The default host is process-wide and RegisterCommand keeps the first
	// registration, so a second run of this test registers nothing new.
	// Assert the command is dispatchable rather than that the count grew.
	ext.Default().RegisterCommand(ext.Command{
		Name:        "controller-probe",
		Description: "probe",
		Run: func(context.Context, []string) (hooks.CommandResult, error) {
			return hooks.CommandResult{Toast: "probed"}, nil
		},
	})

	entries := ext.Default().HookEntries()
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Hook.Name())
	}
	require.Contains(t, names, "controller-probe")

	res, err := hooks.NewManager(entries...).RunCommand(
		t.Context(), "controller-probe", hooks.CommandEvent{})
	require.NoError(t, err)
	assert.Equal(t, "probed", res.Toast)
}
