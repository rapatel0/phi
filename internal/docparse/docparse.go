// Package docparse extracts plain text from documents.
//
// The agent can already read source files, but a PDF, a Word document, or a
// spreadsheet is a container: reading the bytes gives compressed streams or
// zipped XML, not text. This turns those into something a model can read.
//
// Office formats are ZIP archives of XML, so they need only the standard
// library. PDF needs a parser, which is why the package carries one
// dependency.
package docparse

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Doc is the extracted text of one document.
type Doc struct {
	// Path is the file the text came from.
	Path string
	// Kind names the format, for a caller that wants to say so.
	Kind string
	// Text is the extracted content.
	Text string
	// Pages counts pages or sheets, and is 0 for formats without them.
	Pages int
	// Truncated reports that Text stops short of the whole document.
	Truncated bool
}

// ErrUnsupported reports a file this package cannot read.
type ErrUnsupported struct{ Ext string }

func (e ErrUnsupported) Error() string {
	return fmt.Sprintf("docparse: %s is not a supported document", e.Ext)
}

// Options bounds an extraction.
type Options struct {
	// MaxPages stops after this many pages or sheets. Zero means every
	// page. A long document otherwise fills the model's context with one
	// file.
	MaxPages int
	// MaxBytes stops after this much text. Zero means no limit.
	MaxBytes int
}

// DefaultOptions are what a caller gets for the zero Options.
//
// 50 pages and 200 KB are both well inside a modern context window while still
// covering an ordinary report or contract in full.
var DefaultOptions = Options{MaxPages: 50, MaxBytes: 200 << 10}

// Supported lists the extensions this package can read.
func Supported() []string {
	return []string{".pdf", ".docx", ".xlsx", ".pptx", ".csv", ".txt", ".md"}
}

// IsSupported reports whether a path names a document this package can read.
func IsSupported(path string) bool {
	return slices.Contains(Supported(), strings.ToLower(filepath.Ext(path)))
}

// Parse extracts text from a document, choosing the reader by extension.
//
// The extension is trusted rather than sniffed: a file named .docx that is not
// a ZIP fails with a clear error from the reader, which is more useful than
// silently treating it as something else.
func Parse(path string, opts Options) (Doc, error) {
	if opts.MaxPages == 0 {
		opts.MaxPages = DefaultOptions.MaxPages
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = DefaultOptions.MaxBytes
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pdf":
		return parsePDF(path, opts)
	case ".docx":
		return parseDOCX(path, opts)
	case ".xlsx":
		return parseXLSX(path, opts)
	case ".pptx":
		return parsePPTX(path, opts)
	case ".csv":
		return parseCSV(path, opts)
	case ".txt", ".md":
		return parsePlain(path, opts)
	default:
		return Doc{}, ErrUnsupported{Ext: ext}
	}
}

// limiter accumulates text and reports when it has had enough.
//
// Every reader shares it so the byte limit means the same thing in all of
// them, and so a caller can tell a truncated result from a short document.
type limiter struct {
	sb        strings.Builder
	max       int
	truncated bool
}

func newLimiter(max int) *limiter { return &limiter{max: max} }

// add appends text, returning false once the limit is reached.
func (l *limiter) add(s string) bool {
	if l.truncated {
		return false
	}
	if l.max > 0 && l.sb.Len()+len(s) > l.max {
		room := l.max - l.sb.Len()
		if room > 0 {
			l.sb.WriteString(s[:room])
		}
		l.truncated = true
		return false
	}
	l.sb.WriteString(s)
	return true
}

// line appends text and a newline.
func (l *limiter) line(s string) bool { return l.add(s + "\n") }

func (l *limiter) String() string { return l.sb.String() }

// clean collapses the whitespace an extractor leaves behind.
//
// Extracted text carries the layout of the page: runs of spaces where columns
// were, and blank lines where margins were. Collapsing them costs nothing a
// reader needs and saves a lot of context.
//
// Tabs survive. A spreadsheet uses them to separate columns, and folding them
// into spaces would make "Name Total" indistinguishable from a cell whose text
// contains a space.
func clean(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t\r")
		if strings.TrimSpace(ln) == "" {
			blank++
			// One blank line separates paragraphs; more is layout.
			if blank > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, collapseSpaces(ln))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// collapseSpaces squeezes runs of spaces in each tab-separated field, leaving
// the tabs themselves in place.
func collapseSpaces(line string) string {
	fields := strings.Split(line, "\t")
	for i, f := range fields {
		fields[i] = strings.Join(strings.Fields(f), " ")
	}
	return strings.Join(fields, "\t")
}
