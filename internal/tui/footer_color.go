package tui

// Footer status-line color resolution.
//
// codex renders the idle composer footer (the default status line
// `model-with-reasoning · current-dir`) with per-segment theme colors softened
// toward their own luma, NOT a flat dim. Each segment's foreground comes from the
// active syntect theme via foreground_style_for_scopes(accent.scopes()) and is
// then run through soften_status_line_style (status_line_style.rs). The separator
// " · " is always dim.
//
// codexgo cannot resolve syntect TextMate themes, but for the default dark theme
// the resolved-and-softened RGB values are fixed and were captured byte-for-byte
// from codex 0.136.0 at TrueColor:
//
//	model span "<model> <reasoning>" : \x1b[38;2;246;226;183m  (#f6e2b7), not dim
//	separator  " · "                 : \x1b[2m  (dim, default fg)
//	path span  "<current-dir>"       : \x1b[38;2;171;223;167m  (#abdfa7), not dim
//
// These are pinned as constants below. The soften algorithm is ported so the
// values are reproducible/auditable and so a future syntect-backed resolver can
// feed raw theme colors through the same math.

// Captured softened RGB for the default-theme status-line accents (TrueColor).
var (
	// statusLineModelColor is the softened "Model" accent (entity.name.type /
	// support.type / variable scopes) for the default theme: #f6e2b7.
	statusLineModelColor = RGB{R: 246, G: 226, B: 183}
	// statusLinePathColor is the softened "Path" accent (string /
	// markup.underline.link scopes) for the default theme: #abdfa7.
	statusLinePathColor = RGB{R: 171, G: 223, B: 167}
)

// statusLineColorSaturationPercent / statusLineColorBrightnessPercent mirror the
// STATUS_LINE_COLOR_* constants in status_line_style.rs.
const (
	statusLineColorSaturationPercent = 85
	statusLineColorBrightnessPercent = 100
)

// softenStatusLineColor softens an RGB color toward its own luma, mirroring
// soften_status_line_color (status_line_style.rs) for the Color::Rgb case. It is
// a faithful port (verified: soften(255,0,0) == (228,11,11), the Rust unit-test
// value) used to derive accent colors when raw theme colors are available.
func softenStatusLineColor(c RGB) RGB {
	luma := weightedLuma(c.R, c.G, c.B)
	return RGB{
		R: softenRGBChannel(c.R, luma),
		G: softenRGBChannel(c.G, luma),
		B: softenRGBChannel(c.B, luma),
	}
}

// weightedLuma computes the integer weighted luma, mirroring weighted_luma
// (status_line_style.rs): (77*r + 150*g + 29*b) / 256.
func weightedLuma(r, g, b uint8) uint16 {
	return (77*uint16(r) + 150*uint16(g) + 29*uint16(b)) / 256
}

// softenRGBChannel softens one channel toward luma, mirroring soften_rgb_channel
// (status_line_style.rs) with the saturation/brightness constants above.
func softenRGBChannel(channel uint8, luma uint16) uint8 {
	c := uint16(channel)
	softened := (c*statusLineColorSaturationPercent +
		luma*(100-statusLineColorSaturationPercent) + 50) / 100
	return uint8((softened*statusLineColorBrightnessPercent + 50) / 100)
}
