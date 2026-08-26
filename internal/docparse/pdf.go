package docparse

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ledongthuc/pdf"

	"github.com/rapatel0/alpha/internal/debuglog"
)

// PDF is the one format here that needs a dependency.
//
// The others are ZIP archives of XML. A PDF keeps its text in compressed
// content streams as glyph indices, which only mean anything through the
// font's character map. Decompressing the streams is easy and produces font
// bytes, not words; measured on a real corpus, a naive extractor returns
// unreadable output.
//
// github.com/ledongthuc/pdf was chosen by measurement over 605 documents:
// 87.3% yielded readable text, against 53.7% for rsc.io/pdf, while
// dslipak/pdf hung and gen2brain/go-fitz needs cgo and 259 MB, which would
// break the CGO_ENABLED=0 release build. It is BSD licensed, pure Go, about
// 400 KB, has no transitive dependencies, and writes nothing to stderr, which
// matters because stray output corrupts the terminal UI.

// parsePDF extracts the text layer of a PDF.
//
// A scanned document has no text layer and yields nothing. That is reported as
// an empty result rather than an error, because the file is valid and the
// caller needs to tell "no text here" from "could not read this".
func parsePDF(pathName string, opts Options) (doc Doc, err error) {
	// The parser indexes arbitrary offsets from the file and panics on some
	// malformed documents. A corrupt file the model happened to name must
	// not take the process down.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("docparse: %s is malformed: %v", pathName, r)
		}
	}()

	// The parser prints a debug line to stdout when it meets a dictionary it
	// cannot read. Stdout is the terminal UI's own screen, so that would
	// corrupt the display.
	restore := muteStdout()
	defer restore()

	f, r, err := pdf.Open(pathName)
	if err != nil {
		return Doc{}, fmt.Errorf("docparse: open %s: %w", pathName, err)
	}
	defer f.Close()

	total := r.NumPage()
	lim := newLimiter(opts.MaxBytes)
	read := 0
	for i := 1; i <= total; i++ {
		if opts.MaxPages > 0 && read >= opts.MaxPages {
			lim.truncated = true
			break
		}
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		read++
		text, perr := page.GetPlainText(nil)
		if perr != nil {
			// One unreadable page does not make the document
			// unreadable: an embedded font can fail while the rest
			// of the file is fine.
			continue
		}
		if !lim.add(text) {
			break
		}
	}

	return Doc{
		Path:      pathName,
		Kind:      "pdf",
		Text:      clean(lim.String()),
		Pages:     total,
		Truncated: lim.truncated,
	}, nil
}

// ScannedHint is returned to the caller when a PDF has no text layer, so the
// answer explains itself rather than looking like a failure.
const ScannedHint = "no text layer: this PDF is probably scanned and needs OCR"

// IsProbablyScanned reports a PDF that parsed but produced almost no text.
func IsProbablyScanned(d Doc) bool {
	return d.Kind == "pdf" && d.Pages > 0 && len(strings.TrimSpace(d.Text)) < 20
}

// muteStdout redirects os.Stdout for the duration of a call, returning the
// function that restores it.
//
// This exists for one line in the PDF parser that prints to stdout on a
// malformed dictionary. Patching the dependency is worse: the fix would be
// lost at the next upgrade.
//
// The redirect is process-wide, so parsePDF holds parseMu while it runs.
func muteStdout() func() {
	parseMu.Lock()
	saved := os.Stdout
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		// Without somewhere to send it, leaving stdout alone is better
		// than failing the read.
		return parseMu.Unlock
	}
	os.Stdout = devNull
	return func() {
		os.Stdout = saved
		if cerr := devNull.Close(); cerr != nil {
			debuglog.Logf("docparse: close devnull: %v", cerr)
		}
		parseMu.Unlock()
	}
}

// parseMu serializes PDF parsing, because muting stdout is process-wide.
var parseMu sync.Mutex
