package hooks

import (
	"context"
	"sync"

	"github.com/rapatel0/alpha/internal/debuglog"
	"github.com/rapatel0/alpha/internal/llm"
)

// ProviderRequest is the model request a hook may rewrite. It carries the
// assembled message list rather than a provider payload, so one hook covers
// every provider instead of needing four implementations.
type ProviderRequest struct {
	// Provider is the backend about to receive the request, for example
	// "anthropic" or "gemini". A hook needs it because provider limits
	// differ, and the model name alone does not identify the backend.
	// It is empty when the backend is unknown.
	Provider     string
	Model        string
	SystemPrompt string
	Messages     []llm.Message
}

// ProviderFunc rewrites a model request. Returning the messages unchanged, or
// an error, leaves the request alone: a budget must never cost a turn.
//
// The returned slice replaces the request's messages. A hook that only wants
// to observe should return the input.
type ProviderFunc func(ctx context.Context, req ProviderRequest) ([]llm.Message, error)

// providerHooks is the process-wide before_provider_request registry.
//
// It is separate from Manager entries because the callback shape differs from
// the four Hook methods: adding it to the interface would break every
// implementer for one event.
var providerHooks struct {
	mu   sync.RWMutex
	fns  []ProviderFunc
	name []string
}

// RegisterProviderHook adds a before_provider_request rewriter. Hooks run in
// registration order and each sees the previous one's output.
func RegisterProviderHook(name string, fn ProviderFunc) {
	if fn == nil {
		return
	}
	providerHooks.mu.Lock()
	defer providerHooks.mu.Unlock()
	providerHooks.fns = append(providerHooks.fns, fn)
	providerHooks.name = append(providerHooks.name, name)
}

// ResetProviderHooks clears the registry. Tests use it; production registers
// once at startup.
func ResetProviderHooks() {
	providerHooks.mu.Lock()
	defer providerHooks.mu.Unlock()
	providerHooks.fns = nil
	providerHooks.name = nil
}

// HasProviderHooks reports whether any rewriter is registered, so a caller can
// skip copying the message slice when nothing would change it.
func HasProviderHooks() bool {
	providerHooks.mu.RLock()
	defer providerHooks.mu.RUnlock()
	return len(providerHooks.fns) > 0
}

// ApplyProviderHooks runs every registered rewriter in order and returns the
// resulting messages.
//
// A hook that fails is logged and skipped, keeping the messages it was given:
// a media budget or an audit must not be able to break a request.
func ApplyProviderHooks(ctx context.Context, req ProviderRequest) []llm.Message {
	providerHooks.mu.RLock()
	fns := append([]ProviderFunc(nil), providerHooks.fns...)
	names := append([]string(nil), providerHooks.name...)
	providerHooks.mu.RUnlock()

	msgs := req.Messages
	for i, fn := range fns {
		if ctx.Err() != nil {
			break
		}
		// Copy the request and swap only the messages, so a field added to
		// ProviderRequest reaches hooks without another edit here. Rebuilding
		// it field by field silently dropped Provider once already.
		cur := req
		cur.Messages = msgs
		next, err := fn(ctx, cur)
		if err != nil {
			debuglog.Logf("hooks: %s BeforeProviderRequest: %v", names[i], err)
			continue
		}
		if next != nil {
			msgs = next
		}
	}
	return msgs
}
