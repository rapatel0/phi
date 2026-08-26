package media

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/rapatel0/alpha/internal/llm"
)

const (
	maxInBytes = 25 << 20 // refuse to slurp more than this from disk/clipboard

	// maxOutBytes is a raw budget, but every provider states its per-image
	// limit on the base64 payload, which is 4/3 the size. The smallest such
	// limit is 5 MB, on Bedrock and Vertex. 4 MiB raw encodes to 5.6 MB and
	// would breach it, so the budget is set from the encoded limit instead.
	maxOutBytes = 3 << 20 // 3 MiB raw is about 4.2 MB base64

	// maxDim bounds the longest side. Anthropic downscales server-side to
	// 1568 px (2576 px on newer models) and OpenAI tiles from 2048 px, so
	// sending more than this buys nothing and costs upload time. It stays
	// above those tiers rather than matching one, because a provider that
	// does not resize still gets a usable image.
	maxDim = 2048

	// A crop smaller than this is enlarged so thin strokes stay legible.
	minZoomSide   = 768
	maxZoomFactor = 8.0
	maxPending    = 8
)

// ErrEmptyClipboard means the clipboard had no image.
var ErrEmptyClipboard = fmt.Errorf("media: clipboard has no image")

// ErrTooLarge is returned when an image cannot be compressed under maxOutBytes.
var ErrTooLarge = fmt.Errorf("media: image too large")

// DetectMIME sniffs PNG/JPEG/GIF/WebP from magic bytes.
func DetectMIME(data []byte) string {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	if len(data) >= 6 && bytes.Equal(data[:6], []byte{'G', 'I', 'F', '8', '7', 'a'}) ||
		len(data) >= 6 && bytes.Equal(data[:6], []byte{'G', 'I', 'F', '8', '9', 'a'}) {
		return "image/gif"
	}
	if len(data) >= 12 && bytes.Equal(data[:4], []byte{'R', 'I', 'F', 'F'}) &&
		bytes.Equal(data[8:12], []byte{'W', 'E', 'B', 'P'}) {
		return "image/webp"
	}
	return ""
}

// LoadFile reads path, sniffs, and compresses.
func LoadFile(path string) (llm.Image, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return llm.Image{}, fmt.Errorf("media: empty path")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return llm.Image{}, err
	}
	if fi.IsDir() {
		return llm.Image{}, fmt.Errorf("media: %s is a directory", path)
	}
	if fi.Size() > maxInBytes {
		return llm.Image{}, fmt.Errorf("%w: file is %d bytes", ErrTooLarge, fi.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return llm.Image{}, err
	}
	return Normalize(llm.Image{Data: data, Filename: filepath.Base(path)})
}

// Normalize sniffs MIME, downscales, and recompresses so the payload fits
// provider limits. WebP over the output cap is rejected (no stdlib encoder).
func Normalize(img llm.Image) (llm.Image, error) {
	if len(img.Data) == 0 {
		return llm.Image{}, fmt.Errorf("media: empty image")
	}
	if len(img.Data) > maxInBytes {
		return llm.Image{}, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(img.Data))
	}
	mime := DetectMIME(img.Data)
	if mime == "" {
		if img.MIME != "" {
			mime = img.MIME
		} else {
			return llm.Image{}, fmt.Errorf("media: not a PNG, JPEG, GIF, or WebP")
		}
	}
	img.MIME = mime
	if img.Filename == "" {
		img.Filename = defaultName(mime)
	}
	if mime == "image/webp" {
		if len(img.Data) > maxOutBytes {
			return llm.Image{}, fmt.Errorf("%w: webp is %d bytes (max %d)", ErrTooLarge, len(img.Data), maxOutBytes)
		}
		return img, nil
	}
	// Fast path: an image already inside every limit is sent byte for byte.
	// Re-encoding it would only lose information. A GIF is the clearest case:
	// the stdlib has no animated encoder, so a round trip through the budget
	// below silently flattens it to a single JPEG frame and can inflate a
	// 4 KB file into something larger.
	if w, h := PixelSize(img.Data); withinLimits(len(img.Data), w, h) {
		return img, nil
	}
	decoded, err := decode(img.Data, mime)
	if err != nil {
		// Unreadable but sniffed: pass through if already small.
		if len(img.Data) <= maxOutBytes {
			return img, nil
		}
		return llm.Image{}, err
	}
	decoded = fit(decoded, maxDim)
	out, outMIME, err := encodeBudget(decoded, mime)
	if err != nil {
		return llm.Image{}, err
	}
	img.Data = out
	img.MIME = outMIME
	if ext := extFor(outMIME); ext != "" {
		img.Filename = replaceExt(img.Filename, ext)
	}
	return img, nil
}

func defaultName(mime string) string {
	switch mime {
	case "image/jpeg":
		return "clipboard.jpg"
	case "image/gif":
		return "clipboard.gif"
	case "image/webp":
		return "clipboard.webp"
	default:
		return "clipboard.png"
	}
}

func extFor(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	default:
		return ""
	}
}

func replaceExt(name, ext string) string {
	if name == "" {
		return "image" + ext
	}
	cur := filepath.Ext(name)
	if cur == "" {
		return name + ext
	}
	return strings.TrimSuffix(name, cur) + ext
}

// PixelSize returns PNG/JPEG/GIF dimensions, or 0,0 if unknown (e.g. WebP).
func PixelSize(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func decode(data []byte, mime string) (image.Image, error) {
	r := bytes.NewReader(data)
	switch mime {
	case "image/png":
		return png.Decode(r)
	case "image/jpeg":
		return jpeg.Decode(r)
	case "image/gif":
		return gif.Decode(r)
	default:
		img, _, err := image.Decode(r)
		return img, err
	}
}

func encodeBudget(src image.Image, origMIME string) ([]byte, string, error) {
	if origMIME == "image/png" {
		var buf bytes.Buffer
		if err := png.Encode(&buf, src); err == nil && buf.Len() <= maxOutBytes {
			return buf.Bytes(), "image/png", nil
		}
	}
	for _, q := range []int{85, 70, 55, 40} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: q}); err != nil {
			return nil, "", err
		}
		if buf.Len() <= maxOutBytes {
			return buf.Bytes(), "image/jpeg", nil
		}
	}
	return nil, "", fmt.Errorf("%w: could not compress under %d bytes", ErrTooLarge, maxOutBytes)
}

// LooksLikeImagePath reports a single-line paste that is probably a file path
// to an image (used to decide whether to attach instead of inserting text).
func LooksLikeImagePath(s string) bool {
	return len(ImagePaths(s)) > 0
}

// ImagePaths extracts existing image files from pasted text (drag-drop, quoted
// paths, file:// URLs). Non-image lines abort the whole parse so mixed prose
// is not treated as an attach.
func ImagePaths(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'")
	if s == "" || strings.Contains(s, "\x00") {
		return nil
	}
	if !utf8.ValidString(s) {
		return nil
	}
	lines := splitPasteLines(s)
	if len(lines) == 0 || len(lines) > maxPending {
		return nil
	}
	var out []string
	for _, line := range lines {
		p := normalizePastePath(line)
		if p == "" {
			return nil
		}
		if !hasImageExt(p) && DetectMIME(peekFile(p, 16)) == "" {
			return nil
		}
		if _, err := os.Stat(p); err != nil {
			return nil
		}
		if DetectMIME(peekFile(p, 16)) == "" && !hasImageExt(p) {
			return nil
		}
		out = append(out, p)
	}
	return out
}

func splitPasteLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	raw := strings.Split(s, "\n")
	var lines []string
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func normalizePastePath(line string) string {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "`\"'")
	if strings.HasPrefix(strings.ToLower(line), "file://") {
		line = line[7:]
		if strings.HasPrefix(line, "localhost") {
			line = strings.TrimPrefix(line, "localhost")
		}
	}
	if strings.HasPrefix(line, "~"+string(os.PathSeparator)) || line == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		if line == "~" {
			line = home
		} else {
			line = filepath.Join(home, line[2:])
		}
	}
	if !filepath.IsAbs(line) {
		if abs, err := filepath.Abs(line); err == nil {
			line = abs
		}
	}
	return line
}

func hasImageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func peekFile(path string, n int) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, n)
	nr, _ := f.Read(buf)
	return buf[:nr]
}
