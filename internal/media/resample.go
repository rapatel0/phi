package media

import (
	"image"
	"image/color"
	"math"
)

// fit downscales src so neither side exceeds maxSide, preserving the aspect
// ratio and never enlarging.
//
// It averages every source pixel that falls inside a destination pixel (a box
// filter). Point sampling looks cheaper but is wrong: when the stride lands
// between thin features it skips them entirely, so a screenshot of text can
// lose whole rows of strokes at one target size and survive at the next. That
// failure depends on the content, which makes it invisible until a model
// misreads an image. Averaging costs one pass over the source and cannot drop
// a feature, only soften it.
func fit(src image.Image, maxSide int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	if w <= maxSide && h <= maxSide {
		return src
	}
	scale := float64(maxSide) / float64(w)
	if h > w {
		scale = float64(maxSide) / float64(h)
	}
	nw := max(int(float64(w)*scale+0.5), 1)
	nh := max(int(float64(h)*scale+0.5), 1)

	xs := float64(w) / float64(nw)
	ys := float64(h) / float64(nh)

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := range nh {
		y0 := float64(b.Min.Y) + float64(y)*ys
		for x := range nw {
			x0 := float64(b.Min.X) + float64(x)*xs
			dst.Set(x, y, averageBox(src, x0, y0, x0+xs, y0+ys))
		}
	}
	return dst
}

// averageBox is the mean color of a source rectangle whose edges need not
// land on pixel boundaries.
//
// Each source pixel contributes in proportion to how much of it the rectangle
// covers. Rounding the edges to whole pixels instead is wrong at fractional
// scales: at 1.5x some destination pixels would average two source rows and
// their neighbors only one, which shifts brightness by up to 0.08 on fine
// detail.
func averageBox(src image.Image, fx0, fy0, fx1, fy1 float64) color.RGBA {
	x0, x1 := int(math.Floor(fx0)), int(math.Ceil(fx1))
	y0, y1 := int(math.Floor(fy0)), int(math.Ceil(fy1))

	var r, g, b, a, total float64
	for y := y0; y < y1; y++ {
		cy := overlap(float64(y), float64(y+1), fy0, fy1)
		if cy <= 0 {
			continue
		}
		for x := x0; x < x1; x++ {
			cx := overlap(float64(x), float64(x+1), fx0, fx1)
			if cx <= 0 {
				continue
			}
			weight := cx * cy
			cr, cg, cb, ca := src.At(x, y).RGBA()
			r += float64(cr) * weight
			g += float64(cg) * weight
			b += float64(cb) * weight
			a += float64(ca) * weight
			total += weight
		}
	}
	if total == 0 {
		return color.RGBA{}
	}
	// RGBA() returns alpha-premultiplied 16-bit values; >>8 narrows to 8 bits.
	return color.RGBA{
		R: to8(r / total),
		G: to8(g / total),
		B: to8(b / total),
		A: to8(a / total),
	}
}

// to8 narrows a 16-bit channel sum to 8 bits, clamping rather than wrapping.
// Floating-point rounding can push a full-intensity channel just past 65535,
// and a bare conversion would wrap that to 0 — a white pixel turning black.
func to8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 65535 {
		return 255
	}
	return uint8(v / 256)
}

// overlap is the length shared by two 1-D spans.
func overlap(a0, a1, b0, b1 float64) float64 {
	return math.Min(a1, b1) - math.Max(a0, b0)
}

// withinLimits reports whether an image can be sent untouched. Unknown
// dimensions (0) fail the check, so an image we cannot measure still goes
// through the normal decode path rather than being trusted blindly.
func withinLimits(size, w, h int) bool {
	return size <= maxOutBytes && w > 0 && h > 0 && w <= maxDim && h <= maxDim
}
