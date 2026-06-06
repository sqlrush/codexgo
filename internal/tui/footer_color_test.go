package tui

import (
	"testing"
)

// TestSoftenStatusLineColorMatchesRust verifies the soften port against the
// value pinned by the Rust unit test
// (status_line_segments_soften_rgb_theme_styles_without_dimming_text):
// soften(255, 0, 0) == (228, 11, 11).
func TestSoftenStatusLineColorMatchesRust(t *testing.T) {
	got := softenStatusLineColor(RGB{R: 255, G: 0, B: 0})
	want := RGB{R: 228, G: 11, B: 11}
	if got != want {
		t.Fatalf("softenStatusLineColor(255,0,0) = %v, want %v", got, want)
	}
}

// TestStatusLineAccentColorsMatchCodex pins the resolved-and-softened default
// theme accent colors against the values captured byte-for-byte from codex
// 0.136.0 (model #f6e2b7, path #abdfa7).
func TestStatusLineAccentColorsMatchCodex(t *testing.T) {
	if statusLineModelColor != (RGB{R: 246, G: 226, B: 183}) {
		t.Fatalf("model accent = %v, want #f6e2b7 (246,226,183)", statusLineModelColor)
	}
	if statusLinePathColor != (RGB{R: 171, G: 223, B: 167}) {
		t.Fatalf("path accent = %v, want #abdfa7 (171,223,167)", statusLinePathColor)
	}
}

// TestFooterStyledLineSGRMatchesCodex pins codexgo's footer SGR byte stream
// against the per-segment styling captured from codex 0.136.0 at TrueColor:
//
//	model "<model> <reasoning>" → fg RGB(246,226,183) #f6e2b7, NOT dim
//	separator " · "            → dim, default fg
//	path "<current-dir>"       → fg RGB(171,223,167) #abdfa7, NOT dim
//	2-col indent gutter        → unstyled
//
// codex emits the SGR in a different byte order (\x1b[22m\x1b[38;2;…;49m vs
// lipgloss's \x1b[38;2;…m\x1b[0m), but the reconstructed PER-CELL attributes —
// what the frame harness compares — are identical: the model/path cells carry the
// softened RGB foregrounds without DIM, and the separator carries DIM with the
// default foreground.
func TestFooterStyledLineSGRMatchesCodex(t *testing.T) {
	f := ComposerFooter{Model: "gpt-5.5", Directory: "/work"}
	// Render via the TrueColor renderer (the production path) so the RGB accents
	// emit verbatim regardless of the test host's auto-detected color profile.
	got := f.styledLine(80, ColorLevelTrueColor).RenderWith(trueColorRenderer)
	want := "  " +
		"\x1b[38;2;246;226;183mgpt-5.5 default\x1b[0m" +
		"\x1b[2m · \x1b[0m" +
		"\x1b[38;2;171;223;167m/work\x1b[0m"
	if got != want {
		t.Fatalf("footer styled render mismatch:\n got = %q\nwant = %q", got, want)
	}
}

// TestFooterStyledLineSpansCarryAccents verifies the structural span layout: the
// indent is unstyled, the model span carries the model accent fg (not dim), the
// separator is dim, and the path span carries the path accent fg (not dim).
func TestFooterStyledLineSpansCarryAccents(t *testing.T) {
	f := ComposerFooter{Model: "gpt-5.5", Directory: "/work"}
	line := f.styledLine(80, ColorLevelTrueColor)
	if len(line.Spans) != 4 {
		t.Fatalf("span count = %d, want 4 (indent, model, sep, path)", len(line.Spans))
	}
	// Indent gutter: unstyled.
	if line.Spans[0].Text != "  " || line.Spans[0].Style != (Style{}) {
		t.Fatalf("indent span = %+v, want unstyled two spaces", line.Spans[0])
	}
	// Model span: model accent fg (raw RGB), not dim.
	model := line.Spans[1]
	if model.Text != "gpt-5.5 default" || model.Style.Dim || model.Style.Fg != RGBColor(statusLineModelColor) {
		t.Fatalf("model span = %+v, want fg=%v not-dim", model, statusLineModelColor)
	}
	// Separator: dim, no fg.
	sep := line.Spans[2]
	if sep.Text != " · " || !sep.Style.Dim || sep.Style.Fg != nil {
		t.Fatalf("separator span = %+v, want dim default-fg", sep)
	}
	// Path span: path accent fg (raw RGB), not dim.
	path := line.Spans[3]
	if path.Text != "/work" || path.Style.Dim || path.Style.Fg != RGBColor(statusLinePathColor) {
		t.Fatalf("path span = %+v, want fg=%v not-dim", path, statusLinePathColor)
	}
}

// TestFooterStyledLineNoColorWhenLevelUnknown verifies the footer degrades to no
// accent colors (default foreground) at the no-color level, so NO_COLOR / dumb
// terminals do not carry truecolor accents on the model/path spans.
func TestFooterStyledLineNoColorWhenLevelUnknown(t *testing.T) {
	f := ComposerFooter{Model: "gpt-5.5", Directory: "/work"}
	line := f.styledLine(80, ColorLevelUnknown)
	// Spans: [indent, model, sep, path]. Only the separator carries DIM; the
	// model/path spans must carry no foreground color at the no-color level.
	for i, s := range line.Spans {
		if i == 2 { // separator stays dim regardless of color level
			continue
		}
		if s.Style.Fg != nil {
			t.Fatalf("span %q carries fg %v at no-color level, want none", s.Text, s.Style.Fg)
		}
	}
}
