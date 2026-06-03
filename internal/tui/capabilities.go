package tui

import (
	"os"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// Capabilities is the detected terminal feature set the TUI adapts to. It is the
// Go analogue of the information codex-rs gathers from supports_color
// (terminal_palette.rs) and codex_terminal_detection (terminal_probe.rs /
// terminal_palette default color queries).
//
// Capabilities is immutable: build one with [DetectCapabilities] and pass it by
// value. Treat it as a snapshot of the environment at startup.
type Capabilities struct {
	// ColorLevel is the detected output color depth.
	ColorLevel StdoutColorLevel
	// NoColor is true when color output is suppressed (NO_COLOR is set, per the
	// https://no-color.org/ convention).
	NoColor bool
	// UnicodeWidth reports whether ambiguous-width runes are treated as wide. It
	// mirrors runewidth's East Asian Width handling so width math agrees with the
	// rendering backend.
	UnicodeWidth bool
	// IsZellij is true when running inside a Zellij multiplexer, which needs a
	// non-scroll-region history insertion path (mirrors Tui.is_zellij).
	IsZellij bool
	// IsTmux is true when running inside tmux.
	IsTmux bool
	// IsVSCode is true when running inside the VS Code integrated terminal, which
	// affects keyboard-enhancement handling (mirrors running_in_vscode_terminal).
	IsVSCode bool
	// TermProgram is the raw TERM_PROGRAM value, retained for diagnostics.
	TermProgram string
}

// environ abstracts environment lookups so detection is unit-testable.
type environ func(key string) string

// DetectCapabilities probes the process environment for terminal capabilities,
// reading NO_COLOR / COLORTERM / TERM / TERM_PROGRAM and multiplexer markers.
//
// It is a best-effort, allocation-light port of the Rust capability detection:
// the bounded OSC color-query probe (terminal_probe.rs) requires raw-tty access
// that the bubbletea runtime owns, so default-color querying is deferred to the
// runtime; here we classify color depth from environment hints exactly as the
// `supports-color` crate does.
func DetectCapabilities() Capabilities {
	return detectCapabilities(os.Getenv)
}

// detectCapabilities is the testable core of [DetectCapabilities].
func detectCapabilities(getenv environ) Capabilities {
	noColor := getenv("NO_COLOR") != ""
	termProgram := getenv("TERM_PROGRAM")

	level := classifyColorLevel(getenv)
	if noColor {
		level = ColorLevelUnknown
	}

	return Capabilities{
		ColorLevel:   level,
		NoColor:      noColor,
		UnicodeWidth: runewidth.DefaultCondition.EastAsianWidth,
		IsZellij:     getenv("ZELLIJ") != "" || getenv("ZELLIJ_SESSION_NAME") != "",
		IsTmux:       getenv("TMUX") != "" || strings.HasPrefix(getenv("TERM"), "tmux") || strings.HasPrefix(getenv("TERM"), "screen"),
		IsVSCode:     termProgram == "vscode",
		TermProgram:  termProgram,
	}
}

// classifyColorLevel determines color depth from environment hints, in the same
// precedence order as the `supports-color` crate (NO_COLOR handled by caller):
// FORCE_COLOR, COLORTERM (truecolor/24bit), TERM substrings (256/truecolor/
// direct), then a conservative fallback of 16 colors for any non-dumb TERM.
func classifyColorLevel(getenv environ) StdoutColorLevel {
	if fc := getenv("FORCE_COLOR"); fc != "" {
		switch fc {
		case "0", "false":
			return ColorLevelUnknown
		case "1", "true":
			return ColorLevelAnsi16
		case "2":
			return ColorLevelAnsi256
		case "3":
			return ColorLevelTrueColor
		default:
			return ColorLevelAnsi16
		}
	}

	colorTerm := strings.ToLower(getenv("COLORTERM"))
	if colorTerm == "truecolor" || colorTerm == "24bit" {
		return ColorLevelTrueColor
	}

	term := strings.ToLower(getenv("TERM"))
	switch {
	case term == "" || term == "dumb":
		return ColorLevelUnknown
	case strings.Contains(term, "truecolor") || strings.Contains(term, "direct"):
		return ColorLevelTrueColor
	case strings.Contains(term, "256color") || strings.Contains(term, "256"):
		return ColorLevelAnsi256
	default:
		return ColorLevelAnsi16
	}
}

// Profile maps the detected color level onto a termenv color profile, used when
// rendering lipgloss styles so emitted SGR sequences match the terminal's
// capabilities.
func (c Capabilities) Profile() termenv.Profile {
	if c.NoColor {
		return termenv.Ascii
	}
	switch c.ColorLevel {
	case ColorLevelTrueColor:
		return termenv.TrueColor
	case ColorLevelAnsi256:
		return termenv.ANSI256
	case ColorLevelAnsi16:
		return termenv.ANSI
	default:
		return termenv.Ascii
	}
}

// SupportsColor reports whether any color can be emitted.
func (c Capabilities) SupportsColor() bool {
	return !c.NoColor && c.ColorLevel != ColorLevelUnknown
}

// StringWidth returns the display width of s in terminal cells, honoring the
// detected ambiguous-width policy so width math agrees with rendering.
//
// Mirrors the role of mattn/go-runewidth in the Rust port (which used
// unicode-width); callers that previously relied on Rust's UnicodeWidthStr
// should route through here.
func (c Capabilities) StringWidth(s string) int {
	if c.UnicodeWidth {
		cond := runewidth.NewCondition()
		cond.EastAsianWidth = true
		return cond.StringWidth(s)
	}
	return runewidth.StringWidth(s)
}
