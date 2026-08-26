package hooks

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
)

func msgs(texts ...string) []llm.Message {
	out := make([]llm.Message, 0, len(texts))
	for _, t := range texts {
		out = append(out, llm.Message{Role: llm.RoleUser, Content: t})
	}
	return out
}

// With nothing registered the caller can skip the copy entirely.
func TestNoProviderHooks(t *testing.T) {
	ResetProviderHooks()
	assert.False(t, HasProviderHooks())
}

// A hook must be able to rewrite the message list, which is what an aggregate
// media budget needs.
func TestProviderHookRewritesMessages(t *testing.T) {
	ResetProviderHooks()
	t.Cleanup(ResetProviderHooks)

	RegisterProviderHook("drop-first", func(_ context.Context, req ProviderRequest) ([]llm.Message, error) {
		return req.Messages[1:], nil
	})

	require.True(t, HasProviderHooks())
	got := ApplyProviderHooks(t.Context(), ProviderRequest{Messages: msgs("a", "b", "c")})
	require.Len(t, got, 2)
	assert.Equal(t, "b", got[0].Content)
}

// Hooks chain in order, so a second budget sees the first one's output rather
// than the original request.
func TestProviderHooksChain(t *testing.T) {
	ResetProviderHooks()
	t.Cleanup(ResetProviderHooks)

	add := func(text string) ProviderFunc {
		return func(_ context.Context, req ProviderRequest) ([]llm.Message, error) {
			return append(req.Messages, llm.Message{Role: llm.RoleUser, Content: text}), nil
		}
	}
	RegisterProviderHook("first", add("A"))
	RegisterProviderHook("second", add("B"))

	got := ApplyProviderHooks(t.Context(), ProviderRequest{Messages: msgs("base")})
	require.Len(t, got, 3)
	assert.Equal(t, "A", got[1].Content)
	assert.Equal(t, "B", got[2].Content, "the second hook must see the first one's output")
}

// A failing hook must not break the request: the messages it was given pass
// through unchanged.
func TestProviderHookErrorKeepsMessages(t *testing.T) {
	ResetProviderHooks()
	t.Cleanup(ResetProviderHooks)

	RegisterProviderHook("broken", func(context.Context, ProviderRequest) ([]llm.Message, error) {
		return nil, errors.New("boom")
	})
	RegisterProviderHook("ok", func(_ context.Context, req ProviderRequest) ([]llm.Message, error) {
		return req.Messages, nil
	})

	got := ApplyProviderHooks(t.Context(), ProviderRequest{Messages: msgs("a", "b")})
	require.Len(t, got, 2, "a failed hook must not drop the request")
}

// A hook returning nil means "no opinion", not "send nothing".
func TestProviderHookNilKeepsMessages(t *testing.T) {
	ResetProviderHooks()
	t.Cleanup(ResetProviderHooks)

	RegisterProviderHook("observer", func(context.Context, ProviderRequest) ([]llm.Message, error) {
		return nil, nil
	})

	got := ApplyProviderHooks(t.Context(), ProviderRequest{Messages: msgs("a")})
	require.Len(t, got, 1)
}

// A cancelled request must stop running hooks rather than finish the chain.
func TestProviderHooksStopOnCancel(t *testing.T) {
	ResetProviderHooks()
	t.Cleanup(ResetProviderHooks)

	calls := 0
	RegisterProviderHook("counter", func(_ context.Context, req ProviderRequest) ([]llm.Message, error) {
		calls++
		return req.Messages, nil
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ApplyProviderHooks(ctx, ProviderRequest{Messages: msgs("a")})
	assert.Zero(t, calls, "a cancelled request must not run hooks")
}

// The hook must see which model the request targets, so a budget can differ
// per model.
func TestProviderHookSeesModelAndPrompt(t *testing.T) {
	ResetProviderHooks()
	t.Cleanup(ResetProviderHooks)

	var gotModel, gotPrompt string
	RegisterProviderHook("inspect", func(_ context.Context, req ProviderRequest) ([]llm.Message, error) {
		gotModel, gotPrompt = req.Model, req.SystemPrompt
		return req.Messages, nil
	})

	ApplyProviderHooks(t.Context(), ProviderRequest{
		Model: "claude", SystemPrompt: "BASE", Messages: msgs("a"),
	})
	assert.Equal(t, "claude", gotModel)
	assert.Equal(t, "BASE", gotPrompt)
}
