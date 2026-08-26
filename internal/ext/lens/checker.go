// Package lens reports code problems to the model right after it edits a
// file, so a mistake is corrected in the same turn rather than at the next
// build.
//
// It is a small analog of the pi-lens plugin. That one embeds LSP clients,
// tree-sitter grammars, and ast-grep in about 26 MB of JavaScript. This
// shells out to whatever checker the project already uses, which keeps the
// binary lean and means the agent sees the same findings a human would.
package lens

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Problem is one finding from a checker.
type Problem struct {
	File string
	Line int
	Col  int
	Msg  string
}

// String renders a problem the way every checker prints it, so the model sees
// a familiar form it can act on.
func (p Problem) String() string {
	loc := p.File
	if p.Line > 0 {
		loc += ":" + strconv.Itoa(p.Line)
		if p.Col > 0 {
			loc += ":" + strconv.Itoa(p.Col)
		}
	}
	return loc + ": " + p.Msg
}

// checker is one command that reports problems for a kind of file.
type checker struct {
	// bin is looked up on PATH. A missing binary skips the checker: the
	// agent must not be told about a tool the project does not use.
	bin string
	// args build the command line. $FILE and $PKG are replaced.
	args []string
	// needs names a file that must exist in the project root, so a
	// checker configured for another project stays quiet. Empty means the
	// checker always applies.
	needs string
}

// checkersFor returns the checkers for a file extension.
//
// The table is deliberately fixed rather than configurable. A config file
// would be one more surface to document and version, and the mapping from a
// language to its usual checker is not controversial.
func checkersFor(ext string) []checker {
	switch ext {
	case ".go":
		// go vet type-checks the whole package, so it catches undefined
		// symbols and signature drift that a single-file parse cannot.
		// gopls check reports the same type errors but takes about 1.6 s
		// against 0.18 s, which is too slow to run after every edit.
		return []checker{{bin: "go", args: []string{"vet", "$PKG"}}}
	case ".py":
		return []checker{{bin: "ruff", args: []string{"check", "--output-format=concise", "$FILE"}}}
	case ".sh", ".bash":
		return []checker{{bin: "shellcheck", args: []string{"-f", "gcc", "$FILE"}}}
	case ".rs":
		return []checker{{bin: "cargo", args: []string{"check", "--quiet"}, needs: "Cargo.toml"}}
	case ".ts", ".tsx":
		return []checker{{bin: "tsc", args: []string{"--noEmit"}, needs: "tsconfig.json"}}
	default:
		return nil
	}
}

// Check runs the checkers for one file and returns what they report.
//
// A checker that is missing, misconfigured, or slow yields nothing rather
// than an error. This runs after an edit that already succeeded, so a broken
// checker must not look like a broken edit.
func Check(ctx context.Context, root, file string, timeout time.Duration) []Problem {
	cs := checkersFor(strings.ToLower(filepath.Ext(file)))
	if len(cs) == 0 {
		return nil
	}
	var out []Problem
	for _, c := range cs {
		out = append(out, c.run(ctx, root, file, timeout)...)
	}
	return out
}

// run executes one checker and parses its output.
func (c checker) run(ctx context.Context, root, file string, timeout time.Duration) []Problem {
	if c.needs != "" && !fileExists(filepath.Join(root, c.needs)) {
		return nil
	}
	bin, err := exec.LookPath(c.bin)
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := make([]string, len(c.args))
	for i, a := range c.args {
		switch a {
		case "$FILE":
			a = file
		case "$PKG":
			a = "./" + filepath.ToSlash(relDir(root, file))
		}
		args[i] = a
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = root
	// Checkers report findings on both streams and exit non-zero when they
	// find something, so the error is not interesting; the output is.
	combined, _ := cmd.CombinedOutput()
	return parse(string(combined), root, file)
}

// parse pulls problems out of checker output.
//
// Every checker here prints file:line:col: message, so one parser covers all
// of them. Anything that does not match that shape is dropped rather than
// forwarded: a summary line or a progress message is noise to the model.
func parse(out, root, want string) []Problem {
	var problems []Problem
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		// go vet prefixes the first finding with "vet: " and heads each
		// package with "# name".
		line = strings.TrimPrefix(line, "vet: ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, ok := parseLine(line)
		if !ok {
			continue
		}
		// A package-wide checker reports the whole package. Keep only the
		// file just edited, or the model is told about problems it did not
		// cause and cannot see.
		if !sameFile(root, p.File, want) {
			continue
		}
		problems = append(problems, p)
	}
	return problems
}

// parseLine splits one file:line:col: message line.
func parseLine(line string) (Problem, bool) {
	// A Windows path starts with a drive letter, and a message can contain
	// colons, so split from the left exactly as far as needed.
	parts := strings.SplitN(line, ":", 4)
	if len(parts) < 4 {
		return Problem{}, false
	}
	file, lineNo, col, msg := parts[0], parts[1], parts[2], parts[3]
	n, err := strconv.Atoi(lineNo)
	if err != nil || n <= 0 {
		return Problem{}, false
	}
	// gopls prints a range as line:startCol-endCol; keep the start.
	if i := strings.IndexByte(col, '-'); i >= 0 {
		col = col[:i]
	}
	c, err := strconv.Atoi(col)
	if err != nil {
		return Problem{}, false
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return Problem{}, false
	}
	return Problem{File: file, Line: n, Col: c, Msg: msg}, true
}
