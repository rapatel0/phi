package mediaguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/llm"
)

// The budget must apply through the real Host and the provider hook registry,
// not just through Apply: a wiring mistake would pass every unit test.
func TestPluginTrimsThroughHost(t *testing.T) {
	hooks.ResetProviderHooks()
	t.Cleanup(hooks.ResetProviderHooks)

	p := &Plugin{}
	require.NoError(t, p.Register(ext.NewHost()))

	// Gemini has the smallest budget, so build a payload that exceeds it.
	big := BudgetFor("gemini").MaxBytes
	got := hooks.ApplyProviderHooks(t.Context(), hooks.ProviderRequest{
		Provider: "gemini",
		Messages: []llm.Message{img("old.png", big), img("new.png", big)},
	})

	require.Len(t, got, 2)
	assert.Empty(t, got[0].Images, "the older image must be trimmed")
	assert.Len(t, got[1].Images, 1)
}

// The hook must size the budget from the request's provider. Using a stored
// default would apply one provider's limit to another's request.
func TestPluginUsesTheRequestProvider(t *testing.T) {
	hooks.ResetProviderHooks()
	t.Cleanup(hooks.ResetProviderHooks)

	p := &Plugin{}
	require.NoError(t, p.Register(ext.NewHost()))

	// A payload that fits OpenAI's budget but not Gemini's.
	size := BudgetFor("gemini").MaxBytes - 1

	got := hooks.ApplyProviderHooks(t.Context(), hooks.ProviderRequest{
		Provider: "openai",
		Messages: []llm.Message{img("a.png", size), img("b.png", size)},
	})
	assert.Len(t, got[0].Images, 1, "openai has room for both")
	_, _, provider, _ := p.led.snapshot()
	assert.Equal(t, "openai", provider)

	got = hooks.ApplyProviderHooks(t.Context(), hooks.ProviderRequest{
		Provider: "gemini",
		Messages: []llm.Message{img("a.png", size), img("b.png", size)},
	})
	assert.Empty(t, got[0].Images, "gemini cannot fit both")
	_, _, provider, _ = p.led.snapshot()
	assert.Equal(t, "gemini", provider)
}

// /media before any request must say so rather than report a zero budget as if
// it were a real measurement.
func TestCommandBeforeAnyRequest(t *testing.T) {
	p := &Plugin{}
	res, err := p.run(t.Context(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "No model request")
}

// After a request /media reports what happened and reassures that the session
// still holds the media.
func TestCommandReportsLastDecision(t *testing.T) {
	p := &Plugin{}
	b := Budget{MaxBytes: 150, MaxImages: 10}
	_, d := Apply([]llm.Message{img("old.png", 100), img("new.png", 100)}, b)
	p.led.record(d, b, "anthropic")

	res, err := p.run(t.Context(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "Media budget")
	assert.Contains(t, res.Toast, "anthropic", "the report must name the provider it applied")
	assert.Contains(t, res.Toast, "1 of 2 img")
	assert.Contains(t, res.Toast, "stays in the session")
}

// The footer is a scarce slot: it must stay quiet unless the budget acted.
func TestFooterOnlySpeaksWhenItTrims(t *testing.T) {
	hooks.ResetProviderHooks()
	t.Cleanup(hooks.ResetProviderHooks)

	p := &Plugin{}
	h := ext.NewHost()
	require.NoError(t, p.Register(h))

	bits := h.FooterBits()
	assert.Empty(t, bits, "no request yet, so nothing to report")

	p.led.record(Decision{ImagesBefore: 3, ImagesKept: 1, BytesKept: 10}, DefaultBudget(), "anthropic")
	require.Len(t, h.FooterBits(), 1)
	assert.Contains(t, h.FooterBits()[0], "media:")

	p.led.record(Decision{ImagesBefore: 2, ImagesKept: 2, BytesKept: 10}, DefaultBudget(), "anthropic")
	assert.Empty(t, h.FooterBits(), "nothing was trimmed, so stay quiet")
}

// The whole path must work through the real hook registry: a GIF sent to xAI
// arrives as PNG, because xAI documents JPEG and PNG only.
func TestPluginConvertsFormatsThroughHost(t *testing.T) {
	hooks.ResetProviderHooks()
	t.Cleanup(hooks.ResetProviderHooks)

	p := &Plugin{}
	require.NoError(t, p.Register(ext.NewHost()))

	got := hooks.ApplyProviderHooks(t.Context(), hooks.ProviderRequest{
		Provider: "xai",
		Messages: []llm.Message{gifMessage(t, "shot.gif")},
	})
	require.Len(t, got[0].Images, 1)
	assert.Equal(t, "image/png", got[0].Images[0].MIME)

	// The same image is left alone for a provider that documents GIF.
	got = hooks.ApplyProviderHooks(t.Context(), hooks.ProviderRequest{
		Provider: "openai",
		Messages: []llm.Message{gifMessage(t, "shot.gif")},
	})
	assert.Equal(t, "image/gif", got[0].Images[0].MIME)
}
