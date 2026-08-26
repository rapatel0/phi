package doctool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/docparse"
)

func runTool(t *testing.T, args map[string]any) (string, string, error) {
	t.Helper()
	in, err := json.Marshal(args)
	require.NoError(t, err)
	res, err := run(t.Context(), in)
	return res.Content, res.Detail, err
}

func csvFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.csv")
	require.NoError(t, os.WriteFile(path, []byte("name,total\nalice,3\n"), 0o600))
	return path
}

func TestToolReadsADocument(t *testing.T) {
	content, detail, err := runTool(t, map[string]any{"path": csvFile(t)})
	require.NoError(t, err)
	assert.Contains(t, content, "name\ttotal")
	assert.Contains(t, content, "a.csv (csv, 2 rows)", "the header must name the file and unit")
	assert.Contains(t, detail, "csv")
}

// read already handles source files. Sending a .go here would waste a turn.
func TestToolRefusesAnUnsupportedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	require.NoError(t, os.WriteFile(path, []byte("package main"), 0o600))

	_, _, err := runTool(t, map[string]any{"path": path})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use read for text files")
}

func TestToolReportsAMissingFile(t *testing.T) {
	_, _, err := runTool(t, map[string]any{"path": filepath.Join(t.TempDir(), "gone.pdf")})
	require.Error(t, err)
}

func TestToolRequiresAPath(t *testing.T) {
	_, _, err := runTool(t, map[string]any{"path": "   "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

// The model cannot otherwise tell a short document from a long one it only saw
// the start of.
func TestToolSaysWhenTheTextIsTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.csv")
	// The default byte limit is 200 KB; six bytes a row needs more than
	// 34000 rows to cross it.
	var sb strings.Builder
	for range 40000 {
		sb.WriteString("a,b,c\n")
	}
	require.NoError(t, os.WriteFile(path, []byte(sb.String()), 0o600))

	content, detail, err := runTool(t, map[string]any{"path": path})
	require.NoError(t, err)
	assert.Contains(t, content, "truncated")
	assert.Contains(t, detail, "truncated")
}

// A scanned PDF is a valid file with nothing to read. Returning an empty
// string would read as a failure.
func TestScannedHintIsReportedNotAnError(t *testing.T) {
	assert.True(t, docparse.IsProbablyScanned(docparse.Doc{Kind: "pdf", Pages: 2}))
	assert.Contains(t, docparse.ScannedHint, "OCR")
}

func TestPageWordMatchesTheFormat(t *testing.T) {
	assert.Equal(t, "sheets", pageWord(docparse.Doc{Kind: "xlsx"}))
	assert.Equal(t, "slides", pageWord(docparse.Doc{Kind: "pptx"}))
	assert.Equal(t, "rows", pageWord(docparse.Doc{Kind: "csv"}))
	assert.Equal(t, "pages", pageWord(docparse.Doc{Kind: "pdf"}))
}

func TestToolDefinitionIsUsable(t *testing.T) {
	def := DocTool().Definition
	assert.Equal(t, "read_document", def.Name)
	assert.True(t, def.Readable, "reading a document must not need write permission")
	require.NotNil(t, def.Params)
	assert.Equal(t, []string{"path"}, def.Params.Required)

	// The description must name the formats, or the model cannot tell when
	// to reach for this instead of read.
	for _, want := range []string{"PDF", "Word", "Excel"} {
		assert.Contains(t, def.Description, want)
	}
}

func TestDetailFromArgs(t *testing.T) {
	got := DocTool().DetailFromArgs(json.RawMessage(`{"path":"  /tmp/a.pdf  "}`))
	assert.Equal(t, "/tmp/a.pdf", got)
}

func TestBadArguments(t *testing.T) {
	_, err := run(t.Context(), json.RawMessage(`not json`))
	require.Error(t, err)
}
