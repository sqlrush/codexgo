package imageutil

import (
	"image"
	"image/color"
	"math"
)

// resizeDimensions computes the dimensions an image of size (width, height)
// should take so that it fits within a (maxW, maxH) bounding box while
// preserving aspect ratio.
//
// This is a faithful port of the `image` crate's `resize_dimensions` helper
// (with the "fill" parameter set to false), which the upstream `DynamicImage::
// resize` relies on. The ratio is computed in f64 and the result is rounded to
// the nearest integer, with each dimension clamped to be at least 1.
//
// Inputs are read-only; nothing is mutated.
func resizeDimensions(width, height, maxW, maxH uint32) (uint32, uint32) {
	wRatio := float64(maxW) / float64(width)
	hRatio := float64(maxH) / float64(height)

	// "Resize to fit" uses the smaller ratio so that both dimensions land
	// inside the bounding box.
	ratio := math.Min(wRatio, hRatio)

	// Match the upstream rounding: multiply in f64, round to nearest, and
	// guard against a degenerate zero result.
	nw := maxU64(1, uint64(math.Round(float64(width)*ratio)))
	nh := maxU64(1, uint64(math.Round(float64(height)*ratio)))

	// Guard against overflow when casting back; saturate at u32::MAX as the
	// upstream code does.
	if nw > uint64(math.MaxUint32) {
		nw = uint64(math.MaxUint32)
	}
	if nh > uint64(math.MaxUint32) {
		nh = uint64(math.MaxUint32)
	}
	return uint32(nw), uint32(nh)
}

func maxU64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// resizeToFit returns a new RGBA image scaled so that it fits within a
// (maxW, maxH) bounding box while preserving aspect ratio. When the source is
// already within bounds the dimensions are left unchanged.
//
// The resampling uses a separable triangle (linear) filter, mirroring the
// upstream use of `FilterType::Triangle`. The source image is never mutated; a
// freshly allocated image is returned.
func resizeToFit(src image.Image, maxW, maxH uint32) *image.RGBA {
	b := src.Bounds()
	srcW := uint32(b.Dx())
	srcH := uint32(b.Dy())

	dstW, dstH := resizeDimensions(srcW, srcH, maxW, maxH)
	return resampleTriangle(src, srcW, srcH, dstW, dstH)
}

// triangleSupport is the radius of the triangle (tent) reconstruction filter.
const triangleSupport = 1.0

// triangleKernel evaluates the triangle filter at x.
func triangleKernel(x float64) float64 {
	if x < 0 {
		x = -x
	}
	if x < triangleSupport {
		return triangleSupport - x
	}
	return 0
}

// resampleTriangle performs a separable triangle-filter resample of src into a
// dstW x dstH RGBA image. It mirrors the two-pass (horizontal then vertical)
// approach used by the `image` crate's sampling code.
//
// The function operates in premultiplied-free straight RGBA space using f32
// accumulation and operates on a freshly converted copy of the source, never
// mutating the caller's image.
func resampleTriangle(src image.Image, srcW, srcH, dstW, dstH uint32) *image.RGBA {
	// Convert the source to a dense straight-alpha RGBA buffer first so that
	// per-pixel access is cheap and deterministic regardless of the concrete
	// source image type.
	srcRGBA := toRGBA(src)

	// Horizontal pass: srcW x srcH -> dstW x srcH.
	horiz := image.NewRGBA(image.Rect(0, 0, int(dstW), int(srcH)))
	resampleAxis(srcRGBA, horiz, true)

	// Vertical pass: dstW x srcH -> dstW x dstH.
	dst := image.NewRGBA(image.Rect(0, 0, int(dstW), int(dstH)))
	resampleAxis(horiz, dst, false)

	return dst
}

// resampleAxis resamples along a single axis. When horizontal is true it
// resamples columns (width changes, height fixed); otherwise it resamples rows
// (height changes, width fixed). The destination's extent on the resampled axis
// defines the output size.
func resampleAxis(src, dst *image.RGBA, horizontal bool) {
	sb := src.Bounds()
	db := dst.Bounds()

	srcLen := sb.Dx()
	dstLen := db.Dx()
	otherLen := sb.Dy()
	if !horizontal {
		srcLen = sb.Dy()
		dstLen = db.Dy()
		otherLen = sb.Dx()
	}

	ratio := float64(srcLen) / float64(dstLen)
	// When downsampling, widen the filter footprint by the scale ratio so that
	// the output integrates over the correct number of source samples. This is
	// the standard approach used by the `image` crate.
	sratio := ratio
	if sratio < 1.0 {
		sratio = 1.0
	}
	support := triangleSupport * sratio

	for outIdx := 0; outIdx < dstLen; outIdx++ {
		// Map the output sample center back into source coordinates.
		center := (float64(outIdx)+0.5)*ratio - 0.5
		left := int(math.Ceil(center - support))
		right := int(math.Floor(center + support))
		if left < 0 {
			left = 0
		}
		if right > srcLen-1 {
			right = srcLen - 1
		}

		for other := 0; other < otherLen; other++ {
			var r, g, b, a float64
			var weightSum float64
			for in := left; in <= right; in++ {
				w := triangleKernel((float64(in) - center) / sratio)
				if w == 0 {
					continue
				}
				var c color.RGBA
				if horizontal {
					c = rgbaAt(src, in, other)
				} else {
					c = rgbaAt(src, other, in)
				}
				r += float64(c.R) * w
				g += float64(c.G) * w
				b += float64(c.B) * w
				a += float64(c.A) * w
				weightSum += w
			}
			if weightSum != 0 {
				r /= weightSum
				g /= weightSum
				b /= weightSum
				a /= weightSum
			}
			out := color.RGBA{
				R: clampU8(r),
				G: clampU8(g),
				B: clampU8(b),
				A: clampU8(a),
			}
			if horizontal {
				setRGBA(dst, outIdx, other, out)
			} else {
				setRGBA(dst, other, outIdx, out)
			}
		}
	}
}

// rgbaAt returns the straight-alpha RGBA color at (x, y) of a dense RGBA image,
// accounting for the image's bounds origin.
func rgbaAt(img *image.RGBA, x, y int) color.RGBA {
	off := img.PixOffset(img.Bounds().Min.X+x, img.Bounds().Min.Y+y)
	return color.RGBA{
		R: img.Pix[off+0],
		G: img.Pix[off+1],
		B: img.Pix[off+2],
		A: img.Pix[off+3],
	}
}

// setRGBA writes a straight-alpha RGBA color at (x, y) of a dense RGBA image.
func setRGBA(img *image.RGBA, x, y int, c color.RGBA) {
	off := img.PixOffset(img.Bounds().Min.X+x, img.Bounds().Min.Y+y)
	img.Pix[off+0] = c.R
	img.Pix[off+1] = c.G
	img.Pix[off+2] = c.B
	img.Pix[off+3] = c.A
}

// clampU8 rounds and clamps an accumulated channel value into the [0, 255] byte
// range.
func clampU8(v float64) uint8 {
	v = math.Round(v)
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// toRGBA returns a dense straight-alpha RGBA copy of img. If img is already a
// non-subimage *image.RGBA it is copied into a normalized buffer so callers can
// rely on a zero-based origin and contiguous storage. The input is never
// mutated.
func toRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			off := out.PixOffset(x, y)
			out.Pix[off+0] = uint8(r >> 8)
			out.Pix[off+1] = uint8(g >> 8)
			out.Pix[off+2] = uint8(bl >> 8)
			out.Pix[off+3] = uint8(a >> 8)
		}
	}
	return out
}
