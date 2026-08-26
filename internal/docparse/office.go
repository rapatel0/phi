package docparse

import (
	"archive/zip"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// The Office formats are ZIP archives of XML, so the standard library is
// enough. Each one keeps its text in a different part, and the only real work
// is knowing which part and which element.

// maxZipEntry bounds one decompressed entry.
//
// A ZIP can claim a small compressed size and expand to gigabytes. The reader
// is pointed at whatever file the model names, so the bound is what keeps a
// hostile document from exhausting memory.
const maxZipEntry = 64 << 20

// parseDOCX reads word/document.xml.
//
// Word wraps every run of text in <w:t>, so decoding those in order rebuilds
// the document. Paragraph boundaries come from <w:p>, which is what keeps the
// result readable rather than one long line.
func parseDOCX(pathName string, opts Options) (Doc, error) {
	zr, err := openZip(pathName)
	if err != nil {
		return Doc{}, err
	}
	defer zr.Close()

	f, err := zipEntry(zr, "word/document.xml")
	if err != nil {
		return Doc{}, fmt.Errorf("docparse: %s is not a Word document: %w", pathName, err)
	}
	defer f.Close()

	lim := newLimiter(opts.MaxBytes)
	dec := xml.NewDecoder(io.LimitReader(f, maxZipEntry))
	var para strings.Builder
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Doc{}, fmt.Errorf("docparse: read %s: %w", pathName, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				var s string
				if dec.DecodeElement(&s, &t) == nil {
					para.WriteString(s)
				}
			case "tab":
				para.WriteString("\t")
			case "br":
				para.WriteString("\n")
			}
		case xml.EndElement:
			// A paragraph ends a line; a table cell ends a field.
			if t.Name.Local == "p" {
				if !lim.line(para.String()) {
					para.Reset()
					break
				}
				para.Reset()
			}
		}
		if lim.truncated {
			break
		}
	}
	if para.Len() > 0 {
		lim.line(para.String())
	}
	return Doc{
		Path:      pathName,
		Kind:      "docx",
		Text:      clean(lim.String()),
		Truncated: lim.truncated,
	}, nil
}

// parseXLSX reads the worksheets, resolving the shared string table.
//
// Excel stores most cell text once in xl/sharedStrings.xml and refers to it by
// index, so a sheet read without that table is mostly numbers.
func parseXLSX(pathName string, opts Options) (Doc, error) {
	zr, err := openZip(pathName)
	if err != nil {
		return Doc{}, err
	}
	defer zr.Close()

	shared, err := sharedStrings(zr)
	if err != nil {
		return Doc{}, fmt.Errorf("docparse: %s: %w", pathName, err)
	}

	sheets := zipEntriesUnder(zr, "xl/worksheets/", ".xml")
	if len(sheets) == 0 {
		return Doc{}, fmt.Errorf("docparse: %s is not a spreadsheet: no worksheets", pathName)
	}

	lim := newLimiter(opts.MaxBytes)
	count := 0
	for _, name := range sheets {
		if opts.MaxPages > 0 && count >= opts.MaxPages {
			lim.truncated = true
			break
		}
		count++
		if !lim.line("# " + path.Base(name)) {
			break
		}
		if err := readSheet(zr, name, shared, lim); err != nil {
			return Doc{}, fmt.Errorf("docparse: read %s: %w", pathName, err)
		}
		if lim.truncated {
			break
		}
	}
	return Doc{
		Path:      pathName,
		Kind:      "xlsx",
		Text:      clean(lim.String()),
		Pages:     count,
		Truncated: lim.truncated,
	}, nil
}

// readSheet appends one worksheet as tab-separated rows.
func readSheet(zr *zip.ReadCloser, name string, shared []string, lim *limiter) error {
	f, err := zipEntry(zr, name)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := xml.NewDecoder(io.LimitReader(f, maxZipEntry))
	var row []string
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "c" {
				continue
			}
			row = append(row, cellValue(dec, t, shared))
		case xml.EndElement:
			if t.Name.Local == "row" {
				// A row of empty cells is layout, not data.
				if strings.TrimSpace(strings.Join(row, "")) != "" {
					if !lim.line(strings.Join(row, "\t")) {
						return nil
					}
				}
				row = row[:0]
			}
		}
	}
}

// cellValue decodes one <c> element, resolving a shared-string reference.
func cellValue(dec *xml.Decoder, start xml.StartElement, shared []string) string {
	var typ string
	for _, a := range start.Attr {
		if a.Name.Local == "t" {
			typ = a.Value
		}
	}
	var cell struct {
		V string `xml:"v"`
		// An inline string keeps its text in <is><t>.
		IS struct {
			T []string `xml:"t"`
		} `xml:"is"`
	}
	if err := dec.DecodeElement(&cell, &start); err != nil {
		return ""
	}
	switch typ {
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(cell.V))
		if err != nil || i < 0 || i >= len(shared) {
			return ""
		}
		return shared[i]
	case "inlineStr":
		return strings.Join(cell.IS.T, "")
	default:
		return cell.V
	}
}

// sharedStrings reads the workbook string table, which may be absent.
func sharedStrings(zr *zip.ReadCloser) ([]string, error) {
	f, err := zipEntry(zr, "xl/sharedStrings.xml")
	if err != nil {
		// A workbook of only numbers has no table, which is not an error.
		return nil, nil //nolint:nilerr // absence is normal
	}
	defer f.Close()

	var doc struct {
		SI []struct {
			T string   `xml:"t"`
			R []string `xml:"r>t"`
		} `xml:"si"`
	}
	if err := xml.NewDecoder(io.LimitReader(f, maxZipEntry)).Decode(&doc); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(doc.SI))
	for _, si := range doc.SI {
		if len(si.R) > 0 {
			// A styled cell is split into runs that join back up.
			out = append(out, strings.Join(si.R, ""))
			continue
		}
		out = append(out, si.T)
	}
	return out, nil
}

// parsePPTX reads the slides in order.
func parsePPTX(pathName string, opts Options) (Doc, error) {
	zr, err := openZip(pathName)
	if err != nil {
		return Doc{}, err
	}
	defer zr.Close()

	slides := zipEntriesUnder(zr, "ppt/slides/", ".xml")
	if len(slides) == 0 {
		return Doc{}, fmt.Errorf("docparse: %s is not a presentation: no slides", pathName)
	}
	sortSlides(slides)

	lim := newLimiter(opts.MaxBytes)
	count := 0
	for _, name := range slides {
		if opts.MaxPages > 0 && count >= opts.MaxPages {
			lim.truncated = true
			break
		}
		count++
		if !lim.line(fmt.Sprintf("# slide %d", count)) {
			break
		}
		if err := readSlide(zr, name, lim); err != nil {
			return Doc{}, fmt.Errorf("docparse: read %s: %w", pathName, err)
		}
		if lim.truncated {
			break
		}
	}
	return Doc{
		Path:      pathName,
		Kind:      "pptx",
		Text:      clean(lim.String()),
		Pages:     count,
		Truncated: lim.truncated,
	}, nil
}

// readSlide appends one slide's text runs.
func readSlide(zr *zip.ReadCloser, name string, lim *limiter) error {
	f, err := zipEntry(zr, name)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := xml.NewDecoder(io.LimitReader(f, maxZipEntry))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		// PowerPoint uses the drawing namespace's <a:t> for text.
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "t" {
			var s string
			if dec.DecodeElement(&s, &se) == nil && strings.TrimSpace(s) != "" {
				if !lim.line(s) {
					return nil
				}
			}
		}
	}
}

// sortSlides orders slide2.xml before slide10.xml, which a plain string sort
// gets wrong and which changes the order the reader sees.
func sortSlides(names []string) {
	sort.Slice(names, func(i, j int) bool {
		a, b := slideNumber(names[i]), slideNumber(names[j])
		if a != b {
			return a < b
		}
		return names[i] < names[j]
	})
}

// slideNumber pulls the trailing number out of a slide file name.
func slideNumber(name string) int {
	base := strings.TrimSuffix(path.Base(name), ".xml")
	base = strings.TrimPrefix(base, "slide")
	n, err := strconv.Atoi(base)
	if err != nil {
		return 1 << 30
	}
	return n
}

// parseCSV reads a comma-separated file as tab-separated rows.
func parseCSV(pathName string, opts Options) (Doc, error) {
	f, err := os.Open(pathName)
	if err != nil {
		return Doc{}, fmt.Errorf("docparse: open %s: %w", pathName, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	// A spreadsheet export often has ragged rows, and refusing to read one
	// is worse than reporting what is there.
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	lim := newLimiter(opts.MaxBytes)
	rows := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Doc{}, fmt.Errorf("docparse: read %s: %w", pathName, err)
		}
		rows++
		if !lim.line(strings.Join(rec, "\t")) {
			break
		}
	}
	return Doc{
		Path:      pathName,
		Kind:      "csv",
		Text:      lim.String(),
		Pages:     rows,
		Truncated: lim.truncated,
	}, nil
}

// parsePlain reads a text file, bounded like the others.
func parsePlain(pathName string, opts Options) (Doc, error) {
	f, err := os.Open(pathName)
	if err != nil {
		return Doc{}, fmt.Errorf("docparse: open %s: %w", pathName, err)
	}
	defer f.Close()

	limit := int64(opts.MaxBytes)
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return Doc{}, fmt.Errorf("docparse: read %s: %w", pathName, err)
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	return Doc{
		Path:      pathName,
		Kind:      strings.TrimPrefix(strings.ToLower(path.Ext(pathName)), "."),
		Text:      string(data),
		Truncated: truncated,
	}, nil
}

// openZip opens a document that is a ZIP archive.
func openZip(pathName string) (*zip.ReadCloser, error) {
	zr, err := zip.OpenReader(pathName)
	if err != nil {
		return nil, fmt.Errorf("docparse: open %s: %w", pathName, err)
	}
	return zr, nil
}

// zipEntry opens one named entry.
func zipEntry(zr *zip.ReadCloser, name string) (io.ReadCloser, error) {
	for _, f := range zr.File {
		if f.Name == name {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("%s is missing", name)
}

// zipEntriesUnder lists entries in a directory with a suffix, sorted.
//
// Only direct children count: xl/worksheets/_rels holds relationship files
// that are not sheets.
func zipEntriesUnder(zr *zip.ReadCloser, dir, suffix string) []string {
	var out []string
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, dir) || !strings.HasSuffix(f.Name, suffix) {
			continue
		}
		if strings.Contains(strings.TrimPrefix(f.Name, dir), "/") {
			continue
		}
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}
