package tui

// Terminal-background detection and the composer/user-message surface color.
//
// codex paints the composer block (and user-authored history cells) with
// `user_message_style()` (tui/src/style.rs): the terminal's default background
// blended 12% toward white on dark backgrounds, or 4% toward black on light
// ones. The terminal background itself comes from the OSC 11 default-color
// query (terminal_palette::default_bg); when the terminal does not answer,
// codex falls back to an unstyled (transparent) surface. This file ports the
// probe and the surface derivation over the existing color.go Blend/IsLight.

import (
	"fmt"
	"os"

	"github.com/muesli/termenv"
)

// Hex renders the color as a lipgloss-compatible "#rrggbb" string.
func (c RGB) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// DetectTerminalBackground queries the terminal's default background color via
// termenv (the OSC 11 query codex performs in terminal_probe.rs). It must run
// BEFORE the bubbletea program owns the tty. ok is false when stdout is not a
// terminal, so callers fall back to the unstyled surface exactly like codex's
// default_bg() == None path.
func DetectTerminalBackground() (RGB, bool) {
	if !isTerminalFile(os.Stdout) {
		return RGB{}, false
	}
	out := termenv.NewOutput(os.Stdout)
	bg := out.BackgroundColor()
	r, g, b := termenv.ConvertToRGB(bg).RGB255()
	return RGB{R: r, G: g, B: b}, true
}

// isTerminalFile reports whether f is an interactive character device.
func isTerminalFile(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// userMessageBg ports codex's style::user_message_bg: the surface color for the
// composer block and user-authored cells, derived from the terminal background.
func userMessageBg(terminalBg RGB) RGB {
	if IsLight(terminalBg) {
		return Blend(RGB{0, 0, 0}, terminalBg, 0.04)
	}
	return Blend(RGB{255, 255, 255}, terminalBg, 0.12)
}
