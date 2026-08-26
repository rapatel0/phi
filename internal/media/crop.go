package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	"github.com/rapatel0/alpha/internal/llm"
)

// ErrEmptyRegion means a requested region does not overlap the image.
var ErrEmptyRegion = errors.New("media: region is outside the image")

// Region is a crop rectangle in source pixels.
type Region struct {
	X, Y, W, H int
}

// Crop returns the region of an image, re-encoded and normalized.
//
// This is what makes detail readable. A full screenshot is downscaled to fit
// the model's limits, and small text does not survive that. Cropping first
// means the region arrives at its own resolution instead of the whole page's,
// so the same byte budget is spent on the part that matters.
//
// The region is clamped to the image rather than rejected, because a caller
// working from a description of the image will often overshoot an edge by a
// few pixels, and a clamped crop answers the question a hard error would not.
func Crop(data []byte, r Region) (llm.Image, error) {
	if r.W <= 0 || r.H <= 0 {
		return llm.Image{}, fmt.Errorf("%w: width and height must be positive", ErrEmptyRegion)
	}
	mime := DetectMIME(data)
	if mime == "" {
		return llm.Image{}, errors.New("media: not a PNG, JPEG, GIF, or WebP")
	}
	src, err := decode(data, mime)
	if err != nil {
		return llm.Image{}, err
	}

	clamped := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Add(src.Bounds().Min).Intersect(src.Bounds())
	if clamped.Empty() {
		return llm.Image{}, fmt.Errorf("%w: image is %dx%d",
			ErrEmptyRegion, src.Bounds().Dx(), src.Bounds().Dy())
	}

	out := image.NewRGBA(image.Rect(0, 0, clamped.Dx(), clamped.Dy()))
	draw.Draw(out, out.Bounds(), src, clamped.Min, draw.Src)

	// PNG keeps text crisp; a crop is usually text or UI detail, and JPEG
	// ringing around glyph edges is exactly what defeats the purpose.
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return llm.Image{}, err
	}
	return Normalize(llm.Image{Data: buf.Bytes(), Filename: "region.png"})
}

// Zoom crops a region and scales it up so fine detail survives the trip to the
// model. A crop smaller than minZoomSide is enlarged toward that size.
//
// Enlarging adds no information, but it does stop a downstream resize from
// removing what is already there, and it keeps glyph strokes above one pixel.
func Zoom(data []byte, r Region) (llm.Image, error) {
	img, err := Crop(data, r)
	if err != nil {
		return llm.Image{}, err
	}
	w, h := PixelSize(img.Data)
	if w <= 0 || h <= 0 || w >= minZoomSide || h >= minZoomSide {
		return img, nil
	}
	scale := min(float64(minZoomSide)/float64(max(w, h)), maxZoomFactor)
	if scale <= 1 {
		return img, nil
	}
	src, err := decode(img.Data, img.MIME)
	if err != nil {
		return img, nil // the crop is still usable
	}
	enlarged := nearest(src, int(float64(w)*scale+0.5), int(float64(h)*scale+0.5))

	var buf bytes.Buffer
	if err := png.Encode(&buf, enlarged); err != nil {
		return img, nil
	}
	return Normalize(llm.Image{Data: buf.Bytes(), Filename: "region.png"})
}

// nearest enlarges by pixel replication. Upscaling cannot recover detail, so
// the honest choice is to keep hard edges rather than invent smooth ones that
// look like image content.
func nearest(src image.Image, nw, nh int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := range nh {
		sy := b.Min.Y + y*h/nh
		for x := range nw {
			dst.Set(x, y, src.At(b.Min.X+x*w/nw, sy))
		}
	}
	return dst
}
