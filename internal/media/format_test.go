package media

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
)

func gifImage(t *testing.T, dim int) llm.Image {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, dim, dim), []color.Color{color.Black, color.White})
	for y := range dim {
		for x := range dim {
			img.SetColorIndex(x, y, uint8((x/8+y/8)%2))
		}
	}
	var buf bytes.Buffer
	require.NoError(t, gif.Encode(&buf, img, nil))
	return llm.Image{Data: buf.Bytes(), MIME: "image/gif", Filename: "a.gif"}
}

// xAI documents JPEG and PNG only, so a GIF must be converted rather than sent
// and rejected. This is the case that motivated the whole check.
func TestAcceptsXAIRejectsGIFAndWebP(t *testing.T) {
	assert.False(t, Accepts("xai", "image/gif"))
	assert.False(t, Accepts("xai", "image/webp"))
	assert.True(t, Accepts("xai", "image/png"))
	assert.True(t, Accepts("xai", "image/jpeg"))
}

// Gemini documents WebP but not GIF.
func TestAcceptsGeminiExcludesGIF(t *testing.T) {
	assert.False(t, Accepts("gemini", "image/gif"))
	assert.True(t, Accepts("gemini", "image/webp"))
	assert.True(t, Accepts("gemini", "image/heic"))
}

func TestAcceptsOpenAIAndAnthropicAllowGIF(t *testing.T) {
	for _, p := range []string{"openai", "codex", "anthropic"} {
		assert.True(t, Accepts(p, "image/gif"), "%s documents gif", p)
		assert.True(t, Accepts(p, "image/webp"), "%s documents webp", p)
	}
}

// An unrecognized provider must get the narrow intersection: guessing wider is
// what produces a rejected request.
func TestAcceptsUnknownProviderIsNarrow(t *testing.T) {
	assert.True(t, Accepts("", "image/png"))
	assert.True(t, Accepts("", "image/jpeg"))
	assert.False(t, Accepts("", "image/gif"))
	assert.False(t, Accepts("some-new-provider", "image/webp"))
}

// Every provider must accept the intersection, or conversion has no target.
func TestEveryProviderAcceptsTheIntersection(t *testing.T) {
	for _, p := range []string{"anthropic", "openai", "codex", "gemini", "xai", ""} {
		assert.True(t, Accepts(p, "image/png"), "%q must accept png", p)
		assert.True(t, Accepts(p, "image/jpeg"), "%q must accept jpeg", p)
	}
}

func TestToAcceptedConvertsGIFForXAI(t *testing.T) {
	in := gifImage(t, 200)
	out, err := ToAccepted(in, "xai")
	require.NoError(t, err)
	assert.Equal(t, "image/png", out.MIME)
	assert.Equal(t, "a.png", out.Filename, "the name must match the new format")

	_, err = png.Decode(bytes.NewReader(out.Data))
	assert.NoError(t, err, "the result must really be a png")
}

// An accepted format must be returned byte for byte: converting it would only
// lose quality.
func TestToAcceptedLeavesAcceptedFormats(t *testing.T) {
	in := gifImage(t, 100)
	out, err := ToAccepted(in, "openai")
	require.NoError(t, err)
	assert.Equal(t, in.Data, out.Data)
	assert.Equal(t, "image/gif", out.MIME)
}

// An image with no MIME is left alone rather than guessed at.
func TestToAcceptedIgnoresUnknownMIME(t *testing.T) {
	in := llm.Image{Data: []byte("xx"), Filename: "a.bin"}
	out, err := ToAccepted(in, "xai")
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

// A corrupt image must be reported, not silently replaced with a broken one.
func TestToAcceptedReportsUndecodableImages(t *testing.T) {
	in := llm.Image{Data: []byte("not really a gif"), MIME: "image/gif", Filename: "a.gif"}
	out, err := ToAccepted(in, "xai")
	require.Error(t, err)
	assert.Equal(t, in, out, "the original must be returned so the caller can decide")
}
