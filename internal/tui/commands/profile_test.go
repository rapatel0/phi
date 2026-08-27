package commands

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/components/toast"
)

// profileCtx builds a context that records what the command reported.
func profileCtx(args, names []string, setErr error) (CommandContext, *string, *[]string) {
	const active = "work"
	shown := new(string)
	switched := new([]string)
	ctx := CommandContext{
		Args:     args,
		Profile:  func() string { return active },
		Profiles: func() []string { return names },
		SetProfile: func(name string) error {
			if setErr != nil {
				return setErr
			}
			*switched = append(*switched, name)
			return nil
		},
		Toast: func(msg string, _ toast.ToastKind, _ time.Duration) { *shown = msg },
	}
	return ctx, shown, switched
}

// Without an argument the command has to answer "which profiles are there and
// which am I on", which is the question a name alone does not answer.
func TestProfileCommandListsAndMarksTheActive(t *testing.T) {
	ctx, shown, switched := profileCtx(nil, []string{"default", "work"}, nil)

	require.NoError(t, profileCommand(ctx))
	assert.Contains(t, *shown, "default")
	assert.Contains(t, *shown, "work (active)")
	assert.Empty(t, *switched, "listing must not switch anything")
}

func TestProfileCommandSwitches(t *testing.T) {
	ctx, shown, switched := profileCtx([]string{"personal"}, []string{"work", "personal"}, nil)

	require.NoError(t, profileCommand(ctx))
	assert.Equal(t, []string{"personal"}, *switched)
	assert.Contains(t, *shown, "personal")
}

// Switching to the profile already in use must not tear down the session for
// no reason.
func TestProfileCommandIgnoresTheActiveProfile(t *testing.T) {
	ctx, shown, switched := profileCtx([]string{"work"}, []string{"work"}, nil)

	require.NoError(t, profileCommand(ctx))
	assert.Empty(t, *switched, "a switch to the current profile must be a no-op")
	assert.Contains(t, *shown, "already using")
}

// The usual failure is a profile that is not logged in to the model in use.
// The reason has to reach the user, and nothing may change.
func TestProfileCommandReportsAFailure(t *testing.T) {
	ctx, shown, switched := profileCtx(
		[]string{"empty"}, []string{"work", "empty"},
		errors.New("profile empty has no model gpt-5: log in to it first"))

	require.NoError(t, profileCommand(ctx), "a refused switch is reported, not returned")
	assert.Empty(t, *switched)
	assert.Contains(t, *shown, "log in to it first")
}

// A name with spaces around it is what a user actually types.
func TestProfileCommandTrimsTheName(t *testing.T) {
	ctx, _, switched := profileCtx([]string{" ", "personal", " "}, []string{"personal"}, nil)

	require.NoError(t, profileCommand(ctx))
	assert.Equal(t, []string{"personal"}, *switched)
}

// Headless callers leave the capability nil, and the command must say so
// rather than appear to work.
func TestProfileCommandWithoutSupport(t *testing.T) {
	shown := ""
	ctx := CommandContext{
		Args:    []string{"work"},
		Profile: func() string { return "default" },
		Toast:   func(msg string, _ toast.ToastKind, _ time.Duration) { shown = msg },
	}

	require.NoError(t, profileCommand(ctx))
	assert.Contains(t, shown, "not available")
}

func TestProfileSummaryWithoutProfiles(t *testing.T) {
	assert.Equal(t, "no profiles", profileSummary(CommandContext{}))
}

// The palette submenu marks the active profile and switches on accept.
func TestProfileSettingsCommand(t *testing.T) {
	var switched []string
	cmd := profileSettingsCommand(
		func(name string) error { switched = append(switched, name); return nil },
		func() string { return "work" },
		func() []string { return []string{"default", "work"} },
	)

	require.Len(t, cmd.Submenu, 2)
	assert.Equal(t, "default", cmd.Submenu[0].Verb)
	assert.Contains(t, cmd.Submenu[1].Verb, "(active)")

	cmd.Submenu[0].Run()
	assert.Equal(t, []string{"default"}, switched)
}

// A row the user cannot act on must be disabled, not a dead entry that
// silently does nothing.
func TestProfileSettingsCommandWithNoProfiles(t *testing.T) {
	cmd := profileSettingsCommand(nil, nil, nil)

	require.Len(t, cmd.Submenu, 1)
	assert.True(t, cmd.Submenu[0].Disabled)
}

// SubmenuFn is what the palette calls when it opens, so a profile created
// after startup still appears.
func TestProfileSettingsCommandRebuildsOnOpen(t *testing.T) {
	names := []string{"default"}
	cmd := profileSettingsCommand(nil, func() string { return "default" }, func() []string { return names })
	require.Len(t, cmd.Submenu, 1)

	names = append(names, "work")
	require.NotNil(t, cmd.SubmenuFn)
	assert.Len(t, cmd.SubmenuFn(), 2, "a profile added later must appear")
}
