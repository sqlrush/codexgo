package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/sqlrush/codexgo/internal/config"
)

// Theme is the resolved palette of lipgloss styles the TUI renders with. It is
// the Go analogue of the ad-hoc style constants scattered across the Rust tui
// crate, consolidated into one capability-aware layer.
//
// A Theme is immutable; derive variants with the With* helpers if needed.
type Theme struct {
	// Name identifies the theme (the value of config [tui].theme, or "default").
	Name string

	// Foundational colors.
	Foreground lipgloss.TerminalColor
	Background lipgloss.TerminalColor
	Dim        lipgloss.TerminalColor

	// Semantic accent colors.
	Primary lipgloss.TerminalColor
	Success lipgloss.TerminalColor
	Warning lipgloss.TerminalColor
	Error   lipgloss.TerminalColor
	Info    lipgloss.TerminalColor

	// Role styles used by the transcript and bottom pane.
	UserMessage      lipgloss.Style
	AgentMessage     lipgloss.Style
	ReasoningMessage lipgloss.Style
	SystemMessage    lipgloss.Style
	ErrorMessage     lipgloss.Style
	BorderStyle      lipgloss.Style
	ComposerPrompt   lipgloss.Style
	StatusLine       lipgloss.Style

	// ComposerSurfaceBg is the background painted across the composer block,
	// derived from the detected terminal background like codex's
	// user_message_style (style.rs). HasComposerSurface gates it: when the
	// terminal background is unknown the composer stays unstyled, matching
	// codex's default_bg() == None fallback.
	ComposerSurfaceBg  lipgloss.TerminalColor
	HasComposerSurface bool

	// ForegroundRGB is the palette foreground as raw RGB (the shimmer base).
	ForegroundRGB RGB
	// TerminalBgRGB is the detected terminal background (the shimmer
	// highlight target); HasTerminalBg gates it. Set by
	// WithTerminalBackground alongside the composer surface.
	TerminalBgRGB RGB
	HasTerminalBg bool
}

// WithTerminalBackground derives the composer surface color from the detected
// terminal background. ok=false (no tty / no OSC 11 reply) leaves the theme
// unchanged, keeping the unstyled fallback.
func (t Theme) WithTerminalBackground(bg RGB, ok bool) Theme {
	if !ok {
		return t
	}
	t.ComposerSurfaceBg = lipgloss.Color(userMessageBg(bg).Hex())
	t.HasComposerSurface = true
	t.TerminalBgRGB = bg
	t.HasTerminalBg = true
	return t
}

// builtinTheme is a named set of base colors a Theme is assembled from.
type builtinTheme struct {
	name       string
	foreground RGB
	background RGB
	dim        RGB
	primary    RGB
	success    RGB
	warning    RGB
	err        RGB
	info       RGB
}

// builtinThemes are the named palettes selectable via config [tui].theme.
// "default" follows the upstream dark palette; "light" inverts for light
// terminals. Unknown names fall back to "default".
var builtinThemes = map[string]builtinTheme{
	"default": {
		name:       "default",
		foreground: RGB{205, 214, 244},
		background: RGB{30, 30, 46},
		dim:        RGB{127, 132, 156},
		primary:    RGB{137, 180, 250},
		success:    RGB{166, 227, 161},
		warning:    RGB{249, 226, 175},
		err:        RGB{243, 139, 168},
		info:       RGB{148, 226, 213},
	},
	"light": {
		name:       "light",
		foreground: RGB{76, 79, 105},
		background: RGB{239, 241, 245},
		dim:        RGB{140, 143, 161},
		primary:    RGB{30, 102, 245},
		success:    RGB{64, 160, 43},
		warning:    RGB{223, 142, 29},
		err:        RGB{210, 15, 57},
		info:       RGB{23, 146, 153},
	},
}

// DefaultTheme returns the built-in default theme resolved against the supplied
// terminal capabilities.
func DefaultTheme(caps Capabilities) Theme {
	return buildTheme(builtinThemes["default"], caps)
}

// LoadTheme resolves a Theme from config [tui].theme against the supplied
// terminal capabilities. A nil Tui or absent/empty theme name yields the
// default theme; an unrecognized name also falls back to the default.
//
// This is the foundation hook for the config-driven theming the area agents
// extend (e.g. syntax theme previews via SyntaxThemeSelected events).
func LoadTheme(tui *config.Tui, caps Capabilities) Theme {
	name := "default"
	if tui != nil && tui.Theme != nil && *tui.Theme != "" {
		name = *tui.Theme
	}
	base, ok := builtinThemes[name]
	if !ok {
		base = builtinThemes["default"]
		base.name = name // remember the requested (unknown) name for diagnostics
	}
	return buildTheme(base, caps)
}

// buildTheme assembles a Theme from a base palette, mapping each color through
// [BestColor] so it degrades gracefully on lower color-depth terminals.
func buildTheme(base builtinTheme, caps Capabilities) Theme {
	level := caps.ColorLevel
	if caps.NoColor {
		level = ColorLevelUnknown
	}
	col := func(c RGB) lipgloss.TerminalColor { return BestColor(c, level) }

	fg := col(base.foreground)
	dim := col(base.dim)
	primary := col(base.primary)
	success := col(base.success)
	warning := col(base.warning)
	errc := col(base.err)
	info := col(base.info)

	base.name = orDefault(base.name, "default")

	return Theme{
		Name:          base.name,
		Foreground:    fg,
		ForegroundRGB: base.foreground,
		Background:    col(base.background),
		Dim:           dim,
		Primary:       primary,
		Success:       success,
		Warning:       warning,
		Error:         errc,
		Info:          info,

		UserMessage:      lipgloss.NewStyle().Foreground(fg).Bold(true),
		AgentMessage:     lipgloss.NewStyle().Foreground(fg),
		ReasoningMessage: lipgloss.NewStyle().Foreground(dim).Italic(true),
		SystemMessage:    lipgloss.NewStyle().Foreground(info),
		ErrorMessage:     lipgloss.NewStyle().Foreground(errc).Bold(true),
		BorderStyle:      lipgloss.NewStyle().Foreground(dim),
		// codex renders the composer prompt marker "›" with ratatui's `"›".bold()`
		// (chat_composer.rs) — BOLD only, no foreground color — so the cell carries
		// just the bold attribute. The gutter space after it is unstyled.
		ComposerPrompt: lipgloss.NewStyle().Bold(true),
		StatusLine:     lipgloss.NewStyle().Foreground(dim),
	}
}

// orDefault returns v, or fallback when v is empty.
func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
