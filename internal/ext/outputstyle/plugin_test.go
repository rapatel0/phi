package outputstyle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

// The plugin must work through the real Host and Manager, not just its own
// helpers: a wiring mistake would pass every unit test above.
func TestPluginAppliesThroughHost(t *testing.T) {
	dir := t.TempDir()
	writeStyle(t, dir, "concise.md", "---\nname: concise\n---\nBe brief.")

	p := &Plugin{}
	h := ext.NewHost()
	require.NoError(t, p.Register(h))
	p.st.dirs = []string{dir} // override the real home/project lookup
	p.st.set("concise")

	mgr := hooks.NewManager(h.HookEntries()...)
	out := mgr.BeforeAgentStart(t.Context(), hooks.SessionEvent{SystemPrompt: "BASE"})

	require.True(t, out.SystemPromptSet, "the hook must replace the prompt")
	assert.Contains(t, out.SystemPrompt, "BASE")
	assert.Contains(t, out.SystemPrompt, "Be brief.")
}

// Clearing the style must return the prompt to the original text.
func TestPluginClearsThroughHost(t *testing.T) {
	dir := t.TempDir()
	writeStyle(t, dir, "concise.md", "---\nname: concise\n---\nBe brief.")

	p := &Plugin{}
	h := ext.NewHost()
	require.NoError(t, p.Register(h))
	p.st.dirs = []string{dir}
	p.st.set("concise")

	mgr := hooks.NewManager(h.HookEntries()...)
	styled := mgr.BeforeAgentStart(t.Context(), hooks.SessionEvent{SystemPrompt: "BASE"})
	require.True(t, styled.SystemPromptSet)

	p.st.set("")
	cleared := mgr.BeforeAgentStart(t.Context(), hooks.SessionEvent{SystemPrompt: styled.SystemPrompt})
	require.True(t, cleared.SystemPromptSet, "stripping the block is a change")
	assert.Equal(t, "BASE", cleared.SystemPrompt)
}

// The command path is what a user actually touches, so it is worth covering
// end to end: select, list, clear, and reject an unknown name.
func TestCommandSelectsAndClears(t *testing.T) {
	dir := t.TempDir()
	writeStyle(t, dir, "concise.md", "---\nname: concise\ndescription: Few words\n---\nBrief.")
	p := &Plugin{st: store{dirs: []string{dir}}}

	res, err := p.run(t.Context(), []string{"concise"})
	require.NoError(t, err)
	assert.Equal(t, "concise", p.st.get())
	assert.True(t, res.StatusSet)
	assert.Equal(t, "style:concise", res.Status)

	res, err = p.run(t.Context(), []string{"off"})
	require.NoError(t, err)
	assert.Empty(t, p.st.get())
	assert.True(t, res.StatusSet, "clearing must also clear the footer")
	assert.Empty(t, res.Status)
}

func TestCommandRejectsUnknownStyle(t *testing.T) {
	dir := t.TempDir()
	writeStyle(t, dir, "concise.md", "Brief.")
	p := &Plugin{st: store{dirs: []string{dir}}}

	_, err := p.run(t.Context(), []string{"nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "concise", "the error must list what is available")
	assert.Empty(t, p.st.get(), "a rejected name must not become active")
}

// With no argument the command opens a palette page rather than selecting.
// The list covers the built-in styles as well as the user's own.
func TestCommandListsStyles(t *testing.T) {
	dir := t.TempDir()
	writeStyle(t, dir, "concise.md", "---\nname: concise\ndescription: Few words\n---\nB.")
	p := &Plugin{st: store{dirs: []string{dir}}}

	res, err := p.run(t.Context(), nil)
	require.NoError(t, err)
	require.NotNil(t, res.List)

	byLabel := map[string]hooks.CommandListItem{}
	for _, it := range res.List.Items {
		byLabel[it.Label] = it
	}
	require.Contains(t, byLabel, "concise")
	assert.Equal(t, "/style concise", byLabel["concise"].Submit)
	assert.Equal(t, "Few words", byLabel["concise"].Detail, "the file must win over the built-in")
	assert.Contains(t, byLabel, "reviewer", "built-in styles must be listed too")
	assert.NotContains(t, byLabel, "off", "no style is active, so there is nothing to clear")

	// With a style active the list marks it and offers a way to clear it.
	before := len(res.List.Items)
	p.st.set("concise")
	res, err = p.run(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, res.List.Items, before+1)
	assert.Equal(t, "concise (active)", res.List.Items[0].Label)
	assert.Equal(t, "/style off", res.List.Items[len(res.List.Items)-1].Submit)
}

// A built-in style must be selectable with no files on disk at all.
func TestBuiltinStylesAreSelectable(t *testing.T) {
	p := &Plugin{st: store{dirs: []string{t.TempDir()}}}

	_, err := p.run(t.Context(), []string{"reviewer"})
	require.NoError(t, err)

	got, ok := p.st.resolve()
	require.True(t, ok)
	assert.Contains(t, got.Body, "reviewer")
}

// The command must fail loudly rather than silently doing nothing when no
// style directory could be resolved.
func TestCommandWithoutStyleDirs(t *testing.T) {
	p := &Plugin{}
	_, err := p.run(t.Context(), []string{"concise"})
	require.ErrorIs(t, err, errNoStyleDir)
}
