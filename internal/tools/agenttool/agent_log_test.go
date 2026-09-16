package agenttool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/tools"
)

// logMgr returns a manager whose runner writes two progress lines per job.
func logMgr(t *testing.T) *job.Manager {
	t.Helper()
	mgr, err := job.New(job.Options{
		Root: t.TempDir(),
		Runner: job.RunnerFunc(func(_ context.Context, env job.RunEnv) (string, error) {
			env.Log("step one")
			env.Log("step two")
			return "done", nil
		}),
	})
	require.NoError(t, err)
	// The test context is already canceled when cleanups run, so close on a
	// fresh context: a canceled close returns before the job store drains
	// and the temp-dir cleanup then fails on a half-written directory.
	t.Cleanup(func() { _ = mgr.Close(t.Context()) })
	return mgr
}

func spawnJob(t *testing.T, reg tools.Registry, desc string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"prompt": "p", "description": desc})
	require.NoError(t, err)
	res, err := reg["agent_spawn"].Run(t.Context(), raw)
	require.NoError(t, err)
	var out struct {
		JobID string `json:"job_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	return out.JobID
}

func TestAgentLogReturnsTailLines(t *testing.T) {
	mgr := logMgr(t)
	reg := tools.NewRegistry(tools.AgentTools(tools.AgentDeps{Manager: mgr}))

	id := spawnJob(t, reg, "logging")
	_, err := reg["agent_wait"].Run(t.Context(), json.RawMessage(`{"job_id":"`+id+`","timeout_sec":20}`))
	require.NoError(t, err)

	res, err := reg["agent_log"].Run(t.Context(), json.RawMessage(`{"job_id":"`+id+`"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "step one")
	assert.Contains(t, res.Content, "step two")

	// The manager writes its own lifecycle lines, so assert Detail agrees with
	// the payload instead of pinning one number.
	var out struct {
		Lines []string `json:"lines"`
		Count int      `json:"count"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	assert.Equal(t, len(out.Lines), out.Count)
	assert.Equal(t, fmt.Sprintf("%d lines", out.Count), res.Detail)
	assert.True(t, reg["agent_log"].Definition.Readable, "log reads must overlap with other reads")
}

func TestAgentLogCapsTailLines(t *testing.T) {
	mgr := logMgr(t)
	reg := tools.NewRegistry(tools.AgentTools(tools.AgentDeps{Manager: mgr}))
	id := spawnJob(t, reg, "cap")
	_, err := reg["agent_wait"].Run(t.Context(), json.RawMessage(`{"job_id":"`+id+`","timeout_sec":20}`))
	require.NoError(t, err)

	res, err := reg["agent_log"].Run(t.Context(), json.RawMessage(`{"job_id":"`+id+`","tail_lines":1}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, `"count": 1`)
}

func TestAgentLogRequiresJobID(t *testing.T) {
	mgr := logMgr(t)
	reg := tools.NewRegistry(tools.AgentTools(tools.AgentDeps{Manager: mgr}))
	_, err := reg["agent_log"].Run(t.Context(), json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "job_id is required")
}
