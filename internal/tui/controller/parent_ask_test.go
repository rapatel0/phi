package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rapatel0/alpha/internal/job"
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
