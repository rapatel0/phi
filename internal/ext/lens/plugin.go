package lens

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

func init() { ext.Register(&Plugin{}) }

const (
	// checkTimeout bounds one checker. A checker that takes longer than
	// this is not useful after an edit: the model is waiting on it.
	checkTimeout = 10 * time.Second

	// maxReported caps how many problems go to the model. A file with more
	// than this is usually mid-refactor, and a wall of findings buries the
	// first one, which is normally the cause of the rest.
	maxReported = 10
)

// Plugin reports code problems to the model after it writes a file.
type Plugin struct {
	mu    sync.Mutex
	last  []Problem
	files int
}

// Name identifies the extension.
func (*Plugin) Name() string { return "lens" }

// Register wires the post-tool hook and the /lens command.
func (p *Plugin) Register(h *ext.Host) error {
	h.OnToolResult("edit,write", p.check)
	h.RegisterCommand(ext.Command{
		Name:        "lens",
		Description: "Show the problems found in the file last written",
		Run:         p.report,
	})
	return nil
}

// check runs the checkers for the file that was just written and returns a
// note for the model.
//
// A failed edit is skipped: the file on disk is not what the model tried to
// write, so any finding would describe the previous content.
func (p *Plugin) check(ctx context.Context, ev hooks.Event) (string, error) {
	if ev.Err != "" {
		return "", nil
	}
	path := pathFrom(ev.Input)
	if path == "" {
		return "", nil
	}

	problems := Check(ctx, ev.Cwd, path, checkTimeout)

	p.mu.Lock()
	p.last = problems
	p.files++
	p.mu.Unlock()

	return note(problems), nil
}

// note renders problems for the model, or "" when there is nothing to say.
//
// Silence on success is deliberate. A note after every clean edit trains the
// model to skim past the section, which is exactly where the real findings
// appear.
func note(problems []Problem) string {
	if len(problems) == 0 {
		return ""
	}
	shown := problems
	if len(shown) > maxReported {
		shown = shown[:maxReported]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "lens: %s\n", count(len(problems)))
	for _, p := range shown {
		fmt.Fprintf(&b, "  %s\n", p)
	}
	if len(problems) > len(shown) {
		fmt.Fprintf(&b, "  ... and %d more\n", len(problems)-len(shown))
	}
	return strings.TrimRight(b.String(), "\n")
}

// count renders a problem count with the right plural.
func count(n int) string {
	if n == 1 {
		return "1 problem"
	}
	return fmt.Sprintf("%d problems", n)
}

// report backs the /lens command. It shows the findings as a toast, so the
// user can look without spending context: the model already saw them.
func (p *Plugin) report(_ context.Context, _ []string) (hooks.CommandResult, error) {
	p.mu.Lock()
	problems := append([]Problem(nil), p.last...)
	files := p.files
	p.mu.Unlock()

	switch {
	case files == 0:
		return hooks.CommandResult{Toast: "lens: nothing checked yet"}, nil
	case len(problems) == 0:
		return hooks.CommandResult{Toast: "lens: no problems in the file last written"}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s in the file last written:\n", count(len(problems)))
	for _, pr := range problems {
		fmt.Fprintf(&b, "  %s\n", pr)
	}
	return hooks.CommandResult{Toast: strings.TrimRight(b.String(), "\n")}, nil
}

// pathFrom pulls the target path out of tool arguments.
//
// Both edit and write name it "path". Arguments that do not parse are
// ignored: the tool already ran, so a note about its arguments helps nobody.
func pathFrom(input json.RawMessage) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return ""
	}
	return args.Path
}
