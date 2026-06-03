package tui

import "math"

// RGB is an 8-bit-per-channel color, the Go analogue of the Rust `(u8, u8, u8)`
// tuples used throughout codex-rs/tui/src/color.rs and terminal_palette.rs.
type RGB struct {
	R uint8
	G uint8
	B uint8
}

// IsLight reports whether the supplied background color is perceptually light,
// using the same luma threshold as the Rust `color::is_light`.
//
// Port of codex-rs/tui/src/color.rs `is_light`.
func IsLight(bg RGB) bool {
	y := 0.299*float64(bg.R) + 0.587*float64(bg.G) + 0.114*float64(bg.B)
	return y > 128.0
}

// Blend linearly interpolates fg over bg by alpha in [0, 1] and returns the
// resulting opaque color. alpha == 1 yields fg; alpha == 0 yields bg.
//
// Port of codex-rs/tui/src/color.rs `blend`.
func Blend(fg, bg RGB, alpha float64) RGB {
	mix := func(f, b uint8) uint8 {
		return uint8(float64(f)*alpha + float64(b)*(1.0-alpha))
	}
	return RGB{
		R: mix(fg.R, bg.R),
		G: mix(fg.G, bg.G),
		B: mix(fg.B, bg.B),
	}
}

// PerceptualDistance returns the CIE76 perceptual distance between two RGB
// colors (Euclidean distance in CIE Lab space). Smaller is more similar.
//
// Port of codex-rs/tui/src/color.rs `perceptual_distance`.
func PerceptualDistance(a, b RGB) float64 {
	la, aa, ba := rgbToLab(a)
	lb, ab, bb := rgbToLab(b)
	dl := la - lb
	da := aa - ab
	db := ba - bb
	return math.Sqrt(dl*dl + da*da + db*db)
}

// srgbToLinear converts an sRGB channel value to linear RGB.
func srgbToLinear(c uint8) float64 {
	v := float64(c) / 255.0
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// rgbToXYZ converts an sRGB color to CIE XYZ.
func rgbToXYZ(c RGB) (x, y, z float64) {
	r := srgbToLinear(c.R)
	g := srgbToLinear(c.G)
	b := srgbToLinear(c.B)
	x = r*0.4124 + g*0.3576 + b*0.1805
	y = r*0.2126 + g*0.7152 + b*0.0722
	z = r*0.0193 + g*0.1192 + b*0.9505
	return
}

// xyzToLab converts CIE XYZ (D65) to CIE Lab.
func xyzToLab(x, y, z float64) (l, a, b float64) {
	xr := x / 0.95047
	yr := y / 1.00000
	zr := z / 1.08883

	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Cbrt(t)
		}
		return 7.787*t + 16.0/116.0
	}

	fx := f(xr)
	fy := f(yr)
	fz := f(zr)

	l = 116.0*fy - 16.0
	a = 500.0 * (fx - fy)
	b = 200.0 * (fy - fz)
	return
}

// rgbToLab converts an sRGB color directly to CIE Lab.
func rgbToLab(c RGB) (l, a, b float64) {
	x, y, z := rgbToXYZ(c)
	return xyzToLab(x, y, z)
}
