// Package termdetect detects the active terminal program, multiplexer, and
// version from environment variables (TERM, TERM_PROGRAM,
// TERM_PROGRAM_VERSION, TMUX, ZELLIJ_VERSION, etc.).
//
// It is a faithful Go port of codex's `codex-terminal-detection` crate. The
// detected metadata feeds OpenTelemetry user-agent logging and
// terminal-specific configuration choices in the TUI.
//
// All values are reported as Go strings; an absent value is represented by the
// empty string (the Rust original uses Option<String>, which collapses cleanly
// onto Go's empty-string convention here because the crate never distinguishes
// Some("") from None — empty values are normalized away by none_if_whitespace).
package termdetect

// TerminalName is a known terminal name category derived from environment
// variables.
type TerminalName int

// Terminal name categories. The ordering matches the Rust enum declaration
// order but the concrete values are an implementation detail; compare against
// the named constants rather than the integers.
const (
	// TerminalUnknown is an unknown or missing terminal identification.
	TerminalUnknown TerminalName = iota
	// TerminalAppleTerminal is Apple Terminal (Terminal.app).
	TerminalAppleTerminal
	// TerminalGhostty is the Ghostty terminal emulator.
	TerminalGhostty
	// TerminalIterm2 is the iTerm2 terminal emulator.
	TerminalIterm2
	// TerminalWarpTerminal is the Warp terminal emulator.
	TerminalWarpTerminal
	// TerminalVsCode is the Visual Studio Code integrated terminal.
	TerminalVsCode
	// TerminalWezTerm is the WezTerm terminal emulator.
	TerminalWezTerm
	// TerminalKitty is the kitty terminal emulator.
	TerminalKitty
	// TerminalAlacritty is the Alacritty terminal emulator.
	TerminalAlacritty
	// TerminalKonsole is the KDE Konsole terminal emulator.
	TerminalKonsole
	// TerminalGnomeTerminal is the GNOME Terminal emulator.
	TerminalGnomeTerminal
	// TerminalVte is a VTE backend terminal.
	TerminalVte
	// TerminalWindowsTerminal is the Windows Terminal emulator.
	TerminalWindowsTerminal
	// TerminalDumb is a dumb terminal (TERM=dumb).
	TerminalDumb
)

// MultiplexerKind identifies a detected terminal multiplexer.
type MultiplexerKind int

const (
	// MultiplexerNone indicates no multiplexer was detected. It is the zero
	// value so that a Multiplexer{} reports "no multiplexer".
	MultiplexerNone MultiplexerKind = iota
	// MultiplexerTmux is the tmux terminal multiplexer.
	MultiplexerTmux
	// MultiplexerZellij is the zellij terminal multiplexer.
	MultiplexerZellij
)

// Multiplexer is detected terminal multiplexer metadata.
//
// In the Rust original this is an enum carrying a version field per variant.
// Here it is a small struct: Kind selects the variant and Version holds the
// associated version string (empty when unknown). The Present helper reports
// whether any multiplexer was detected, mirroring Rust's Option<Multiplexer>.
type Multiplexer struct {
	// Kind is the detected multiplexer, or MultiplexerNone when absent.
	Kind MultiplexerKind
	// Version is the multiplexer version string, or "" when unavailable.
	//
	// For tmux this is derived from TERM_PROGRAM_VERSION when
	// TERM_PROGRAM=tmux. For zellij it comes from ZELLIJ_VERSION or, as a
	// best-effort fallback, the `zellij --version` command.
	Version string
}

// Present reports whether a multiplexer was detected.
func (m Multiplexer) Present() bool {
	return m.Kind != MultiplexerNone
}

// TerminalInfo is structured terminal identification data.
//
// Optional string fields use "" to mean absent, matching the Rust crate's
// behavior of normalizing empty/whitespace values to None.
type TerminalInfo struct {
	// Name is the detected terminal name category.
	Name TerminalName
	// TermProgram is the TERM_PROGRAM value when provided by the terminal,
	// or "" when not applicable.
	TermProgram string
	// Version is the terminal version string when available, or "".
	Version string
	// Term is the TERM value when falling back to capability strings, or "".
	Term string
	// Multiplexer holds multiplexer metadata. Use Multiplexer.Present to
	// check whether a multiplexer is active.
	Multiplexer Multiplexer
}

// IsZellij reports whether the active terminal multiplexer is Zellij.
func (t TerminalInfo) IsZellij() bool {
	return t.Multiplexer.Kind == MultiplexerZellij
}
