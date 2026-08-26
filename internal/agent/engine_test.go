package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/permission"
	"github.com/rapatel0/alpha/internal/session"
	"github.com/rapatel0/alpha/internal/tools"
)

// sseToolCallChunk encodes one SSE data line carrying a full tool-call delta.
func sseToolCallChunk(id, name, args string) string {
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{map[string]any{
					"index":    0,
					"id":       id,
					"type":     "function",
					"function": map[string]any{"name": name, "arguments": args},
				}},
			},
		}},
	})
	if err != nil {
		panic(err)
	}
	return "data: " + string(payload) + "\n\n"
}

func sseTextChunk(text string) string {
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{
				"role":    "assistant",
				"content": text,
			},
		}},
	})
	if err != nil {
		panic(err)
	}
	return "data: " + string(payload) + "\n\n"
}

// fakeToolSequenceServer returns tool calls for finalAfter tool requests, then
// returns a final text response. A negative finalAfter means tool calls forever.
func fakeToolSequenceServer(finalAfter int) (*httptest.Server, *atomic.Int32) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		request := requests.Add(1)
		if finalAfter < 0 || int(request) <= finalAfter {
			_, _ = fmt.Fprint(w, sseToolCallChunk(fmt.Sprintf("call_%d", request), "count", `{}`))
		} else {
			_, _ = fmt.Fprint(w, sseTextChunk("done"))
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	return server, &requests
}

func countingTool(runs *atomic.Int32) tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        "count",
			Description: "count tool executions",
			Params:      &llm.FunctionParameters{Type: "object"},
		},
		Run: func(context.Context, json.RawMessage) (tools.Result, error) {
			runs.Add(1)
			return tools.Result{Content: "ok"}, nil
		},
	}
}

func newRoundTestEngine(t *testing.T, serverURL string, runs *atomic.Int32) *Engine {
	t.Helper()
	engine, err := NewEngine(EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: serverURL, APIKey: "x"},
		SessionOpts: SessionOpts{Cwd: t.TempDir()},
		Gate:        permission.AllowAll{},
		Tools:       []tools.Tool{countingTool(runs)},
	})
	require.NoError(t, err)
	return engine
}

func TestLoopMaxRoundsAllowsFinalAnswerAfterLastToolRound(t *testing.T) {
	server, requests := fakeToolSequenceServer(2)
	defer server.Close()

	var runs atomic.Int32
	engine := newRoundTestEngine(t, server.URL, &runs)
	require.NoError(t, engine.SetMaxRounds(2))

	var lastErr error
	var finalText string
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			lastErr = err
			break
		}
		if update, ok := ev.(session.AssistantMessageUpdate); ok && update.Message.State == session.StateComplete {
			finalText = update.Message.FlatText()
		}
	}
	require.NoError(t, lastErr)
	require.Equal(t, int32(2), runs.Load())
	require.Equal(t, int32(3), requests.Load())
	require.Equal(t, "done", finalText)
}

func TestLoopMaxRoundsDoesNotExecuteExtraToolRound(t *testing.T) {
	server, requests := fakeToolSequenceServer(-1)
	defer server.Close()

	var runs atomic.Int32
	engine := newRoundTestEngine(t, server.URL, &runs)
	require.NoError(t, engine.SetMaxRounds(2))

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr, "loop should stop when the model requests a third tool round")
	if !errors.Is(lastErr, ErrMaxRounds) {
		t.Fatalf("expected ErrMaxRounds to be wrapped, got %v", lastErr)
	}
	require.Equal(t, int32(2), runs.Load())
	require.Equal(t, int32(3), requests.Load())

	assistantToolRounds := 0
	for _, msg := range engine.session.BuildContext() {
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
			assistantToolRounds++
		}
	}
	require.Equal(t, 2, assistantToolRounds)
}

func TestLoopContinueAskGrantsAnotherBudget(t *testing.T) {
	server, _ := fakeToolSequenceServer(-1)
	defer server.Close()

	var asks atomic.Int32
	engine, err := NewEngine(EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
		SessionOpts: SessionOpts{Cwd: t.TempDir()},
		Gate:        permission.AllowAll{},
		ContinueAsk: func(context.Context, int) (bool, error) {
			// Approve once so the loop can start a second budget window, then stop.
			return asks.Add(1) == 1, nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, engine.SetMaxRounds(1))

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr)
	require.ErrorIs(t, lastErr, ErrMaxRounds)
	require.Equal(t, int32(2), asks.Load(), "should ask once per exhausted budget")
}

func TestLoopContinueAskDeclineReturnsErrMaxRounds(t *testing.T) {
	server, _ := fakeToolSequenceServer(-1)
	defer server.Close()

	engine, err := NewEngine(EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
		SessionOpts: SessionOpts{Cwd: t.TempDir()},
		Gate:        permission.AllowAll{},
		ContinueAsk: func(context.Context, int) (bool, error) {
			return false, nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, engine.SetMaxRounds(1))

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.ErrorIs(t, lastErr, ErrMaxRounds)
}

func TestSetMaxRoundsRejectsNonPositive(t *testing.T) {
	engine, err := NewEngine(EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: "http://unused", APIKey: "x"},
		SessionOpts: SessionOpts{Cwd: t.TempDir()},
		Gate:        permission.AllowAll{},
	})
	require.NoError(t, err)
	require.Error(t, engine.SetMaxRounds(0))
	require.Error(t, engine.SetMaxRounds(-1))
	require.NoError(t, engine.SetMaxRounds(1))
}

// recorder collects the session events a manager dispatched, in order.
type recorder struct {
	mu     sync.Mutex
	kinds  []hooks.Kind
	events []hooks.SessionEvent
}

func (r *recorder) record(ev hooks.SessionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds = append(r.kinds, ev.Kind)
	r.events = append(r.events, ev)
}

func (r *recorder) seen() []hooks.Kind {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hooks.Kind(nil), r.kinds...)
}

func (r *recorder) first(kind hooks.Kind) (hooks.SessionEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range r.events {
		if ev.Kind == kind {
			return ev, true
		}
	}
	return hooks.SessionEvent{}, false
}

// lifecycleManager wires a recorder to every session kind the engine fires.
func lifecycleManager(t *testing.T, rec *recorder, deny hooks.Kind) *hooks.Manager {
	t.Helper()
	var entries []hooks.Entry
	for _, kind := range []hooks.Kind{
		hooks.KindAgentStart, hooks.KindAgentEnd,
		hooks.KindSessionBeforeCompact, hooks.KindSessionCompact,
	} {
		entries = append(entries, hooks.Entry{
			Kind: kind,
			Hook: hooks.FuncHook{
				HookName: string(kind),
				Sess: func(_ context.Context, ev hooks.SessionEvent) (hooks.SessionResult, error) {
					rec.record(ev)
					if ev.Kind == deny {
						return hooks.SessionResult{Action: hooks.ActionDeny}, nil
					}
					return hooks.SessionResult{Action: hooks.ActionAllow}, nil
				},
			},
		})
	}
	return hooks.NewManager(entries...)
}

// fakeTextServer answers once with plain text and no tool calls.
func fakeTextServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n" +
				"data: [DONE]\n\n"))
	}))
}

func drain(ctx context.Context, t *testing.T, engine *Engine) {
	t.Helper()
	for _, err := range engine.Loop(ctx, "go", LoopOpts{}) {
		if err != nil {
			return
		}
	}
}

// A turn must report its start and its end, in that order.
func TestLoopFiresAgentStartAndEnd(t *testing.T) {
	server := fakeTextServer()
	defer server.Close()

	rec := &recorder{}
	engine, err := NewEngine(EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
		SessionOpts: SessionOpts{Cwd: t.TempDir()},
		Gate:        permission.AllowAll{},
		Hooks:       lifecycleManager(t, rec, ""),
	})
	require.NoError(t, err)

	drain(t.Context(), t, engine)

	assert.Equal(t, []hooks.Kind{hooks.KindAgentStart, hooks.KindAgentEnd}, rec.seen())

	end, ok := rec.first(hooks.KindAgentEnd)
	require.True(t, ok)
	assert.NotEmpty(t, end.SessionID, "the hook must know which session ended")
	assert.NotEmpty(t, end.MessageID, "agent_end identifies the assistant message it ended on")
}

// The turn can stop for many reasons. agent_end must fire for all of them, or
// a hook that allocates on agent_start leaks.
func TestLoopFiresAgentEndWhenCallerStopsEarly(t *testing.T) {
	server := fakeTextServer()
	defer server.Close()

	rec := &recorder{}
	engine, err := NewEngine(EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
		SessionOpts: SessionOpts{Cwd: t.TempDir()},
		Gate:        permission.AllowAll{},
		Hooks:       lifecycleManager(t, rec, ""),
	})
	require.NoError(t, err)

	// Stop consuming after the first event, so Loop returns through the
	// yield-false path rather than running to completion.
	for range engine.Loop(t.Context(), "go", LoopOpts{}) {
		break
	}

	assert.Contains(t, rec.seen(), hooks.KindAgentEnd, "a caller that stops early still ends the turn")
}

// A cancelled turn still ended. The hook must hear about it.
func TestLoopFiresAgentEndOnCancel(t *testing.T) {
	server := fakeTextServer()
	defer server.Close()

	rec := &recorder{}
	engine, err := NewEngine(EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
		SessionOpts: SessionOpts{Cwd: t.TempDir()},
		Gate:        permission.AllowAll{},
		Hooks:       lifecycleManager(t, rec, ""),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	drain(ctx, t, engine)

	assert.Contains(t, rec.seen(), hooks.KindAgentEnd, "a cancelled turn still ends")
}

// A nil manager must not panic: hooks are optional.
func TestLoopWithoutHooks(t *testing.T) {
	server := fakeTextServer()
	defer server.Close()

	engine, err := NewEngine(EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
		SessionOpts: SessionOpts{Cwd: t.TempDir()},
		Gate:        permission.AllowAll{},
	})
	require.NoError(t, err)

	assert.NotPanics(t, func() { drain(t.Context(), t, engine) })
}

// compactServer answers with text and reports usage, so compaction has a
// number to act on.
func compactServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"summary\"}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"total_tokens\":999}}\n\n" +
				"data: [DONE]\n\n"))
	}))
}

// newCompactingEngine forces compaction: a context window of 1 puts the
// threshold at 0, so any reported usage exceeds it.
func newCompactingEngine(t *testing.T, url string, mgr *hooks.Manager) *Engine {
	t.Helper()
	engine, err := NewEngine(EngineOpts{
		Model: llm.ModelConfig{
			Name: "fake", BaseURL: url, APIKey: "x", ContextWindow: 1,
		},
		SessionOpts: SessionOpts{Cwd: t.TempDir()},
		Gate:        permission.AllowAll{},
		Hooks:       mgr,
	})
	require.NoError(t, err)
	return engine
}

// A hook must be able to veto compaction. Denying keeps the turn intact.
func TestCompactionCanBeDenied(t *testing.T) {
	server := compactServer()
	defer server.Close()

	rec := &recorder{}
	engine := newCompactingEngine(t, server.URL, lifecycleManager(t, rec, hooks.KindSessionBeforeCompact))

	drain(t.Context(), t, engine)

	seen := rec.seen()
	assert.Contains(t, seen, hooks.KindSessionBeforeCompact, "the veto hook must be asked")
	assert.NotContains(t, seen, hooks.KindSessionCompact, "a denied compaction must not report success")
}

// session_before_compact asks; session_compact reports. Both must carry the
// session the hook is about to read.
func TestCompactionFiresBeforeAndAfter(t *testing.T) {
	server := compactServer()
	defer server.Close()

	rec := &recorder{}
	engine := newCompactingEngine(t, server.URL, lifecycleManager(t, rec, ""))

	drain(t.Context(), t, engine)

	seen := rec.seen()
	before := slices.Index(seen, hooks.KindSessionBeforeCompact)
	after := slices.Index(seen, hooks.KindSessionCompact)
	require.NotEqual(t, -1, before, "want session_before_compact, got %v", seen)
	require.NotEqual(t, -1, after, "want session_compact, got %v", seen)
	assert.Less(t, before, after, "the veto must be asked before the work is reported")

	ev, ok := rec.first(hooks.KindSessionCompact)
	require.True(t, ok)
	assert.NotEmpty(t, ev.SessionID)
}
