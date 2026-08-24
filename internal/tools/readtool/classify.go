package readtool

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/pulseaiclub/phi/internal/media"
	"github.com/pulseaiclub/phi/internal/tools/tooldef"
)

// classifyPrefix decides whether bytes should be shown as text, or refused
// with a pointer at a tool that can actually see them. Magic bytes win over
// extension (stolen from pi-go's read content detector).
func classifyPrefix(prefix []byte) string {
	if len(prefix) == 0 {
		return ""
	}
	if bytes.HasPrefix(prefix, []byte("%PDF-")) {
		return "pdf"
	}
	if mime := media.DetectMIME(prefix); mime != "" {
		return "image"
	}
	if strings.HasPrefix(http.DetectContentType(prefix), "image/") {
		return "image"
	}
	if bytes.IndexByte(prefix, 0) >= 0 {
		return "binary"
	}
	return ""
}

func refuseNonText(kind, display string, prefix []byte, size int64) tooldef.Result {
	var note string
	switch kind {
	case "image":
		mime := media.DetectMIME(prefix)
		if mime == "" {
			mime = http.DetectContentType(prefix)
		}
		note = fmt.Sprintf(
			"%s is an image (%s, %d bytes). Use the read_image tool to actually see it — this tool would only return bytes you cannot read.",
			display,
			mime,
			size,
		)
	case "pdf":
		note = fmt.Sprintf(
			"%s is a PDF (%d bytes), so it has no text lines to number. Extract text first: `pdftotext %q -` via bash.",
			display, size, display,
		)
	default:
		note = fmt.Sprintf(
			"%s is a binary file (%s, %d bytes) and has no text to show. Inspect it with file/xxd/strings via bash.",
			display, http.DetectContentType(prefix), size,
		)
	}
	return tooldef.Result{Content: note, Detail: display, Output: note}
}
