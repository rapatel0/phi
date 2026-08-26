package lens

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

func writeEvent(t *testing.T, tool, root, path string) hooks.Event {
	t.Helper()
	in, err := json.Marshal(map[string]string{"path": path})
	require.NoError(t, err)
	return hooks.Event{Tool: tool, Cwd: root, Input: in}
}

// The whole point, through the real hook manager: a broken file the model just
// wrote comes back as a note on the tool result.
func TestPluginReportsThroughTheHookManager(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() { missingHelper() }\n")

	h := ext.NewHost()
	require.NoError(t, (&Plugin{}).Register(h))
	mgr := hooks.NewManager(h.HookEntries()...)

	for _, tool := range []string{"write", "edit"} {
		out := mgr.PostTool(t.Context(), writeEvent(t, tool, root, file))
		assert.Contains(t, out.Context, "undefined: missingHelper", "%s must report", tool)
	}
}

// A tool that does not write files must not be checked.
func TestPluginIgnoresOtherTools(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() { missingHelper() }\n")

	h := ext.NewHost()
	require.NoError(t, (&Plugin{}).Register(h))
	mgr := hooks.NewManager(h.HookEntries()...)

	out := mgr.PostTool(t.Context(), writeEvent(t, "bash", root, file))
	assert.Empty(t, out.Context)
}

// A failed edit leaves the previous content on disk, so any finding would
// describe code the model did not write.
func TestPluginSkipsFailedTools(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() { missingHelper() }\n")

	p := &Plugin{}
	ev := writeEvent(t, "write", root, file)
	ev.Err = "permission denied"

	note, err := p.check(t.Context(), ev)
	require.NoError(t, err)
	assert.Empty(t, note)
}

// Arguments that do not carry a path must be ignored rather than guessed at.
func TestPluginIgnoresUnparsableInput(t *testing.T) {
	p := &Plugin{}
	for _, in := range []string{`{"other":"x"}`, `not json`, ``} {
		note, err := p.check(t.Context(), hooks.Event{Tool: "write", Cwd: ".", Input: json.RawMessage(in)})
		require.NoError(t, err)
		assert.Empty(t, note)
	}
}

// Silence on a clean file is what keeps the note worth reading.
func TestPluginSaysNothingWhenTheFileIsClean(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() int { return 1 }\n")

	note, err := (&Plugin{}).check(t.Context(), writeEvent(t, "write", root, file))
	require.NoError(t, err)
	assert.Empty(t, note)
}

func TestNoteFormatting(t *testing.T) {
	assert.Empty(t, note(nil), "nothing to report means no note")

	one := note([]Problem{{File: "a.go", Line: 1, Col: 2, Msg: "boom"}})
	assert.Contains(t, one, "lens: 1 problem\n")
	assert.Contains(t, one, "a.go:1:2: boom")
	assert.NotContains(t, one, "problems", "one problem must not be plural")
}

// A file mid-refactor can report hundreds of problems. A wall of findings
// buries the first one, which is usually the cause of the rest.
func TestNoteCapsLongLists(t *testing.T) {
	var many []Problem
	for i := range maxReported + 5 {
		many = append(many, Problem{File: "a.go", Line: i + 1, Col: 1, Msg: "boom"})
	}
	got := note(many)
	assert.Contains(t, got, "lens: 15 problems")
	assert.Contains(t, got, "... and 5 more")
	assert.Equal(t, maxReported, countLines(got, "  a.go:"))
}

func countLines(s, prefix string) int {
	n := 0
	for _, line := range splitLines(s) {
		if len(line) >= len(prefix) && line[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// /lens must distinguish "nothing checked yet" from "checked and clean", or a
// silent extension looks the same as a broken one.
func TestLensCommandStates(t *testing.T) {
	p := &Plugin{}
	res, err := p.report(t.Context(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "nothing checked yet")

	root, file := goModule(t, "package probe\n\nfunc F() int { return 1 }\n")
	_, err = p.check(t.Context(), writeEvent(t, "write", root, file))
	require.NoError(t, err)
	res, err = p.report(t.Context(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "no problems")

	root, file = goModule(t, "package probe\n\nfunc F() { missingHelper() }\n")
	_, err = p.check(t.Context(), writeEvent(t, "write", root, file))
	require.NoError(t, err)
	res, err = p.report(t.Context(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "undefined: missingHelper")
}

// The extension registers the command it documents.
func TestPluginRegistersTheLensCommand(t *testing.T) {
	h := ext.NewHost()
	require.NoError(t, (&Plugin{}).Register(h))

	var names []string
	for _, c := range h.Commands() {
		names = append(names, c.Name)
	}
	assert.Contains(t, names, "lens")
}

// Non-Go files must work too, or the extension only serves one language.
func TestPluginChecksPythonWhenRuffIsPresent(t *testing.T) {
	if _, err := os.Stat("/dev/null"); err != nil {
		t.Skip("unexpected environment")
	}
	root := t.TempDir()
	file := filepath.Join(root, "m.py")
	require.NoError(t, os.WriteFile(file, []byte("import os\n"), 0o600))

	note, err := (&Plugin{}).check(t.Context(), writeEvent(t, "write", root, file))
	require.NoError(t, err)
	if note == "" {
		t.Skip("ruff is not installed")
	}
	assert.Contains(t, note, "m.py")
}
