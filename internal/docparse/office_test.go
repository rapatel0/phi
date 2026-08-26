package docparse

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildZip writes a ZIP with the given entries, which is what every Office
// format is underneath.
func buildZip(t *testing.T, name string, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	zw := zip.NewWriter(f)
	for n, body := range entries {
		w, err := zw.Create(n)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return path
}

const docxBody = `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:r><w:t>First paragraph.</w:t></w:r></w:p>
<w:p><w:r><w:t>Second </w:t></w:r><w:r><w:t>paragraph split in runs.</w:t></w:r></w:p>
</w:body></w:document>`

// Word splits a sentence across runs whenever formatting changes, so a reader
// that treats each run as a line breaks ordinary text.
func TestDOCXJoinsRunsAndSplitsParagraphs(t *testing.T) {
	path := buildZip(t, "a.docx", map[string]string{"word/document.xml": docxBody})

	doc, err := Parse(path, Options{})
	require.NoError(t, err)
	assert.Equal(t, "docx", doc.Kind)
	assert.Contains(t, doc.Text, "Second paragraph split in runs.")
	assert.Equal(t, []string{
		"First paragraph.",
		"Second paragraph split in runs.",
	}, strings.Split(doc.Text, "\n"))
}

func TestDOCXRejectsAFileThatIsNotAWordDocument(t *testing.T) {
	path := buildZip(t, "a.docx", map[string]string{"other.xml": "<x/>"})

	_, err := Parse(path, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a Word document")
}

const sharedXML = `<?xml version="1.0"?>
<sst><si><t>Name</t></si><si><t>Total</t></si><si><r><t>Split</t></r><r><t>Cell</t></r></si></sst>`

const sheetXML = `<?xml version="1.0"?>
<worksheet><sheetData>
<row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row>
<row><c><v>42</v></c><c t="s"><v>2</v></c></row>
<row><c/><c/></row>
</sheetData></worksheet>`

// Excel stores most text once and refers to it by index, so a sheet read
// without the shared table is mostly numbers.
func TestXLSXResolvesSharedStrings(t *testing.T) {
	path := buildZip(t, "a.xlsx", map[string]string{
		"xl/sharedStrings.xml":      sharedXML,
		"xl/worksheets/sheet1.xml":  sheetXML,
		"xl/worksheets/_rels/x.xml": "<ignored/>",
	})

	doc, err := Parse(path, Options{})
	require.NoError(t, err)
	assert.Contains(t, doc.Text, "Name\tTotal")
	assert.Contains(t, doc.Text, "42\tSplitCell", "a styled cell is split into runs that must rejoin")
	assert.Equal(t, 1, doc.Pages, "the _rels entry is not a worksheet")
}

// A row of empty cells is layout, not data.
func TestXLSXDropsEmptyRows(t *testing.T) {
	path := buildZip(t, "a.xlsx", map[string]string{
		"xl/sharedStrings.xml":     sharedXML,
		"xl/worksheets/sheet1.xml": sheetXML,
	})

	doc, err := Parse(path, Options{})
	require.NoError(t, err)
	for line := range strings.SplitSeq(doc.Text, "\n") {
		assert.NotEqual(t, "\t", line)
	}
}

// A workbook of only numbers has no shared table, which is not an error.
func TestXLSXWithoutASharedTable(t *testing.T) {
	path := buildZip(t, "a.xlsx", map[string]string{
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c><v>7</v></c></row></sheetData></worksheet>`,
	})

	doc, err := Parse(path, Options{})
	require.NoError(t, err)
	assert.Contains(t, doc.Text, "7")
}

func TestXLSXRejectsAFileWithNoWorksheets(t *testing.T) {
	path := buildZip(t, "a.xlsx", map[string]string{"docProps/app.xml": "<x/>"})

	_, err := Parse(path, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no worksheets")
}

func slideXML(text string) string {
	return `<?xml version="1.0"?><p:sld xmlns:a="x"><p:cSld><a:t>` + text + `</a:t></p:cSld></p:sld>`
}

// A plain string sort puts slide10 before slide2, which silently reorders the
// deck.
func TestPPTXReadsSlidesInNumericOrder(t *testing.T) {
	entries := map[string]string{}
	for _, n := range []string{"1", "2", "10"} {
		entries["ppt/slides/slide"+n+".xml"] = slideXML("content of slide " + n)
	}
	path := buildZip(t, "a.pptx", entries)

	doc, err := Parse(path, Options{})
	require.NoError(t, err)
	assert.Equal(t, 3, doc.Pages)

	one := strings.Index(doc.Text, "content of slide 1")
	two := strings.Index(doc.Text, "content of slide 2")
	ten := strings.Index(doc.Text, "content of slide 10")
	assert.Positive(t, two, "slide 2 must be present")
	assert.Less(t, one, two)
	assert.Less(t, two, ten, "slide 10 must come after slide 2")
}

func TestPPTXRejectsAFileWithNoSlides(t *testing.T) {
	path := buildZip(t, "a.pptx", map[string]string{"ppt/presentation.xml": "<x/>"})

	_, err := Parse(path, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no slides")
}

func TestSlideNumberOrdering(t *testing.T) {
	names := []string{"ppt/slides/slide10.xml", "ppt/slides/slide2.xml", "ppt/slides/slide1.xml"}
	sortSlides(names)
	assert.Equal(t, []string{
		"ppt/slides/slide1.xml",
		"ppt/slides/slide2.xml",
		"ppt/slides/slide10.xml",
	}, names)
}

// A spreadsheet export often has ragged rows; refusing to read one is worse
// than reporting what is there.
func TestCSVAcceptsRaggedRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.csv")
	require.NoError(t, os.WriteFile(path, []byte("a,b,c\n1,2\n3,4,5,6\n"), 0o600))

	doc, err := Parse(path, Options{})
	require.NoError(t, err)
	assert.Equal(t, 3, doc.Pages)
	assert.Contains(t, doc.Text, "a\tb\tc")
	assert.Contains(t, doc.Text, "3\t4\t5\t6")
}

func TestPlainTextIsReadDirectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	require.NoError(t, os.WriteFile(path, []byte("# Title\n\nbody\n"), 0o600))

	doc, err := Parse(path, Options{})
	require.NoError(t, err)
	assert.Equal(t, "md", doc.Kind)
	assert.Contains(t, doc.Text, "# Title")
}

// A long document must not fill the model's context from one file, and the
// caller must be able to tell that it was cut.
func TestByteLimitTruncatesAndSaysSo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", 5000)), 0o600))

	doc, err := Parse(path, Options{MaxBytes: 100})
	require.NoError(t, err)
	assert.Len(t, doc.Text, 100)
	assert.True(t, doc.Truncated)
}

func TestPageLimitStopsEarly(t *testing.T) {
	entries := map[string]string{}
	for _, n := range []string{"1", "2", "3", "4"} {
		entries["ppt/slides/slide"+n+".xml"] = slideXML("slide body " + n)
	}
	path := buildZip(t, "a.pptx", entries)

	doc, err := Parse(path, Options{MaxPages: 2})
	require.NoError(t, err)
	assert.Equal(t, 2, doc.Pages)
	assert.True(t, doc.Truncated)
	assert.NotContains(t, doc.Text, "slide body 3")
}

func TestUnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.bin")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

	_, err := Parse(path, Options{})
	require.Error(t, err)

	var unsupported ErrUnsupported
	assert.ErrorAs(t, err, &unsupported)
	assert.Equal(t, ".bin", unsupported.Ext)
}

func TestIsSupported(t *testing.T) {
	assert.True(t, IsSupported("a.pdf"))
	assert.True(t, IsSupported("A.DOCX"), "the extension check must ignore case")
	assert.False(t, IsSupported("a.go"))
	assert.False(t, IsSupported("noextension"))
}

// Extracted text carries the page layout: runs of spaces where columns were,
// and blank lines where margins were.
func TestCleanCollapsesLayoutWhitespace(t *testing.T) {
	got := clean("  a    b  \n\n\n\n  c  \n\n")
	assert.Equal(t, "a b\n\nc", got)
}

// A ZIP can claim a small compressed size and expand to gigabytes. The reader
// is pointed at whatever file the model names.
func TestZipEntriesAreBounded(t *testing.T) {
	assert.Positive(t, maxZipEntry)
	assert.LessOrEqual(t, maxZipEntry, 256<<20, "the bound must stay well under available memory")
}

// A spreadsheet separates columns with tabs. Folding them into spaces makes a
// two-column row indistinguishable from one cell containing a space.
func TestCleanKeepsColumnSeparators(t *testing.T) {
	assert.Equal(t, "a\tb", clean("a\tb"))
	assert.Equal(t, "a b\tc d", clean("a   b\tc    d"), "spaces collapse inside a field, tabs do not")
	assert.Equal(t, "Name\tTotal", clean("  Name  \t  Total  "))
}
