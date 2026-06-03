package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestBestColorTrueColor(t *testing.T) {
	target := RGB{12, 34, 56}
	got := BestColor(target, ColorLevelTrueColor)
	want := RGBColor(target)
	if got != want {
		t.Fatalf("BestColor truecolor = %v, want %v", got, want)
	}
}

func TestBestColorUnknownIsNoColor(t *testing.T) {
	got := BestColor(RGB{255, 0, 0}, ColorLevelUnknown)
	if _, ok := got.(lipgloss.NoColor); !ok {
		t.Fatalf("BestColor unknown = %T, want lipgloss.NoColor", got)
	}
}

func TestBestColorAnsi256NearestExactMatch(t *testing.T) {
	// XtermColors[196] is pure red (255,0,0); its perceptual nearest among the
	// >=16 fixed palette is itself.
	got := BestColor(RGB{255, 0, 0}, ColorLevelAnsi256)
	if got != lipgloss.Color("196") {
		t.Fatalf("BestColor ansi256 red = %v, want index 196", got)
	}
}

func TestNearestXtermIndexIgnoresFirst16(t *testing.T) {
	// Even though index 9 is also pure red, only indices >=16 are considered.
	idx := nearestXtermIndex(RGB{255, 0, 0})
	if idx < 16 {
		t.Fatalf("nearestXtermIndex returned %d, want >=16", idx)
	}
}

func TestHexColor(t *testing.T) {
	if got := hexColor(RGB{0, 0, 0}); got != "#000000" {
		t.Fatalf("hexColor black = %q", got)
	}
	if got := hexColor(RGB{255, 255, 255}); got != "#ffffff" {
		t.Fatalf("hexColor white = %q", got)
	}
	if got := hexColor(RGB{18, 52, 86}); got != "#123456" {
		t.Fatalf("hexColor = %q, want #123456", got)
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 7: "7", 16: "16", 255: "255"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestXtermColorsLength(t *testing.T) {
	if len(XtermColors) != 256 {
		t.Fatalf("len(XtermColors) = %d, want 256", len(XtermColors))
	}
	// Spot-check a few well-known entries from the reference table.
	if XtermColors[16] != (RGB{0, 0, 0}) {
		t.Fatalf("XtermColors[16] = %v, want Grey0", XtermColors[16])
	}
	if XtermColors[231] != (RGB{255, 255, 255}) {
		t.Fatalf("XtermColors[231] = %v, want Grey100", XtermColors[231])
	}
	if XtermColors[196] != (RGB{255, 0, 0}) {
		t.Fatalf("XtermColors[196] = %v, want Red1", XtermColors[196])
	}
}
