package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Style captures the SGR attributes a span can carry. It is the Go analogue of
// ratatui's `Style`, decoupled from any single rendering backend so the same
// span data can be turned into a lipgloss style or compared in tests.
//
// Style is a value type; helpers return modified copies (immutability).
type Style struct {
	// Fg is the foreground color, or nil for the terminal default.
	Fg lipgloss.TerminalColor
	// Bg is the background color, or nil for the terminal default.
	Bg lipgloss.TerminalColor
	Bold,
	Dim,
	Italic,
	Underline,
	Reverse,
	Strikethrough bool
}

// WithFg returns a copy of s with the foreground color set.
func (s Style) WithFg(c lipgloss.TerminalColor) Style { s.Fg = c; return s }

// WithBg returns a copy of s with the background color set.
func (s Style) WithBg(c lipgloss.TerminalColor) Style { s.Bg = c; return s }

// WithBold returns a copy of s with bold set.
func (s Style) WithBold(v bool) Style { s.Bold = v; return s }

// Lip converts the style to a lipgloss.Style for rendering.
func (s Style) Lip() lipgloss.Style {
	out := lipgloss.NewStyle()
	if s.Fg != nil {
		out = out.Foreground(s.Fg)
	}
	if s.Bg != nil {
		out = out.Background(s.Bg)
	}
	if s.Bold {
		out = out.Bold(true)
	}
	if s.Dim {
		out = out.Faint(true)
	}
	if s.Italic {
		out = out.Italic(true)
	}
	if s.Underline {
		out = out.Underline(true)
	}
	if s.Reverse {
		out = out.Reverse(true)
	}
	if s.Strikethrough {
		out = out.Strikethrough(true)
	}
	return out
}

// ANSI named-color indices (0..15), the analogue of ratatui's named `Color`
// variants (e.g. `Color::Cyan`, `Color::Magenta`). Rendered via lipgloss they
// emit the classic 16-color SGR codes (30-37 / 90-97 for fg) regardless of the
// detected color depth — exactly like ratatui, which maps a named color to its
// SGR code rather than to an RGB or 256-indexed approximation. Using these for
// spans codex styles with a NAMED color (not a theme color) keeps the emitted
// SGR — and therefore the reconstructed cell attributes — byte-identical.
var (
	// ANSICyan is ratatui's `Color::Cyan` (SGR 36). codex uses it for the
	// session-header "/model" hint and default status-line model segment.
	ANSICyan lipgloss.TerminalColor = lipgloss.Color("6")
	// ANSIGreen is ratatui's `Color::Green` (SGR 32), used for status-line path
	// segments under the non-theme fallback styling.
	ANSIGreen lipgloss.TerminalColor = lipgloss.Color("2")
	// ANSIMagenta is ratatui's `Color::Magenta` (SGR 35).
	ANSIMagenta lipgloss.TerminalColor = lipgloss.Color("5")
)

// Span is a run of text sharing one style, the analogue of ratatui's `Span`.
type Span struct {
	Text  string
	Style Style
}

// PlainSpan builds an unstyled span.
func PlainSpan(text string) Span { return Span{Text: text} }

// StyledSpan builds a span with the supplied style.
func StyledSpan(text string, style Style) Span { return Span{Text: text, Style: style} }

// Line is an ordered list of styled spans rendered on one row, the analogue of
// ratatui's `Line`.
type Line struct {
	Spans []Span
}

// PlainLine builds a single unstyled span line.
func PlainLine(text string) Line {
	if text == "" {
		return Line{}
	}
	return Line{Spans: []Span{PlainSpan(text)}}
}

// String returns the unstyled concatenation of the line's spans.
func (l Line) String() string {
	var b strings.Builder
	for _, s := range l.Spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Render returns the line as a styled terminal string for the given profile.
// Each span is rendered with its own lipgloss style; an empty line renders as
// the empty string.
func (l Line) Render() string {
	if len(l.Spans) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range l.Spans {
		if s.Text == "" {
			continue
		}
		b.WriteString(s.Style.Lip().Render(s.Text))
	}
	return b.String()
}

// Width returns the display width of the line in cells under the supplied
// capabilities.
func (l Line) Width(caps Capabilities) int {
	return caps.StringWidth(l.String())
}
