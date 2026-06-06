package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestUserMessageBg locks the surface derivation to codex's user_message_bg
// math: 12% toward white on dark backgrounds, 4% toward black on light ones.
func TestUserMessageBg(t *testing.T) {
	cases := []struct {
		name string
		bg   RGB
		want RGB
	}{
		// blend((255,255,255), (0,0,0), 0.12) = (30,30,30)
		{"black terminal", RGB{0, 0, 0}, RGB{30, 30, 30}},
		// blend((0,0,0), (255,255,255), 0.04) = (244,244,244)
		{"white terminal", RGB{255, 255, 255}, RGB{244, 244, 244}},
		// dark gray stays dark-blended: 255*0.12 + 30*0.88 = 57.0 -> 57
		{"dark gray terminal", RGB{30, 30, 30}, RGB{57, 57, 57}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := userMessageBg(tc.bg); got != tc.want {
				t.Errorf("userMessageBg(%v) = %v, want %v", tc.bg, got, tc.want)
			}
		})
	}
}

// TestComposerSurfaceBackgroundPainted verifies the composer block rows carry
// the surface background (full-width band) when the terminal background is
// known, mirroring codex's Block::default().style(user_message_style()) paint
// over composer_rect.
func TestComposerSurfaceBackgroundPainted(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	p := newIdleBottomPane("Explain this codebase")
	p.theme = p.theme.WithTerminalBackground(RGB{0, 0, 0}, true)

	rows := p.renderComposerBlock(40)
	if len(rows) != 3 {
		t.Fatalf("composer block rows = %d, want 3", len(rows))
	}
	// userMessageBg(black) = #1e1e1e -> truecolor bg sequence 48;2;30;30;30.
	const bgSeq = "48;2;30;30;30"
	for i, row := range rows {
		if !strings.Contains(row, bgSeq) {
			t.Errorf("row %d %q missing surface bg %q", i, row, bgSeq)
		}
	}
	// The padding rows must span the full width so the band covers the block.
	if w := runeDisplayWidth(stripSGR(rows[0])); w != 40 {
		t.Errorf("top padding width = %d, want 40", w)
	}
	if w := runeDisplayWidth(stripSGR(rows[1])); w != 40 {
		t.Errorf("prompt row width = %d, want 40", w)
	}
}

// TestComposerSurfaceAbsentWithoutTerminalBg verifies the unstyled fallback:
// with no detected terminal background the rows render exactly as before
// (codex's default_bg() == None path).
func TestComposerSurfaceAbsentWithoutTerminalBg(t *testing.T) {
	p := newIdleBottomPane("Explain this codebase")
	p.theme = p.theme.WithTerminalBackground(RGB{}, false)
	rows := p.renderComposerBlock(40)
	if rows[0] != "" || rows[2] != "" {
		t.Errorf("padding rows = %q, %q; want empty (unstyled fallback)", rows[0], rows[2])
	}
	if got := stripSGR(rows[1]); got != "› Explain this codebase" {
		t.Errorf("prompt row = %q, want %q", got, "› Explain this codebase")
	}
}
