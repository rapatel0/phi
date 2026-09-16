package diffreview

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// LabelForSpec is the overlay title suffix for a /diff argument list.
func LabelForSpec(spec []string) string {
	if len(spec) == 0 {
		return "working tree"
	}
	switch strings.ToLower(spec[0]) {
	case "staged", "--staged", "--cached":
		return "staged"
	case "head":
		return "HEAD"
	default:
		return strings.Join(spec, " ")
	}
}

// EmptyNote explains an empty diff for spec. Untracked files never appear in
// `git diff` output, which is the usual reason a reviewer sees nothing.
func EmptyNote(spec []string) string {
	if len(spec) == 0 {
		return "No unstaged changes. Untracked files are not shown — run git status."
	}
	switch strings.ToLower(spec[0]) {
	case "staged", "--staged", "--cached":
		return "No staged changes. Index matches HEAD; untracked files are not shown."
	case "head":
		return "HEAD has no changes to show."
	default:
		return fmt.Sprintf("No changes vs %s. Untracked files are not shown.", LabelForSpec(spec))
	}
}

// GitArgv is the git command that produces a unified diff for spec.
func GitArgv(spec []string) []string {
	argv := []string{"git", "-c", "color.ui=never"}
	if len(spec) == 0 {
		return append(argv, "diff")
	}
	switch strings.ToLower(spec[0]) {
	case "staged", "--staged", "--cached":
		return append(argv, append([]string{"diff", "--staged"}, spec[1:]...)...)
	case "head":
		return append(argv, "show", "--pretty=medium", "-p", "HEAD")
	case "--":
		if len(spec) == 1 {
			return append(argv, "diff")
		}
		return spec[1:]
	default:
		return append(argv, append([]string{"diff"}, spec...)...)
	}
}

// LoadGit runs git in cwd and returns unified-diff text.
func LoadGit(ctx context.Context, cwd string, spec []string) (string, error) {
	argv := GitArgv(spec)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // G204: git from GitArgv, or /diff -- argv
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil || looksLikeDiff(text) {
		return text, nil
	}
	return "", gitError(argv, text, err)
}

// gitError renders a git failure as one line the status bar can show, and names
// the next step for the failures users actually hit.
func gitError(argv []string, text string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	msg := gitMessage(text, err)
	return fmt.Errorf("%s: %s%s", strings.Join(argv, " "), msg, gitHint(msg))
}

// gitMessage reduces git's stderr to something a status line can show. Failures
// like "not a git repository" come with a full usage dump attached.
func gitMessage(text string, err error) string {
	raw := text
	if strings.TrimSpace(raw) == "" {
		raw = err.Error()
	}
	if i := strings.Index(raw, "\nusage:"); i >= 0 {
		raw = raw[:i]
	}
	if msg := oneLine(raw); msg != "" {
		return truncate(msg, 160)
	}
	return truncate(oneLine(err.Error()), 160)
}

func gitHint(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "not a git repository"):
		return " — open /diff inside the repo"
	case strings.Contains(lower, "unknown revision"), strings.Contains(lower, "bad object"),
		strings.Contains(lower, "ambiguous argument"):
		return " — fetch first, or check the revision name"
	case strings.Contains(lower, "does not have any commits"):
		return " — commit something first"
	}
	return ""
}

// oneLine flattens git's multi-line stderr into a single status line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

func looksLikeDiff(text string) bool {
	return strings.Contains(text, "\ndiff --git ") || strings.HasPrefix(text, "diff --git ") ||
		strings.HasPrefix(text, "commit ")
}
