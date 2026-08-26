package media

import (
	"bytes"
	"fmt"
	"image/png"
	"slices"

	"github.com/rapatel0/alpha/internal/llm"
)

// Providers disagree about which image formats they accept, and they only
// state it in prose. A format a provider does not list is rejected at the API
// rather than degraded, so the request fails.
var (
	// mimeJPEG and mimePNG are the intersection every provider accepts.
	mimeJPEG = "image/jpeg"
	mimePNG  = "image/png"
)

// Accepts reports whether a provider documents support for a MIME type.
//
// An unknown provider gets the JPEG and PNG intersection: guessing wider is
// what produces a rejected request, and a transcode only costs a little
// quality.
func Accepts(provider, mime string) bool {
	return slices.Contains(acceptedMIME(provider), mime)
}

// acceptedMIME lists what a provider documents. Sources are recorded in
// doc/media-limits.md; add a format only with one.
func acceptedMIME(provider string) []string {
	switch provider {
	case "openai", "codex":
		// PNG, JPEG, WebP, and non-animated GIF.
		return []string{mimePNG, mimeJPEG, "image/webp", "image/gif"}
	case "gemini":
		// PNG, JPEG, WebP, HEIC, and HEIF. GIF is not listed.
		return []string{mimePNG, mimeJPEG, "image/webp", "image/heic", "image/heif"}
	case "anthropic":
		// JPEG, PNG, GIF, and WebP.
		return []string{mimePNG, mimeJPEG, "image/gif", "image/webp"}
	default:
		// xAI documents JPEG and PNG only, and an unrecognized provider is
		// safest on the same intersection.
		return []string{mimePNG, mimeJPEG}
	}
}

// ToAccepted converts an image to a format the provider documents, returning
// it unchanged when the format is already accepted.
//
// It transcodes to PNG rather than JPEG because an image that needed
// converting is usually a screenshot or a diagram, and JPEG ringing around
// text is the damage most likely to matter. An animation loses every frame but
// the first: the stdlib has no animated encoder, and one readable frame beats
// a rejected request.
func ToAccepted(img llm.Image, provider string) (llm.Image, error) {
	if img.MIME == "" || Accepts(provider, img.MIME) {
		return img, nil
	}
	decoded, err := decode(img.Data, img.MIME)
	if err != nil {
		return img, fmt.Errorf("media: cannot convert %s for %s: %w", img.MIME, provider, err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, decoded); err != nil {
		return img, fmt.Errorf("media: cannot encode %s as png: %w", img.MIME, err)
	}
	out := img
	out.Data = buf.Bytes()
	out.MIME = mimePNG
	out.Filename = replaceExt(img.Filename, ".png")
	return out, nil
}
