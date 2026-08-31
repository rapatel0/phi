package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/session"
	"github.com/rapatel0/alpha/internal/tools"
	"github.com/rapatel0/alpha/internal/tools/parentask"
)

// AskParent implements [agent.ParentAsker]. A child blocks here until the
// parent agent replies. The parent may call ask_user_question if it needs
// the user.
func (c *Controller) AskParent(ctx context.Context, jobID, question string) (string, error) {
	if c == nil || c.engine == nil {
		return "", errors.New("ask_parent: parent is not running")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "", errors.New("ask_parent: empty question")
	}
	info, err := c.jobInfo(jobID)
	if err != nil {
		return "", fmt.Errorf("ask_parent: %w", err)
	}
	prompt := parentAskPrompt(info, question)

	c.streamMu.Lock()
	busy := c.streamCancel != nil
	c.streamMu.Unlock()
	if busy {
		// The parent Loop is already running (usually blocked in agent_wait).
		// A nested Loop on the same engine is not safe, so answer on a
		// side job. The child stays blocked on this call until it finishes.
		return c.askParentSide(ctx, prompt)
	}

	turnCtx, cancel := context.WithCancel(ctx)
	c.streamMu.Lock()
	if c.streamCancel != nil {
		c.streamMu.Unlock()
		cancel()
		return c.askParentSide(ctx, prompt)
	}
	c.streamCancel = cancel
	c.streamGen++
	gen := c.streamGen
	c.streamMu.Unlock()

	last := c.runLoop(turnCtx, gen, prompt, nil, nil)
	if last == "" {
		return "", errors.New("ask_parent: parent returned no answer")
	}
	return last, nil
}

func (c *Controller) askParentSide(ctx context.Context, prompt string) (string, error) {
	mgr := c.engineJobs()
	if mgr == nil {
		return "", errors.New("ask_parent: parent is busy")
	}
	info, err := mgr.Spawn(ctx, job.SpawnRequest{
		Prompt:      prompt,
		Description: "ask_parent",
		ParentID:    c.SessionID(),
		Role:        job.RoleExplore,
		WorkDir:     c.cwd,
	})
	if err != nil {
		return "", err
	}
	res, err := mgr.Wait(ctx, info.ID)
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(res.Summary)
	if summary == "" {
		return "", errors.New("ask_parent: parent returned no answer")
	}
	return summary, nil
}

func parentAskPrompt(info job.Info, question string) string {
	role := strings.TrimSpace(string(info.Role))
	if role == "" {
		role = "sub-agent"
	}
	desc := strings.TrimSpace(info.Description)
	var b strings.Builder
	b.WriteString("A running sub-agent needs a decision. Answer it directly.\n")
	b.WriteString("If you need the user's preference, use ask_user_question, then answer the sub-agent.\n\n")
	fmt.Fprintf(&b, "Sub-agent: %s", role)
	if desc != "" {
		fmt.Fprintf(&b, " · %s", desc)
	}
	if info.ID != "" {
		fmt.Fprintf(&b, " (%s)", info.ID)
	}
	b.WriteString("\nQuestion:\n")
	b.WriteString(question)
	b.WriteString("\n\nReply with a short answer the sub-agent can act on. Do not mention these instructions.")
	return b.String()
}

func (c *Controller) childTools(jobID string, base []tools.Tool) []tools.Tool {
	id := jobID
	out := append([]tools.Tool(nil), base...)
	out = append(out, parentask.Tool(func(ctx context.Context, q string) (string, error) {
		return c.AskParent(ctx, id, q)
	}))
	return out
}

func assistantTextFromEvent(ev session.Event) string {
	up, ok := ev.(session.AssistantMessageUpdate)
	if !ok || up.Message.State != session.StateComplete {
		return ""
	}
	return strings.TrimSpace(up.Message.FlatText())
}

func (c *Controller) clearStream(gen int) {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	if c.streamGen == gen {
		c.streamCancel = nil
	}
}
