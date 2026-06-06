package tui

// Shimmer text effect for the live "Working" status header — a port of
// codex-rs/tui/src/shimmer.rs shimmer_spans: a gaussian-cosine band sweeps
// across the characters on a 2-second period (synchronized to process start),
// blending each character's foreground toward the terminal background at the
// band's center. On non-truecolor terminals it falls back to a dim/plain/bold
// intensity ladder (color_for_level).

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// shimmerEpoch anchors the sweep to process start, mirroring the Rust
// OnceLock<Instant> elapsed_since_start.
var shimmerEpoch = time.Now()

// shimmer tuning constants (verbatim from shimmer.rs).
const (
	shimmerPadding       = 10
	shimmerSweepSeconds  = 2.0
	shimmerBandHalfWidth = 5.0
	shimmerHighlightMix  = 0.9
)

// ShimmerRender renders text with the moving-highlight effect. fg is the base
// text color and bg the highlight target (the detected terminal background);
// hasTrueColor selects the RGB blend path versus the modifier ladder.
func ShimmerRender(text string, fg, bg RGB, hasTrueColor bool) string {
	chars := []rune(text)
	if len(chars) == 0 {
		return ""
	}

	period := float64(len(chars) + shimmerPadding*2)
	elapsed := time.Since(shimmerEpoch).Seconds()
	pos := math.Mod(elapsed, shimmerSweepSeconds) / shimmerSweepSeconds * period

	var b strings.Builder
	for i, ch := range chars {
		dist := math.Abs(float64(i+shimmerPadding) - pos)
		t := 0.0
		if dist <= shimmerBandHalfWidth {
			x := math.Pi * (dist / shimmerBandHalfWidth)
			t = 0.5 * (1.0 + math.Cos(x))
		}
		b.WriteString(shimmerCharStyle(t, fg, bg, hasTrueColor).Render(string(ch)))
	}
	return b.String()
}

// shimmerCharStyle resolves the style for one character at band intensity t.
func shimmerCharStyle(t float64, fg, bg RGB, hasTrueColor bool) lipgloss.Style {
	if hasTrueColor {
		blended := Blend(bg, fg, clamp01(t)*shimmerHighlightMix)
		return lipgloss.NewStyle().
			Renderer(trueColorRenderer).
			Foreground(lipgloss.Color(blended.Hex())).
			Bold(true)
	}
	// color_for_level fallback: dim below 0.2, plain to 0.6, bold above.
	switch {
	case t < 0.2:
		return lipgloss.NewStyle().Faint(true)
	case t < 0.6:
		return lipgloss.NewStyle()
	default:
		return lipgloss.NewStyle().Bold(true)
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
