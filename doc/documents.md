# Documents

`read_document` extracts the text of a PDF, Word document, spreadsheet,
presentation, or CSV.

`read` returns the bytes of a file, which works for source code. These formats
are containers: an Office document is zipped XML and a PDF is compressed
streams, so `read` returns neither text nor a useful error.

## Formats

| Extension | Reader | Dependency |
| --- | --- | --- |
| `.pdf` | text layer | `github.com/ledongthuc/pdf` |
| `.docx` | `word/document.xml` | none |
| `.xlsx` | worksheets plus the shared string table | none |
| `.pptx` | `ppt/slides/*.xml` in numeric order | none |
| `.csv` | `encoding/csv`, as tab-separated rows | none |
| `.txt`, `.md` | read directly | none |

The Office formats are ZIP archives of XML, so they need only the standard
library.

## Why PDF needs a dependency

A PDF stores text as glyph indices in compressed content streams. The indices
only mean anything through the font's character map, so decompressing the
streams yields font bytes rather than words. A prototype using only
`compress/zlib` produced unreadable output on real files.

`github.com/ledongthuc/pdf` was chosen by measuring 605 real documents:

| Library | Readable text | Notes |
| --- | --- | --- |
| `ledongthuc/pdf` | 87.3% | BSD, pure Go, ~400 KB, no transitive dependencies |
| `rsc.io/pdf` | 53.7% | writes parser noise to stderr |
| `dslipak/pdf` | did not finish | hung on the corpus |
| `gen2brain/go-fitz` | not tested | cgo and 259 MB; breaks `CGO_ENABLED=0` |

Two behaviors of that library are contained in `internal/docparse`:

- It panics on some malformed files. A fuzz sweep of small mutations hit this
  on roughly one file in five, so parsing recovers and reports an error.
- It prints a debug line to stdout when it meets a dictionary it cannot read.
  Stdout is the terminal UI's own screen, so stdout is muted for the call. That
  redirect is process-wide, so PDF parsing is serialized.

## Scanned PDFs

A scanned page is an image. It has no text layer, and there is no OCR here, so
nothing can be extracted. That is reported rather than returned as an empty
string, which would read as a failure:

```text
report.pdf: no text layer: this PDF is probably scanned and needs OCR
```

Roughly one in ten PDFs in a typical downloads folder is of this kind. For
those, use the `pi-docparser` MCP server, which does OCR.

## Limits

Reading stops at 50 pages or 200 KB of text, whichever comes first, so one long
document cannot fill the context window. A truncated result says so, because
the model cannot otherwise tell a short document from the start of a long one.

Raise the page limit per call with `max_pages`.
