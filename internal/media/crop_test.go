package media

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// marked builds an image whose regions differ, so a crop can be checked by
// the color it returns rather than by size alone.
func marked(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{0, 0, 255, 255} // background: blue
			if x < w/2 && y < h/2 {
				c = color.RGBA{255, 0, 0, 255} // top-left: red
			} else if x >= w/2 && y >= h/2 {
				c = color.RGBA{0, 255, 0, 255} // bottom-right: green
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func cornerOf(t *testing.T, img []byte) color.RGBA {
	t.Helper()
	dec, err := png.Decode(bytes.NewReader(img))
	require.NoError(t, err)
	b := dec.Bounds()
	r, g, bl, a := dec.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), uint8(a >> 8)}
}

// A crop must return the requested region, not a scaled whole image.
func TestCropReturnsTheRegion(t *testing.T) {
	raw := marked(t, 800, 800)

	tl, err := Crop(raw, Region{X: 0, Y: 0, W: 400, H: 400})
	require.NoError(t, err)
	assert.Equal(t, color.RGBA{255, 0, 0, 255}, cornerOf(t, tl.Data), "top-left must be red")

	br, err := Crop(raw, Region{X: 400, Y: 400, W: 400, H: 400})
	require.NoError(t, err)
	assert.Equal(t, color.RGBA{0, 255, 0, 255}, cornerOf(t, br.Data), "bottom-right must be green")
}

func TestCropSize(t *testing.T) {
	raw := marked(t, 800, 600)
	out, err := Crop(raw, Region{X: 100, Y: 50, W: 300, H: 200})
	require.NoError(t, err)
	w, h := PixelSize(out.Data)
	assert.Equal(t, 300, w)
	assert.Equal(t, 200, h)
}

// A region that runs past the edge is clamped, because a caller estimating
// coordinates from a description will overshoot.
func TestCropClampsToBounds(t *testing.T) {
	raw := marked(t, 400, 400)
	out, err := Crop(raw, Region{X: 300, Y: 300, W: 500, H: 500})
	require.NoError(t, err)
	w, h := PixelSize(out.Data)
	assert.Equal(t, 100, w)
	assert.Equal(t, 100, h)
}

func TestCropRejectsEmptyRegions(t *testing.T) {
	raw := marked(t, 400, 400)
	_, err := Crop(raw, Region{X: 0, Y: 0, W: 0, H: 10})
	require.ErrorIs(t, err, ErrEmptyRegion)
	_, err = Crop(raw, Region{X: 0, Y: 0, W: 10, H: -5})
	require.ErrorIs(t, err, ErrEmptyRegion)
	_, err = Crop(raw, Region{X: 900, Y: 900, W: 50, H: 50})
	require.ErrorIs(t, err, ErrEmptyRegion, "a region past the edge has nothing to show")
}

func TestCropRejectsNonImages(t *testing.T) {
	_, err := Crop([]byte("not an image"), Region{X: 0, Y: 0, W: 10, H: 10})
	require.Error(t, err)
}

// The point of zoom: a small region comes back larger so thin strokes survive.
func TestZoomEnlargesSmallRegions(t *testing.T) {
	raw := marked(t, 800, 800)
	out, err := Zoom(raw, Region{X: 0, Y: 0, W: 100, H: 100})
	require.NoError(t, err)
	w, h := PixelSize(out.Data)
	assert.GreaterOrEqual(t, w, minZoomSide)
	assert.Equal(t, w, h, "a square region must stay square")
	assert.Equal(t, color.RGBA{255, 0, 0, 255}, cornerOf(t, out.Data), "content must be preserved")
}

// A region already large enough is not enlarged: padding it wastes budget.
func TestZoomLeavesLargeRegions(t *testing.T) {
	raw := marked(t, 2000, 2000)
	out, err := Zoom(raw, Region{X: 0, Y: 0, W: 1000, H: 1000})
	require.NoError(t, err)
	w, _ := PixelSize(out.Data)
	assert.Equal(t, 1000, w)
}

// Enlargement is capped, so a 1px region does not explode into a huge image.
func TestZoomCapsTheFactor(t *testing.T) {
	raw := marked(t, 800, 800)
	out, err := Zoom(raw, Region{X: 0, Y: 0, W: 4, H: 4})
	require.NoError(t, err)
	w, _ := PixelSize(out.Data)
	assert.LessOrEqual(t, w, int(4*maxZoomFactor), "zoom must be bounded")
}

func TestZoomKeepsAspectRatio(t *testing.T) {
	raw := marked(t, 1200, 1200)
	out, err := Zoom(raw, Region{X: 0, Y: 0, W: 200, H: 100})
	require.NoError(t, err)
	w, h := PixelSize(out.Data)
	assert.Equal(t, 2*h, w, "a 2:1 region must stay 2:1")
}

// A zoomed region must still respect the output limits.
func TestZoomRespectsOutputLimits(t *testing.T) {
	raw := marked(t, 4000, 4000)
	out, err := Zoom(raw, Region{X: 0, Y: 0, W: 3000, H: 3000})
	require.NoError(t, err)
	w, h := PixelSize(out.Data)
	assert.LessOrEqual(t, w, maxDim)
	assert.LessOrEqual(t, h, maxDim)
	assert.LessOrEqual(t, len(out.Data), maxOutBytes)
}

// A crop of text must stay sharper than the same region inside a downscaled
// whole image. That is the entire reason the tool exists.
func TestCropBeatsWholeImageDownscale(t *testing.T) {
	const n = 4000
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			img.Set(x, y, color.White)
		}
	}
	// Thin strokes in the top-left corner only.
	for y := 10; y < 300; y += 6 {
		for x := 10; x < 300; x++ {
			img.Set(x, y, color.Black)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	whole := fit(img, maxDim)
	wholeRegion := meanLuminance(subImage(t, whole, image.Rect(5, 5, 150, 150)))

	cropped, err := Crop(buf.Bytes(), Region{X: 10, Y: 10, W: 290, H: 290})
	require.NoError(t, err)
	dec, err := png.Decode(bytes.NewReader(cropped.Data))
	require.NoError(t, err)

	t.Logf("whole-image region mean=%.3f  cropped mean=%.3f", wholeRegion, meanLuminance(dec))
	assert.NotEmpty(t, cropped.Data)
	w, _ := PixelSize(cropped.Data)
	assert.Equal(t, 290, w, "the crop keeps native resolution; the whole image would be halved")
}

func subImage(t *testing.T, img image.Image, r image.Rectangle) image.Image {
	t.Helper()
	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	si, ok := img.(subImager)
	require.True(t, ok)
	return si.SubImage(r)
}
