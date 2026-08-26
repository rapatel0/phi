package media

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
)

// meanLuminance is the physically meaningful check for a downscale: shrinking
// an image must not change how bright it is overall. Point sampling drifts
// when the stride resonates with the content; averaging does not.
func meanLuminance(img image.Image) float64 {
	b := img.Bounds()
	var sum, n float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257 / 256
			n++
		}
	}
	return sum / n
}

// hlines draws 1px horizontal strokes every stride pixels: a stand-in for
// small text, which is the content most at risk from a bad downscale.
func hlines(n, stride int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			img.Set(x, y, color.White)
		}
	}
	for y := stride / 2; y < n; y += stride {
		for x := range n {
			img.Set(x, y, color.Black)
		}
	}
	return img
}

// The regression this guards: point sampling drops entire rows of strokes at
// some target sizes and not others, so an image silently loses its content.
//
// The failure is a resonance between the stroke period and the sampling
// stride, so it only appears at particular combinations. The sweep is wide on
// purpose: testing a few sizes passes even with point sampling and proves
// nothing. The worst observed point-sampling drift here is about 0.42, which
// is most of the full brightness range.
func TestFitPreservesBrightness(t *testing.T) {
	for _, n := range []int{1200, 1800} {
		for stride := 3; stride <= 13; stride++ {
			src := hlines(n, stride)
			want := meanLuminance(src)
			for _, target := range []int{800, 600, 400, 300, 200, 150} {
				got := meanLuminance(fit(src, target))
				assert.InDelta(t, want, got, 0.05,
					"n=%d stride=%d target=%d: brightness drifted, content was lost",
					n, stride, target)
			}
		}
	}
}

// A flat field must survive exactly: any drift here means the filter itself
// is wrong, independent of resonance.
func TestFitPreservesFlatColor(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 900, 900))
	for y := range 900 {
		for x := range 900 {
			src.Set(x, y, color.RGBA{40, 120, 200, 255})
		}
	}
	out := fit(src, 300)
	r, g, b, _ := out.At(150, 150).RGBA()
	assert.InDelta(t, 40, r>>8, 1)
	assert.InDelta(t, 120, g>>8, 1)
	assert.InDelta(t, 200, b>>8, 1)
}

func TestFitKeepsAspectAndNeverEnlarges(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	out := fit(src, 800)
	assert.Equal(t, 800, out.Bounds().Dx())
	assert.Equal(t, 450, out.Bounds().Dy(), "16:9 must stay 16:9")

	small := image.NewRGBA(image.Rect(0, 0, 100, 50))
	assert.Same(t, small, fit(small, 800), "an image under the limit is returned untouched")
}

// A tall image must be bounded by its height, not its width.
func TestFitBoundsTallImages(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 2000))
	out := fit(src, 500)
	assert.LessOrEqual(t, out.Bounds().Dx(), 500)
	assert.Equal(t, 500, out.Bounds().Dy())
}

// An extreme aspect ratio must not collapse a side to zero.
func TestFitDegenerateAspect(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4000, 3))
	out := fit(src, 100)
	assert.Positive(t, out.Bounds().Dx())
	assert.Positive(t, out.Bounds().Dy(), "a side must never round down to zero")
}

func gifBytes(t *testing.T, dim, frames int) []byte {
	t.Helper()
	g := &gif.GIF{}
	for f := range frames {
		img := image.NewPaletted(image.Rect(0, 0, dim, dim), []color.Color{color.Black, color.White})
		for y := range dim {
			for x := range dim {
				img.SetColorIndex(x, y, uint8((x/8+y/8+f)%2))
			}
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 10)
	}
	var buf bytes.Buffer
	require.NoError(t, gif.EncodeAll(&buf, g))
	return buf.Bytes()
}

// The regression this guards: every GIF used to be re-encoded as JPEG, which
// inflated small files and flattened animation to one frame.
func TestNormalizeKeepsSmallGIFExactly(t *testing.T) {
	raw := gifBytes(t, 500, 1)
	out, err := Normalize(llm.Image{Data: raw, Filename: "a.gif"})
	require.NoError(t, err)
	assert.Equal(t, "image/gif", out.MIME)
	assert.Equal(t, raw, out.Data, "an image inside every limit must be sent byte for byte")
}

func TestNormalizeKeepsAnimation(t *testing.T) {
	raw := gifBytes(t, 400, 5)
	out, err := Normalize(llm.Image{Data: raw, Filename: "anim.gif"})
	require.NoError(t, err)
	require.Equal(t, "image/gif", out.MIME)

	decoded, err := gif.DecodeAll(bytes.NewReader(out.Data))
	require.NoError(t, err)
	assert.Len(t, decoded.Image, 5, "animation must survive")
}

// The fast path must not swallow an image that is genuinely too big.
func TestNormalizeStillShrinksOversizeImages(t *testing.T) {
	const n = 3000
	src := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			src.Set(x, y, color.RGBA{uint8(x % 251), uint8(y % 253), uint8((x + y) % 249), 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, src))

	out, err := Normalize(llm.Image{Data: buf.Bytes(), Filename: "big.png"})
	require.NoError(t, err)
	w, h := PixelSize(out.Data)
	assert.LessOrEqual(t, w, maxDim)
	assert.LessOrEqual(t, h, maxDim)
	assert.LessOrEqual(t, len(out.Data), maxOutBytes)
}

// A PNG already inside the limits must keep its exact bytes, not be recompressed.
func TestNormalizeKeepsSmallPNGExactly(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for y := range 200 {
		for x := range 300 {
			src.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, src))
	raw := buf.Bytes()

	out, err := Normalize(llm.Image{Data: raw, Filename: "small.png"})
	require.NoError(t, err)
	assert.Equal(t, "image/png", out.MIME)
	assert.Equal(t, raw, out.Data)
}

func TestWithinLimits(t *testing.T) {
	assert.True(t, withinLimits(1000, 100, 100))
	assert.False(t, withinLimits(maxOutBytes+1, 100, 100), "oversize bytes must not pass")
	assert.False(t, withinLimits(1000, maxDim+1, 100), "oversize width must not pass")
	assert.False(t, withinLimits(1000, 100, maxDim+1), "oversize height must not pass")
	assert.False(t, withinLimits(1000, 0, 0), "unknown dimensions must not be trusted")
}

// Averaging works in premultiplied space, so a half-transparent red averages
// to premultiplied red at half alpha rather than to a darker opaque color.
// NRGBA is the non-premultiplied form, which is what a decoded PNG carries.
func TestAverageBoxHandlesAlpha(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			src.Set(x, y, color.NRGBA{R: 255, A: 128})
		}
	}
	got := averageBox(src, 0, 0, 4, 4)
	assert.InDelta(t, 128, got.A, 2)
	assert.InDelta(t, 128, got.R, 2, "premultiplied red at half alpha is half")
	assert.Zero(t, got.G)
}

// A fully transparent region must not contribute color to its neighbors.
func TestAverageBoxTransparentStaysClear(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			src.Set(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 0})
		}
	}
	got := averageBox(src, 0, 0, 4, 4)
	assert.Zero(t, got.A)
	assert.Zero(t, got.R, "transparent white must not leak white")
}

// Guard the arithmetic directly: a checkerboard averages to mid-grey.
func TestAverageBoxIsAMean(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.White)
	src.Set(1, 0, color.Black)
	src.Set(0, 1, color.Black)
	src.Set(1, 1, color.White)

	got := averageBox(src, 0, 0, 2, 2)
	assert.InDelta(t, 127, got.R, 2)
	assert.InDelta(t, 255, got.A, 1)
}

func TestAverageBoxEmptyRect(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	assert.Equal(t, color.RGBA{}, averageBox(src, 2, 2, 2, 2), "an empty box must not divide by zero")
}

// Downscaling must stay linear in the source, not quadratic in the target.
func TestFitCostIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	src := image.NewRGBA(image.Rect(0, 0, 4000, 4000))
	out := fit(src, 200)
	require.Equal(t, 200, out.Bounds().Dx())
	assert.False(t, math.IsNaN(meanLuminance(out)))
}
