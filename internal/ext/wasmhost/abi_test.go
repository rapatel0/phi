package wasmhost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/ext/askuser"
	"github.com/rapatel0/alpha/internal/ext/btw"
	"github.com/rapatel0/alpha/internal/ext/goal"
	"github.com/rapatel0/alpha/internal/ext/outputstyle"
	"github.com/rapatel0/alpha/internal/ext/todo"
	"github.com/rapatel0/alpha/internal/ext/tokenspeed"
	"github.com/rapatel0/alpha/internal/ext/toolstats"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools"
)

func TestModelJSONOmitsAPIKey(t *testing.T) {
	raw := modelJSON(llm.ModelConfig{
		Name:          "grok-4.6",
		APIKey:        "secret",
		BaseURL:       "https://api.x.ai/v1",
		ContextWindow: 500_000,
	})

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "grok-4.6", got["name"])
	assert.Equal(t, "https://api.x.ai/v1", got["base_url"])
	assert.NotContains(t, string(raw), "secret")
}

func TestScopeNamesAcceptsBothForms(t *testing.T) {
	assert.Equal(t, []string{"read", "grep"}, scopeNames(`["read","grep"]`))
	assert.Equal(t, []string{"read", "grep"}, scopeNames("read, grep"))
	assert.Nil(t, scopeNames("  "))
}

func TestAssetPathStaysInsideModuleDir(t *testing.T) {
	assert.Equal(t, "dir/prompts/style.md", assetPath("dir", "prompts/style.md"))
	assert.Equal(t, "dir/style.md", assetPath("dir", "../style.md"))
}

func TestStatePathIsNamespacedPerPlugin(t *testing.T) {
	assert.Equal(t, "/home/u/.alpha/plugins/todo/state.json", statePath("/home/u", "todo", "state.json"))
}

// TestGetterGuestUsesHostABI loads a hand-written module that calls each getter
// added for plugins, so the reply offset and the returned lengths are checked
// against a real guest rather than only the Go helpers.
func TestGetterGuestUsesHostABI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "getter.wasm"), readTestdata(t, "getter.wasm"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("asset for the guest"), 0o644))

	h := ext.NewHost()
	var scoped []string
	h.SetToolScope(func(names []string) { scoped = names })
	h.SetToolNames(func() []string { return []string{"read", "grep"} })
	h.SetModelInfo(llm.ModelConfig{
		Name:          "grok-4.6",
		APIKey:        "secret",
		BaseURL:       "https://api.x.ai/v1",
		ContextWindow: 500_000,
	})
	require.NoError(t, loadOne(t.Context(), h, filepath.Join(dir, "getter.wasm")))

	assert.Equal(t, []string{"read"}, scoped, "set_active_tools must reach the engine")

	var found ext.Command
	for _, c := range h.Commands() {
		if c.Name == "getter" {
			found = c
		}
	}
	require.NotNil(t, found.Run)

	res, err := found.Run(t.Context(), nil)
	require.NoError(t, err)

	// model_info reaches the toast, without the credential.
	assert.Contains(t, res.Toast, "grok-4.6")
	assert.NotContains(t, res.Toast, "secret")
	// read_asset reaches the submit text, read from the module directory.
	assert.Equal(t, "asset for the guest", res.Submit)
	// active_tools reaches the footer status as a JSON list.
	assert.True(t, res.StatusSet)
	assert.Equal(t, `["read","grep"]`, res.Status)
}

func readTestdata(t *testing.T, file string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", file))
	require.NoError(t, err)
	return raw
}

// The guest in testdata/tierb.wasm runs the same extension packages the
// compiled-in path uses. Every check here compares the two on one input, so a
// footer or command string cannot drift between paths.
//
// A "guest" is the wasm build of these packages, so the wasm ABI and the Go
// implementation must agree on text the user sees.

func wiredHost(t *testing.T) *ext.Host {
	t.Helper()
	h := ext.NewHost()
	h.SetQuestionAsker(func(_ context.Context, q ext.Question) (ext.Answer, error) {
		if len(q.Options) < 2 {
			return ext.Answer{Index: 0, Label: q.Options[0]}, nil
		}
		return ext.Answer{Index: 1, Label: q.Options[1]}, nil
	})
	h.SetSideChannel(func(_ context.Context, req ext.SideRequest) (ext.SideResult, error) {
		return ext.SideResult{JobID: "j1", Summary: "side summary for " + req.Prompt}, nil
	})
	return h
}

// goHost wires the compiled-in extensions onto a fresh host.
func goHost(t *testing.T) *ext.Host {
	t.Helper()
	h := wiredHost(t)
	for _, p := range []ext.Plugin{
		tokenspeed.Plugin{},
		toolstats.New(),
		todo.Plugin{},
		askuser.Plugin{},
		&btw.Plugin{},
		&goal.Plugin{},
		&outputstyle.Plugin{},
	} {
		require.NoError(t, h.Add(p))
	}
	return h
}

// wasmHost loads the guest twin onto a host with the same wiring.
func wasmHost(t *testing.T) *ext.Host {
	t.Helper()
	h := wiredHost(t)
	require.NoError(t, loadOne(t.Context(), h, filepath.Join("testdata", "tierb.wasm")))
	return h
}

func command(t *testing.T, h *ext.Host, name string, args ...string) hooks.CommandResult {
	t.Helper()
	for _, c := range h.Commands() {
		if c.Name == name {
			res, err := c.Run(t.Context(), args)
			require.NoError(t, err)
			return res
		}
	}
	t.Fatalf("command %q not registered", name)
	return hooks.CommandResult{}
}

func toolContent(t *testing.T, h *ext.Host, name, input string) string {
	t.Helper()
	return toolRun(t, h, name, input).Content
}

func toolRun(t *testing.T, h *ext.Host, name, input string) tools.Result {
	t.Helper()
	for _, tl := range h.Tools() {
		if tl.Definition.Name == name {
			res, err := tl.Run(t.Context(), []byte(input))
			require.NoError(t, err)
			return res
		}
	}
	t.Fatalf("tool %q not registered", name)
	return tools.Result{}
}

// tryCommand runs one registered command and reports its error, so an error
// path can be compared the same way a toast is.
func tryCommand(t *testing.T, h *ext.Host, name string, args ...string) string {
	t.Helper()
	for _, c := range h.Commands() {
		if c.Name == name {
			_, err := c.Run(t.Context(), args)
			if err == nil {
				return ""
			}
			return err.Error()
		}
	}
	t.Fatalf("command %q not registered", name)
	return ""
}

func toolError(t *testing.T, h *ext.Host, name, input string) string {
	t.Helper()
	for _, tl := range h.Tools() {
		if tl.Definition.Name == name {
			_, err := tl.Run(t.Context(), []byte(input))
			if err == nil {
				return ""
			}
			return err.Error()
		}
	}
	t.Fatalf("tool %q not registered", name)
	return ""
}

func footerOf(h *ext.Host) string { return joinBits(h.FooterBits()) }

func labels(list *hooks.CommandList) []string {
	out := make([]string, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, it.Label)
	}
	return out
}

func joinBits(bits []string) string {
	out := ""
	for _, b := range bits {
		if b == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += b
	}
	return out
}

func TestGuestFooterMatchesAfterUsage(t *testing.T) {
	goH, wasmH := goHost(t), wasmHost(t)

	goH.EmitUsage(120, 100, 250*time.Millisecond)
	wasmH.EmitUsage(120, 100, 250*time.Millisecond)

	goFooter, wasmFooter := footerOf(goH), footerOf(wasmH)
	require.NotEmpty(t, goFooter, "compiled-in footer must show a rate")
	assert.Equal(t, goFooter, wasmFooter)
}

// postTool drives a post-tool event through every hook of one host. Both paths
// use it, so neither gets an extra scheduling turn.
func postTool(t *testing.T, h *ext.Host, ev hooks.Event) {
	t.Helper()
	for _, e := range h.HookEntries() {
		if e.Kind != hooks.KindPostTool || !e.Hook.Match(ev.Tool) {
			continue
		}
		_, err := e.Hook.PostTool(t.Context(), ev)
		require.NoError(t, err)
	}
}

func TestGuestToolstatsCommandMatches(t *testing.T) {
	goH, wasmH := goHost(t), wasmHost(t)

	events := []hooks.Event{
		{Tool: "bash"},
		{Tool: "bash", Err: "exit 1"},
		{Tool: "read"},
	}
	for _, goOrWasm := range []*ext.Host{goH, wasmH} {
		for _, ev := range events {
			postTool(t, goOrWasm, ev)
		}
	}

	want := "3 tool calls: bash 2 (1 failed), read 1"
	assert.Equal(t, want, command(t, goH, "toolstats").Toast, "compiled-in summary")
	assert.Equal(t, want, command(t, wasmH, "toolstats").Toast, "guest summary")
}

func TestGuestTodoToolAndFooterMatch(t *testing.T) {
	goH, wasmH := goHost(t), wasmHost(t)
	input := `{"todos":[{"id":"1","text":"a","status":"completed"},{"id":"2","text":"b","status":"pending"}]}`

	goRun := toolRun(t, goH, "todo_write", input)
	wasmRun := toolRun(t, wasmH, "todo_write", input)

	assert.Equal(t, goRun.Content, wasmRun.Content)
	require.NotEmpty(t, goRun.Detail, "compiled-in row summary")
	assert.Equal(t, goRun.Detail, wasmRun.Detail, "guest row summary")
	assert.Equal(t, footerOf(goH), footerOf(wasmH))
}

func TestGuestAskQuestionMatches(t *testing.T) {
	// ask_user_question reaches for the process host, so the compiled-in path
	// needs the asker on ext.Default() the way the shell wires it.
	ext.Default().SetQuestionAsker(func(_ context.Context, q ext.Question) (ext.Answer, error) {
		return ext.Answer{Index: 1, Label: q.Options[1]}, nil
	})

	goH, wasmH := goHost(t), wasmHost(t)
	input := `{"header":"Pick","prompt":"Which editor?","options":["vim","emacs"]}`

	assert.Equal(t, toolContent(t, goH, "ask_user_question", input),
		toolContent(t, wasmH, "ask_user_question", input))
}

func TestGuestBtwCommandMatches(t *testing.T) {
	goH, wasmH := goHost(t), wasmHost(t)

	assert.Equal(t, command(t, goH, "btw", "hello").Toast, command(t, wasmH, "btw", "hello").Toast)

	goClear := command(t, goH, "btw", "clear")
	wasmClear := command(t, wasmH, "btw", "clear")
	assert.Equal(t, goClear.Toast, wasmClear.Toast)
	assert.Equal(t, goClear.StatusSet, wasmClear.StatusSet)
}

func TestGuestGoalCommandAndPromptMatch(t *testing.T) {
	goH, wasmH := goHost(t), wasmHost(t)

	goSet := command(t, goH, "goal", "Ship", "the", "slice")
	wasmSet := command(t, wasmH, "goal", "Ship", "the", "slice")
	assert.Equal(t, goSet.Toast, wasmSet.Toast)
	assert.Equal(t, goSet.Status, wasmSet.Status)

	goPrompt := hooks.NewManager(goH.HookEntries()...).BeforeAgentStart(t.Context(), hooks.SessionEvent{
		Kind:         hooks.KindBeforeAgentStart,
		Prompt:       "continue",
		SystemPrompt: "base prompt",
	})
	wasmPrompt := hooks.NewManager(wasmH.HookEntries()...).BeforeAgentStart(t.Context(), hooks.SessionEvent{
		Kind:         hooks.KindBeforeAgentStart,
		Prompt:       "continue",
		SystemPrompt: "base prompt",
	})
	require.True(t, goPrompt.SystemPromptSet)
	assert.Equal(t, goPrompt.SystemPrompt, wasmPrompt.SystemPrompt)
}

func TestGuestStyleCommandMatches(t *testing.T) {
	goH, wasmH := goHost(t), wasmHost(t)

	// todo keeps its list at package level, so clear it on both paths before
	// comparing footers. Both sides then show only the style bit.
	toolContent(t, goH, "todo_write", `{"todos":[]}`)
	toolContent(t, wasmH, "todo_write", `{"todos":[]}`)

	goList := command(t, goH, "style")
	wasmList := command(t, wasmH, "style")
	require.NotNil(t, goList.List)
	require.NotNil(t, wasmList.List)
	assert.Equal(t, goList.List.Title, wasmList.List.Title)

	want := labels(goList.List)
	require.NotEmpty(t, want)
	name := want[0]
	assert.Contains(t, labels(wasmList.List), name, "guest must offer the same styles")

	goPick := command(t, goH, "style", name)
	wasmPick := command(t, wasmH, "style", name)
	assert.Equal(t, goPick.Toast, wasmPick.Toast)
	assert.Equal(t, goPick.Status, wasmPick.Status)
	assert.Equal(t, footerOf(goH), footerOf(wasmH))
}

func TestGuestCommandErrorMatches(t *testing.T) {
	goH, wasmH := goHost(t), wasmHost(t)

	want := "ask something: /btw <question>, or /btw list"
	assert.Equal(t, want, tryCommand(t, goH, "btw"), "compiled-in error")
	assert.Equal(t, want, tryCommand(t, wasmH, "btw"), "guest error")
}

func TestGuestToolErrorMatches(t *testing.T) {
	goH, wasmH := goHost(t), wasmHost(t)
	input := `{"todos":"bad"}`

	goErr := toolError(t, goH, "todo_write", input)
	wasmErr := toolError(t, wasmH, "todo_write", input)
	require.NotEmpty(t, goErr, "compiled-in tool should reject malformed todos")
	assert.Equal(t, goErr, wasmErr, "guest tool error")
}
