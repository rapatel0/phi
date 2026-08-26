// Package btw is the pi-btw analog: a /btw side conversation.
//
// A side question ("btw, what does this env var do?") does not belong in the
// main thread. Asking it there spends context the main task needs and drags
// the answer into every later turn.
//
// /btw runs the question as a sub-agent and returns only its summary, so the
// exchange stays out of the main agent's context. The child transcript lands
// under ~/.alpha/jobs like any other job, so the existing popup can view it.
package btw

import (
	"errors"
	"strings"
	"sync"
)

// Thread records the side exchanges of this session, so /btw list can show
// what was asked without reopening each job.
type Thread struct {
	mu    sync.RWMutex
	turns []Turn
}

// Turn is one side question and its answer.
type Turn struct {
	Prompt  string
	Summary string
	JobID   string
}

func (t *Thread) add(turn Turn) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turns = append(t.turns, turn)
}

// Turns returns a copy of the thread.
func (t *Thread) Turns() []Turn {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return append([]Turn(nil), t.turns...)
}

// Len reports how many side exchanges happened.
func (t *Thread) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.turns)
}

// reset clears the thread, for /btw clear.
func (t *Thread) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turns = nil
}

// Args is a parsed /btw invocation.
type Args struct {
	Prompt string
	// Tangent starts a side thread that does not inherit the main
	// conversation, for a question unrelated to the current work.
	Tangent bool
	// Sub is the subcommand: "", "list", or "clear".
	Sub string
}

// ParseArgs reads a /btw invocation.
//
// The first word selects a subcommand only when it is one: "btw list" lists,
// but "btw list the open files" is a question, because a subcommand takes no
// further words.
func ParseArgs(args []string) Args {
	var a Args
	rest := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--tangent", ":tangent":
			a.Tangent = true
		default:
			rest = append(rest, arg)
		}
	}
	if len(rest) == 1 {
		switch strings.ToLower(rest[0]) {
		case "list", "clear":
			a.Sub = strings.ToLower(rest[0])
			return a
		}
	}
	a.Prompt = strings.TrimSpace(strings.Join(rest, " "))
	return a
}

// Render formats the thread for display.
func Render(turns []Turn) string {
	if len(turns) == 0 {
		return "No side conversations yet. Ask one with /btw <question>."
	}
	var b strings.Builder
	b.WriteString("Side conversation:\n")
	for i, t := range turns {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("· ")
		b.WriteString(firstLine(t.Prompt))
		b.WriteString("\n  ")
		b.WriteString(firstLine(t.Summary))
	}
	return b.String()
}

// firstLine keeps a listing to one line per entry.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no answer)"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 100
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

var errNoPrompt = errors.New("ask something: /btw <question>, or /btw list")
