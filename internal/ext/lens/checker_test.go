package lens

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goModule writes a minimal module so go vet has something to type-check.
func goModule(t *testing.T, body string) (root, file string) {
	t.Helper()
	root = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module probe\n\ngo 1.25\n"), 0o600))
	file = filepath.Join(root, "sample.go")
	require.NoError(t, os.WriteFile(file, []byte(body), 0o600))
	return root, file
}

// The point of the extension: a type error the model just introduced comes
// back with a line and a column it can act on.
func TestCheckReportsGoTypeErrors(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() {\n\tvar x int = \"nope\"\n\t_ = x\n}\n")

	got := Check(t.Context(), root, file, 30*time.Second)
	require.Len(t, got, 1)
	assert.Equal(t, 4, got[0].Line)
	assert.Contains(t, got[0].Msg, "cannot use")
}

// An undefined symbol is the error a single-file parse cannot find, and it is
// why the Go checker type-checks the whole package.
func TestCheckReportsUndefinedSymbols(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() { missingHelper() }\n")

	got := Check(t.Context(), root, file, 30*time.Second)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Msg, "undefined: missingHelper")
}

// Silence on a clean file is what keeps the note worth reading.
func TestCheckIsSilentOnCleanFiles(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() int { return 1 }\n")

	assert.Empty(t, Check(t.Context(), root, file, 30*time.Second))
}

// A package-wide checker reports every file in the package. Only the file just
// written may be reported, or the model is told about problems it did not
// cause and cannot see.
func TestCheckReportsOnlyTheEditedFile(t *testing.T) {
	root, file := goModule(t, "package probe\n\nfunc F() { helperA() }\n")
	other := filepath.Join(root, "other.go")
	require.NoError(t, os.WriteFile(other, []byte("package probe\n\nfunc G() { helperB() }\n"), 0o600))

	got := Check(t.Context(), root, file, 30*time.Second)
	for _, p := range got {
		assert.True(t, sameFile(root, p.File, file), "reported a file that was not edited: %s", p.File)
	}
}

// An extension with no checker for a language must do nothing rather than
// guess at a command.
func TestCheckSkipsUnknownExtensions(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "notes.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello\n"), 0o600))

	assert.Empty(t, Check(t.Context(), root, file, 30*time.Second))
}

// A checker whose config file is absent belongs to another project and must
// stay quiet.
func TestCheckerNeedsItsConfigFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "app.ts")
	require.NoError(t, os.WriteFile(file, []byte("const x: number = 'no'\n"), 0o600))

	// No tsconfig.json, so the TypeScript checker must not run.
	assert.Empty(t, Check(t.Context(), root, file, 30*time.Second))
}

// A missing binary must be a silent skip. The agent must not be told about a
// tool the project does not use.
func TestCheckerSkipsMissingBinaries(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "x.go")
	require.NoError(t, os.WriteFile(file, []byte("package p\n"), 0o600))

	c := checker{bin: "definitely-not-a-real-binary", args: []string{"$FILE"}}
	assert.Empty(t, c.run(t.Context(), root, file, time.Second))
}

// A checker that hangs must not hold up the turn.
func TestCheckerStopsAtTheTimeout(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep is not on PATH")
	}
	root := t.TempDir()
	file := filepath.Join(root, "x.go")
	require.NoError(t, os.WriteFile(file, []byte("package p\n"), 0o600))

	c := checker{bin: "sleep", args: []string{"30"}}
	start := time.Now()
	got := c.run(t.Context(), root, file, 200*time.Millisecond)
	assert.Empty(t, got)
	assert.Less(t, time.Since(start), 5*time.Second, "the timeout must cut the checker off")
}

func TestParseLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Problem
		ok   bool
	}{
		{
			name: "go vet",
			in:   "./sample.go:3:12: undefined: missingHelper",
			want: Problem{File: "./sample.go", Line: 3, Col: 12, Msg: "undefined: missingHelper"},
			ok:   true,
		},
		{
			name: "ruff",
			in:   "ok.py:1:8: F401 [*] `os` imported but unused",
			want: Problem{File: "ok.py", Line: 1, Col: 8, Msg: "F401 [*] `os` imported but unused"},
			ok:   true,
		},
		{
			name: "shellcheck gcc format",
			in:   "s.sh:2:6: warning: x is referenced but not assigned. [SC2154]",
			want: Problem{File: "s.sh", Line: 2, Col: 6, Msg: "warning: x is referenced but not assigned. [SC2154]"},
			ok:   true,
		},
		{
			// gopls prints a range; the start is the useful half.
			name: "column range keeps the start",
			in:   "a.go:77:29-35: cannot use \"nope\"",
			want: Problem{File: "a.go", Line: 77, Col: 29, Msg: "cannot use \"nope\""},
			ok:   true,
		},
		{
			name: "a message containing colons survives",
			in:   "a.go:1:1: want map[string]int: got nil",
			want: Problem{File: "a.go", Line: 1, Col: 1, Msg: "want map[string]int: got nil"},
			ok:   true,
		},
		{name: "summary line is not a problem", in: "Found 2 errors.", ok: false},
		{name: "package header is not a problem", in: "# probe", ok: false},
		{name: "a line without a column is dropped", in: "a.go:12: something", ok: false},
		{name: "a non-numeric line is dropped", in: "a.go:x:1: something", ok: false},
		{name: "an empty message is dropped", in: "a.go:1:1: ", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseLine(tt.in)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// go vet prefixes its first finding and heads each package with a comment.
// Both must be stripped, or the model sees a mangled path.
func TestParseStripsGoVetDecoration(t *testing.T) {
	out := "# probe\n# [probe]\nvet: ./sample.go:3:12: undefined: missingHelper\n"
	got := parse(out, "/root", "/root/sample.go")
	require.Len(t, got, 1)
	assert.Equal(t, "./sample.go", got[0].File)
	assert.Equal(t, "undefined: missingHelper", got[0].Msg)
}

func TestProblemString(t *testing.T) {
	assert.Equal(t, "a.go:3:12: boom", Problem{File: "a.go", Line: 3, Col: 12, Msg: "boom"}.String())
	assert.Equal(t, "a.go:3: boom", Problem{File: "a.go", Line: 3, Msg: "boom"}.String())
	assert.Equal(t, "a.go: boom", Problem{File: "a.go", Msg: "boom"}.String())
}
