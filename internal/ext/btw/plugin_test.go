package btw

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/ext"
)

// newPlugin wires a plugin to a host whose side channel is a stub, so the
// command path is testable without a job manager.
func newPlugin(t *testing.T, side ext.SideFunc) *Plugin {
	t.Helper()
	p := &Plugin{}
	h := ext.NewHost()
	require.NoError(t, p.Register(h))
	h.SetSideChannel(side)
	return p
}

func TestRunAsksAndRecords(t *testing.T) {
	var gotReq ext.SideRequest
	p := newPlugin(t, func(_ context.Context, req ext.SideRequest) (ext.SideResult, error) {
		gotReq = req
		return ext.SideResult{JobID: "job-1", Summary: "PATH lists search dirs."}, nil
	})

	res, err := p.run(t.Context(), []string{"what", "is", "PATH?"})
	require.NoError(t, err)
	assert.Equal(t, "PATH lists search dirs.", res.Toast)
	assert.Equal(t, "what is PATH?", gotReq.Prompt)
	assert.True(t, gotReq.Inherit, "a normal aside carries the main thread as background")

	require.Equal(t, 1, p.thread.Len())
	assert.Equal(t, "job-1", p.thread.Turns()[0].JobID)
}

// A tangent must not inherit the main conversation: that is the whole point.
func TestRunTangentDoesNotInherit(t *testing.T) {
	var gotReq ext.SideRequest
	p := newPlugin(t, func(_ context.Context, req ext.SideRequest) (ext.SideResult, error) {
		gotReq = req
		return ext.SideResult{Summary: "ok"}, nil
	})

	_, err := p.run(t.Context(), []string{"--tangent", "unrelated"})
	require.NoError(t, err)
	assert.False(t, gotReq.Inherit)
}

// An empty invocation must explain the usage rather than spawn a blank job.
func TestRunWithoutPrompt(t *testing.T) {
	called := false
	p := newPlugin(t, func(context.Context, ext.SideRequest) (ext.SideResult, error) {
		called = true
		return ext.SideResult{}, nil
	})

	_, err := p.run(t.Context(), nil)
	require.ErrorIs(t, err, errNoPrompt)
	assert.False(t, called, "no prompt means no job")
}

// A failed side run must surface the error and leave the thread untouched.
func TestRunFailureIsNotRecorded(t *testing.T) {
	p := newPlugin(t, func(context.Context, ext.SideRequest) (ext.SideResult, error) {
		return ext.SideResult{}, errors.New("spawn failed")
	})

	_, err := p.run(t.Context(), []string{"question"})
	require.Error(t, err)
	assert.Zero(t, p.thread.Len(), "a failed aside is not part of the thread")
}

// A child that produced no summary must say so rather than showing a blank
// toast that looks like success.
func TestRunEmptySummaryExplains(t *testing.T) {
	p := newPlugin(t, func(context.Context, ext.SideRequest) (ext.SideResult, error) {
		return ext.SideResult{JobID: "job-9"}, nil
	})

	res, err := p.run(t.Context(), []string{"question"})
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "no summary")
}

// Without a side channel the command must fail with a clear reason, which is
// the headless case.
func TestRunWithoutSideChannel(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Register(ext.NewHost()))

	_, err := p.run(t.Context(), []string{"question"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interactive shell")
}

func TestRunListAndClear(t *testing.T) {
	p := newPlugin(t, func(context.Context, ext.SideRequest) (ext.SideResult, error) {
		return ext.SideResult{Summary: "answer"}, nil
	})
	_, err := p.run(t.Context(), []string{"question"})
	require.NoError(t, err)

	res, err := p.run(t.Context(), []string{"list"})
	require.NoError(t, err)
	assert.Contains(t, res.Toast, "question")

	_, err = p.run(t.Context(), []string{"clear"})
	require.NoError(t, err)
	assert.Zero(t, p.thread.Len())
}

// The footer must stay quiet until there is something to report.
func TestFooterCountsAsides(t *testing.T) {
	p := &Plugin{}
	h := ext.NewHost()
	require.NoError(t, p.Register(h))
	assert.Empty(t, h.FooterBits())

	p.thread.add(Turn{Prompt: "q", Summary: "a"})
	require.Len(t, h.FooterBits(), 1)
	assert.Equal(t, "btw:1 aside", h.FooterBits()[0])
}
