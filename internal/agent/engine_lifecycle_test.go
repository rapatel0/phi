package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/permission"
)

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
