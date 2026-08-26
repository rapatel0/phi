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
	p.budget = Budget{MaxBytes: 150, MaxImages: 10}

	got := hooks.ApplyProviderHooks(t.Context(), hooks.ProviderRequest{
		Messages: []llm.Message{img("old.png", 100), img("new.png", 100)},
	})

	require.Len(t, got, 2)
	assert.Empty(t, got[0].Images, "the older image must be trimmed")
	assert.Len(t, got[1].Images, 1)
}

// /media before any request must say so rather than report a zero budget as if
// it were a real measurement.
func TestCommandBeforeAnyRequest(t *testing.T) {
	p := &Plugin{budget: DefaultBudget()}
	res, err := p.run(t.Context(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "No model request")
}

// After a request /media reports what happened and reassures that the session
// still holds the media.
func TestCommandReportsLastDecision(t *testing.T) {
	p := &Plugin{budget: Budget{MaxBytes: 150, MaxImages: 10}}
	_, d := Apply([]llm.Message{img("old.png", 100), img("new.png", 100)}, p.budget)
	p.led.record(d)

	res, err := p.run(t.Context(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "Media budget:")
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

	p.led.record(Decision{ImagesBefore: 3, ImagesKept: 1, BytesKept: 10})
	require.Len(t, h.FooterBits(), 1)
	assert.Contains(t, h.FooterBits()[0], "media:")

	p.led.record(Decision{ImagesBefore: 2, ImagesKept: 2, BytesKept: 10})
	assert.Empty(t, h.FooterBits(), "nothing was trimmed, so stay quiet")
}
