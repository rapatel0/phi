package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/agent"
	"github.com/pulseaiclub/phi/internal/job"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/session"
)

func attachSSE(reply string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		content := strings.ReplaceAll(reply, `\`, `\\`)
		content = strings.ReplaceAll(content, `"`, `\"`)
		content = strings.ReplaceAll(content, "\n", `\n`)
		_, _ = fmt.Fprintf(
			w,
			"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"%s\"}}]}\n\n",
			content,
		)
		_, _ = fmt.Fprint(
			w,
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n",
		)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func TestAttachAndFollowUp(t *testing.T) {
	srv := attachSSE("child found login.go")
	defer srv.Close()

	c := &Controller{
		bus:      NewBus(nil),
		children: newChildRegistry(),
		cwd:      t.TempDir(),
		modelCfg: llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
	}
	mgr, err := job.New(job.Options{
		Root: t.TempDir(),
		Runner: agent.EngineRunner{
			Model:     c.modelCfg,
			MaxRounds: 4,
			Hub:       c,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close(t.Context()) })
	c.jobs = mgr

	info, err := mgr.Spawn(t.Context(), job.SpawnRequest{
		Prompt:      "Look at auth",
		Description: "auth explore",
		WorkDir:     t.TempDir(),
	})
	require.NoError(t, err)
	res, err := mgr.Wait(t.Context(), info.ID)
	require.NoError(t, err)
	assert.Equal(t, job.StatusCompleted, res.Info.Status)

	snap, got, err := c.Attach(info.ID)
	require.NoError(t, err)
	assert.Equal(t, info.ID, c.AttachedID())
	assert.Equal(t, "auth explore", got.Description)
	assert.True(t, c.CanEnqueue())
	var texts []string
	for _, m := range snap.Messages {
		texts = append(texts, m.Text)
	}
	assert.Contains(t, strings.Join(texts, "\n"), "login.go")

	c.StartPrompt("now look at oauth", nil, nil)
	deadline := time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		for _, m := range c.bus.Drain() {
			ev, ok := m.(SessionEventMsg)
			if !ok || ev.JobID != info.ID {
				continue
			}
			if up, ok := ev.Event.(session.AssistantMessageUpdate); ok {
				if strings.Contains(up.Message.FlatText(), "login.go") && up.Message.State == session.StateComplete {
					found = true
				}
			}
		}
		if found {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.True(t, found, "expected child follow-up events on the bus")

	c.Detach()
	assert.Empty(t, c.AttachedID())
	assert.False(t, c.CanEnqueue())
}

func TestEmitChildAlwaysPublishes(t *testing.T) {
	c := &Controller{
		bus:      NewBus(nil),
		children: newChildRegistry(),
	}
	c.BindChild(job.Meta{ID: "j1"}, nil)
	c.EmitChild("j1", session.UserAppend{Text: "hello"})
	batch := c.bus.Drain()
	require.Len(t, batch, 1)
	ev := batch[0].(SessionEventMsg)
	assert.Equal(t, "j1", ev.JobID)
}

func TestSubmitChildQueuesWhileSpawning(t *testing.T) {
	c := &Controller{
		bus:      NewBus(nil),
		children: newChildRegistry(),
	}
	c.BindChild(job.Meta{ID: "j1", Role: job.RoleExplore}, nil)
	c.streamMu.Lock()
	c.attachedID = "j1"
	c.attachedInfo = job.Info{Meta: job.Meta{ID: "j1"}}
	c.streamMu.Unlock()

	c.submitChild("steer this", nil, nil)
	slot := c.children.get("j1")
	require.NotNil(t, slot)
	slot.mu.Lock()
	n := len(slot.inbox)
	slot.mu.Unlock()
	assert.Equal(t, 1, n)

	c.cancelChild()
	slot.mu.Lock()
	n = len(slot.inbox)
	slot.mu.Unlock()
	assert.Equal(t, 0, n)
}

func TestAttachUnknownJob(t *testing.T) {
	c := &Controller{children: newChildRegistry()}
	_, _, err := c.Attach("missing")
	assert.Error(t, err)
}
