package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/session"
)

func TestParentAskPromptNamesTheChild(t *testing.T) {
	got := parentAskPrompt(job.Info{Meta: job.Meta{
		ID:          "abc",
		Role:        job.RoleWorker,
		Description: "voice agent",
	}}, "which codec?")
	assert.Contains(t, got, "worker")
	assert.Contains(t, got, "voice agent")
	assert.Contains(t, got, "which codec?")
	assert.Contains(t, got, "ask_user_question")
}

func TestAskParentRequiresJob(t *testing.T) {
	c := &Controller{}
	_, err := c.AskParent(t.Context(), "missing", "which codec?")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ask_parent")
}

func TestAssistantTextFromEventUsesThisTurn(t *testing.T) {
	partial := session.AssistantMessageUpdate{Message: session.Message{
		State:   session.StateStreaming,
		Text:    "old",
		Content: []session.ContentBlock{{Type: session.BlockText, Text: "old"}},
	}}
	assert.Empty(t, assistantTextFromEvent(partial))

	done := session.AssistantMessageUpdate{Message: session.Message{
		State:   session.StateComplete,
		Text:    "fresh answer",
		Content: []session.ContentBlock{{Type: session.BlockText, Text: "fresh answer"}},
	}}
	assert.Equal(t, "fresh answer", assistantTextFromEvent(done))
}
