package docparse

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildPDF writes a complete one-page PDF with a text layer.
//
// It is generated rather than embedded because a PDF carries byte offsets in
// its cross-reference table: a literal would break the moment anyone reflowed
// it, and the parser would reject it for a reason that looks unrelated.
func buildPDF(t *testing.T, text string) string {
	t.Helper()

	stream := "BT /F1 24 Tf 72 700 Td (" + text + ") Tj ET\n"
	objects := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Kids[3 0 R]/Count 1>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R" +
			"/Resources<</Font<</F1 5 0 R>>>>>>",
		"", // the content stream, written separately
		"<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>",
	}

	var buf strings.Builder
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		if body == "" {
			fmt.Fprintf(&buf, "%d 0 obj<</Length %d>>stream\n%sendstream\nendobj\n", i+1, len(stream), stream)
			continue
		}
		fmt.Fprintf(&buf, "%d 0 obj%sendobj\n", i+1, body)
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)

	return writeFile(t, "a.pdf", buf.String())
}

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// The reason this package carries a dependency: a PDF keeps its text as glyph
// indices in compressed streams, and decompressing them by hand yields font
// bytes rather than words.
func TestPDFExtractsItsTextLayer(t *testing.T) {
	doc, err := Parse(buildPDF(t, "Hello from a real PDF"), Options{})
	require.NoError(t, err)
	assert.Equal(t, "pdf", doc.Kind)
	assert.Equal(t, 1, doc.Pages)
	assert.Contains(t, doc.Text, "Hello from a real PDF")
	assert.False(t, IsProbablyScanned(doc))
}

// A file that is not a PDF must fail with a readable message rather than
// returning empty text, which reads as "this document is blank".
func TestPDFRejectsANonPDF(t *testing.T) {
	_, err := Parse(writeFile(t, "a.pdf", "this is not a pdf at all"), Options{})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "pdf")
}

// The parser indexes arbitrary offsets and panics on some malformed files: a
// fuzz sweep of small mutations panicked on roughly one file in five. A
// corrupt document the model happened to name must not take the process down.
func TestPDFSurvivesACorruptFile(t *testing.T) {
	good, err := os.ReadFile(buildPDF(t, "text"))
	require.NoError(t, err)

	// Deterministic mutations, each of which panicked the parser before the
	// recover was added: a damaged cross-reference table and a broken
	// dictionary are what a truncated download actually looks like.
	rng := rand.New(rand.NewSource(7))
	dir := t.TempDir()
	for i := range 60 {
		body := append([]byte(nil), good...)
		for range 1 + rng.Intn(6) {
			body[rng.Intn(len(body))] = byte(rng.Intn(256))
		}
		path := filepath.Join(dir, fmt.Sprintf("corrupt%d.pdf", i))
		require.NoError(t, os.WriteFile(path, body, 0o600))

		// The assertion is that this returns rather than panicking.
		doc, err := Parse(path, Options{})
		if err != nil {
			assert.Contains(t, err.Error(), "docparse", "an error must name its source")
			continue
		}
		assert.Equal(t, "pdf", doc.Kind)
	}
}

// A truncated file is the common corruption, and it must be an error rather
// than a panic.
func TestPDFSurvivesTruncation(t *testing.T) {
	good, err := os.ReadFile(buildPDF(t, "text"))
	require.NoError(t, err)

	for _, frac := range []int{2, 3, 4, 8} {
		path := writeFile(t, "cut.pdf", string(good[:len(good)/frac]))
		_, err := Parse(path, Options{})
		require.Error(t, err, "a file cut to 1/%d must fail cleanly", frac)
		assert.Contains(t, err.Error(), "docparse")
	}
}

func TestPDFRespectsThePageLimit(t *testing.T) {
	doc, err := Parse(buildPDF(t, "Hello from a real PDF"), Options{MaxPages: 1})
	require.NoError(t, err)
	assert.Equal(t, 1, doc.Pages)
}

func TestPDFRespectsTheByteLimit(t *testing.T) {
	doc, err := Parse(buildPDF(t, "Hello from a real PDF"), Options{MaxBytes: 10})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(doc.Text), 10) //nolint:testifylint // a bound, not an exact length
	assert.True(t, doc.Truncated)
}

// A scanned PDF is a valid file with nothing to read. The caller has to tell
// that apart from a failure, or it reports the wrong thing to the user.
func TestIsProbablyScanned(t *testing.T) {
	assert.True(t, IsProbablyScanned(Doc{Kind: "pdf", Pages: 3, Text: ""}),
		"pages but no text is the shape of a scan")
	assert.True(t, IsProbablyScanned(Doc{Kind: "pdf", Pages: 1, Text: "  \n "}))

	assert.False(t, IsProbablyScanned(Doc{Kind: "pdf", Pages: 1, Text: strings.Repeat("a", 50)}))
	assert.False(t, IsProbablyScanned(Doc{Kind: "docx", Text: ""}),
		"only a PDF can be scanned; an empty docx is just empty")
	assert.False(t, IsProbablyScanned(Doc{Kind: "pdf", Pages: 0, Text: ""}),
		"a file that never parsed is not a scan")
}

func TestScannedHintNamesTheRemedy(t *testing.T) {
	assert.Contains(t, ScannedHint, "OCR")
}

func TestMissingFile(t *testing.T) {
	_, err := Parse(filepath.Join(t.TempDir(), "absent.pdf"), Options{})
	require.Error(t, err)
}

// The defaults have to be large enough for an ordinary report and small enough
// not to fill a context window from one file.
func TestDefaultOptionsAreSane(t *testing.T) {
	assert.Positive(t, DefaultOptions.MaxPages)
	assert.Positive(t, DefaultOptions.MaxBytes)
	assert.LessOrEqual(t, DefaultOptions.MaxBytes, 1<<20)
}

// A zero Options must get the defaults rather than reading nothing.
func TestZeroOptionsUseTheDefaults(t *testing.T) {
	path := writeFile(t, "a.txt", strings.Repeat("x", 300<<10))

	doc, err := Parse(path, Options{})
	require.NoError(t, err)
	assert.Equal(t, DefaultOptions.MaxBytes, len(doc.Text))
	assert.True(t, doc.Truncated)
}

// The PDF parser prints a debug line to stdout when it meets a dictionary it
// cannot read, and stdout is the terminal UI's own screen.
//
// The mutation is the one a fuzz sweep found to reach that path: an arbitrary
// corruption usually fails earlier and prints nothing, so a test that picks
// one at random passes whether or not stdout is muted.
func TestParsingDoesNotWriteToStdout(t *testing.T) {
	good, err := os.ReadFile(buildPDF(t, "text"))
	require.NoError(t, err)

	// Seed 51 was found by sweeping: it corrupts a dictionary in the way
	// that reaches the parser's debug print.
	rng := rand.New(rand.NewSource(51))
	body := append([]byte(nil), good...)
	for range 1 + rng.Intn(6) {
		body[rng.Intn(len(body))] = byte(rng.Intn(256))
	}
	path := writeFile(t, "noisy.pdf", string(body))

	r, w, err := os.Pipe()
	require.NoError(t, err)
	saved := os.Stdout
	os.Stdout = w

	_, _ = Parse(path, Options{})

	os.Stdout = saved
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	assert.Empty(t, string(out), "the parser must not write to the terminal")
}

// muteStdout must restore stdout and release its lock on every path, or the
// next read deadlocks.
func TestMuteStdoutIsBalanced(t *testing.T) {
	before := os.Stdout
	for range 20 {
		restore := muteStdout()
		assert.NotEqual(t, before, os.Stdout, "stdout must be muted")
		restore()
		assert.Equal(t, before, os.Stdout, "stdout must be restored")
	}

	// A leaked lock deadlocks here rather than failing.
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			restore := muteStdout()
			restore()
		})
	}
	wg.Wait()
}
