package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/agent"
	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/llm"
)

func TestNewEngineRegistersJobs(t *testing.T) {
	mgr, err := job.New(job.Options{
		Root: t.TempDir(),
		Runner: job.RunnerFunc(func(_ context.Context, _ job.RunEnv) (string, error) {
			return "ok", nil
		}),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close(t.Context()) })

	eng, err := agent.NewEngine(agent.EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:9", APIKey: "x"},
		SessionOpts: agent.SessionOpts{Cwd: t.TempDir()},
		Jobs:        mgr,
	})
	require.NoError(t, err)
	assert.Same(t, mgr, eng.Jobs())
	assert.True(t, eng.HasTool("agent_spawn"))
	assert.True(t, eng.HasTool("read_image"))

	require.NoError(t, eng.SetModel(llm.ModelConfig{Name: "fake2", BaseURL: "http://127.0.0.1:9", APIKey: "x"}))
	assert.Same(t, mgr, eng.Jobs())
	assert.True(t, eng.HasTool("agent_spawn"))
}

func TestSetJobsTogglesAgentTools(t *testing.T) {
	mgr, err := job.New(job.Options{
		Root: t.TempDir(),
		Runner: job.RunnerFunc(func(_ context.Context, _ job.RunEnv) (string, error) {
			return "ok", nil
		}),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close(t.Context()) })

	eng, err := agent.NewEngine(agent.EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:9", APIKey: "x"},
		SessionOpts: agent.SessionOpts{Cwd: t.TempDir()},
	})
	require.NoError(t, err)
	assert.False(t, eng.HasTool("agent_spawn"))

	eng.SetJobs(mgr)
	assert.Same(t, mgr, eng.Jobs())
	assert.True(t, eng.HasTool("agent_spawn"))

	eng.SetJobs(nil)
	assert.Nil(t, eng.Jobs())
	assert.False(t, eng.HasTool("agent_spawn"))
	assert.True(t, eng.HasTool("bash")) // default tools still present
}

func TestChildToolsHaveNoAgent(t *testing.T) {
	for _, tool := range agent.ChildTools() {
		assert.NotContains(t, tool.Definition.Name, "agent_")
	}
}

func TestChildToolsAreReadonly(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range agent.ChildTools() {
		names[tool.Definition.Name] = true
	}
	assert.True(t, names["read"])
	assert.True(t, names["read_image"])
	assert.True(t, names["grep"])
	assert.True(t, names["bash"])
	assert.True(t, names["skill"])
	assert.True(t, names["webfetch"])
	assert.True(t, names["websearch"])
	assert.False(t, names["write"])
	assert.False(t, names["edit"])
}

func TestChildSetModelKeepsReadonlyTools(t *testing.T) {
	eng, err := agent.NewEngine(agent.EngineOpts{
		Model:       llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:9", APIKey: "x"},
		SessionOpts: agent.SessionOpts{Cwd: t.TempDir()},
		Tools:       agent.ChildTools(),
	})
	require.NoError(t, err)
	assert.False(t, eng.HasTool("agent_spawn"))
	assert.False(t, eng.HasTool("edit"))
	assert.True(t, eng.HasTool("read"))

	require.NoError(t, eng.SetModel(llm.ModelConfig{Name: "fake2", BaseURL: "http://127.0.0.1:9", APIKey: "x"}))
	assert.False(t, eng.HasTool("agent_spawn"))
	assert.False(t, eng.HasTool("edit"))
	assert.True(t, eng.HasTool("read"))
}
