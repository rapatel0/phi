package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/project"
)

func TestNewController_RequiresCollaborators(t *testing.T) {
	bus := NewBus(nil)
	_, err := NewController(nil, &project.Project{}, t.TempDir())
	assert.Error(t, err)

	_, err = NewController(bus, nil, t.TempDir())
	assert.Error(t, err)
}

func TestNewController_ReadyEngine(t *testing.T) {
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

	bus := NewBus(nil)
	ctrl, err := NewController(bus, proj, cwd)
	require.NoError(t, err)
	require.NotNil(t, ctrl)
	require.NotNil(t, ctrl.engine)
	assert.Equal(t, cwd, ctrl.cwd)
	assert.NotEmpty(t, ctrl.sessionDir)
	assert.Same(t, proj, ctrl.proj)
}

func TestSetRoleModelValidationAndClearing(t *testing.T) {
	ctrl := newRoleModelController(t)
	assert.Error(t, ctrl.SetRoleModel("unknown", "cheap"))
	assert.Error(t, ctrl.SetRoleModel("review", "missing"))

	require.NoError(t, ctrl.SetRoleModel(" review ", " strong "))
	assert.Equal(t, "strong", ctrl.modelForRole(job.RoleReview).Name)
	require.NoError(t, ctrl.SetRoleModel("review", ""))
	assert.Equal(t, "parent", ctrl.modelForRole(job.RoleReview).Name)
}

func TestRoleModelSessionOverridePrecedence(t *testing.T) {
	ctrl := newRoleModelController(t)
	assert.Equal(t, "cheap", ctrl.modelForRole(job.RoleExplore).Name)

	require.NoError(t, ctrl.SetRoleModel("explore", "strong"))
	assert.Equal(t, "strong", ctrl.modelForRole(job.RoleExplore).Name)
	require.NoError(t, ctrl.SetRoleModel("explore", ""))
	assert.Equal(t, "parent", ctrl.modelForRole(job.RoleExplore).Name)
}

func newRoleModelController(t *testing.T) *Controller {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ALPHA_MODEL", "")
	t.Setenv("ALPHA_API_KEY", "")
	t.Setenv("ALPHA_BASE_URL", "")
	cwd := t.TempDir()
	proj, err := project.Discover(cwd)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(proj.Global().ConfigFile()), 0o755))
	require.NoError(t, os.WriteFile(proj.Global().ConfigFile(), []byte(`
models:
  - name: parent
    api_key: parent-key
  - name: cheap
    api_key: cheap-key
  - name: strong
    api_key: strong-key
agents:
  models:
    explore: cheap
`), 0o644))
	ctrl, err := NewController(NewBus(nil), proj, cwd)
	require.NoError(t, err)
	t.Cleanup(ctrl.Close)
	return ctrl
}

func TestRedrawRelay_BindAfterBus(t *testing.T) {
	relay := NewRedrawRelay()
	bus := NewBus(relay.Fire)
	var n int
	relay.Bind(func() { n++ })
	bus.Publish(SubmitMsg{Text: "y"})
	assert.GreaterOrEqual(t, n, 1)

	// Drain so the next Publish can re-arm wake + Fire.
	_ = bus.Drain()
	bus.Publish(SubmitMsg{Text: "z"})
	assert.GreaterOrEqual(t, n, 2)
}
