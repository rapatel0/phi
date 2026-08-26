// Package doctool exposes document text extraction to the model.
package doctool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rapatel0/alpha/internal/docparse"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

const description = `Extract the text of a document: PDF, Word, Excel, PowerPoint, or CSV.

Use this instead of read for those formats. They are containers, so read
returns compressed or zipped bytes rather than text.

A scanned PDF has no text layer and returns nothing to read; the result says
so rather than failing.`

// DocTool returns the read_document tool.
func DocTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "read_document",
			Description: description,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type": "string",
						"description": "Path to the document. Supported: " +
							strings.Join(docparse.Supported(), ", "),
					},
					"max_pages": llm.Object{
						"type": "integer",
						"description": fmt.Sprintf(
							"Maximum pages or sheets to read; default %d.",
							docparse.DefaultOptions.MaxPages),
					},
				},
				Required: []string{"path"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in docInput
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Path)
		},
		Run: run,
	}
}

type docInput struct {
	Path     string `json:"path"`
	MaxPages int    `json:"max_pages,omitempty"`
}

func run(_ context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in docInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("read_document: bad arguments: %w", err)
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return tooldef.Result{}, errors.New("read_document: path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return tooldef.Result{}, fmt.Errorf("read_document: %s: %w", path, err)
	}
	if !docparse.IsSupported(path) {
		return tooldef.Result{}, fmt.Errorf(
			"read_document: %s is not a supported document; supported: %s (use read for text files)",
			path, strings.Join(docparse.Supported(), ", "))
	}

	doc, err := docparse.Parse(path, docparse.Options{MaxPages: in.MaxPages})
	if err != nil {
		return tooldef.Result{}, err
	}

	// A scanned PDF is a valid file with nothing to read. Saying so is more
	// useful than returning an empty string, which reads as a failure.
	if docparse.IsProbablyScanned(doc) {
		return tooldef.Result{
			Content: fmt.Sprintf("%s: %s", path, docparse.ScannedHint),
			Detail:  fmt.Sprintf("%s (%d pages, no text)", doc.Kind, doc.Pages),
		}, nil
	}

	return tooldef.Result{
		Content: header(doc) + doc.Text,
		Detail:  detail(doc),
	}, nil
}

// header tells the model what it is reading and whether anything is missing.
func header(d docparse.Doc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (%s", d.Path, d.Kind)
	if d.Pages > 0 {
		fmt.Fprintf(&b, ", %d %s", d.Pages, pageWord(d))
	}
	b.WriteString(")\n")
	if d.Truncated {
		// Without this the model cannot tell a short document from a
		// long one it only saw the start of.
		b.WriteString("(truncated: raise max_pages to read more)\n")
	}
	b.WriteString("\n")
	return b.String()
}

// pageWord names the unit a format counts in.
func pageWord(d docparse.Doc) string {
	switch d.Kind {
	case "xlsx":
		return "sheets"
	case "pptx":
		return "slides"
	case "csv":
		return "rows"
	default:
		return "pages"
	}
}

// detail is the one-line summary shown in the tool row.
func detail(d docparse.Doc) string {
	out := fmt.Sprintf("%s, %d chars", d.Kind, len(d.Text))
	if d.Pages > 0 {
		out = fmt.Sprintf("%s, %d %s, %d chars", d.Kind, d.Pages, pageWord(d), len(d.Text))
	}
	if d.Truncated {
		out += " (truncated)"
	}
	return out
}
