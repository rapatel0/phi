package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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
		info = job.Info{}
		info.ID = jobID
	}
	prompt := parentAskPrompt(info, question)
	if err := c.waitParentIdle(ctx, 2*time.Minute); err != nil {
		return "", err
	}

	turnCtx, cancel := context.WithCancel(ctx)
	c.streamMu.Lock()
	if c.streamCancel != nil {
		c.streamMu.Unlock()
		cancel()
		return "", errors.New("ask_parent: parent started another turn")
	}
	c.streamCancel = cancel
	c.streamGen++
	gen := c.streamGen
	c.streamMu.Unlock()

	last := c.runLoopCollect(turnCtx, gen, prompt)
	if last == "" {
		return "", errors.New("ask_parent: parent returned no answer")
	}
	return last, nil
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

func (c *Controller) waitParentIdle(ctx context.Context, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		c.streamMu.Lock()
		busy := c.streamCancel != nil
		c.streamMu.Unlock()
		if !busy {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("ask_parent: parent is busy")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(80 * time.Millisecond):
		}
	}
}

func (c *Controller) childTools(jobID string, base []tools.Tool) []tools.Tool {
	id := jobID
	out := append([]tools.Tool(nil), base...)
	out = append(out, parentask.Tool(func(ctx context.Context, q string) (string, error) {
		return c.AskParent(ctx, id, q)
	}))
	return out
}

func (c *Controller) runLoopCollect(ctx context.Context, gen int, prompt string) string {
	defer c.clearStream(gen)
	c.runLoop(ctx, gen, prompt, nil, nil)
	if c.engine == nil || c.engine.Session() == nil {
		return ""
	}
	snap := session.SnapshotFromEntries(c.engine.Session().PathEntries())
	for _, m := range slices.Backward(snap.Messages) {
		if m.Role != session.RoleAssistant {
			continue
		}
		if t := strings.TrimSpace(m.FlatText()); t != "" {
			return t
		}
	}
	return ""
}

func (c *Controller) clearStream(gen int) {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	if c.streamGen == gen {
		c.streamCancel = nil
	}
}
