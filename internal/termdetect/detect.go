package termdetect

import "strings"

// Detect returns structured terminal metadata for the current process.
//
// Unlike the Rust crate, the result is not memoized: each call re-reads the
// environment. Callers that want a cached value should cache it themselves.
func Detect() TerminalInfo {
	return detectFromEnv(processEnvironment{})
}

// UserAgent returns a sanitized terminal identifier suitable for use as a
// User-Agent token for the current process.
func UserAgent() string {
	return Detect().userAgentToken()
}

// detectFromEnv detects structured terminal metadata from an injectable
// environment.
//
// Detection order favors explicit identifiers before falling back to capability
// strings:
//   - If TERM_PROGRAM=tmux, the tmux client term type/name are used instead.
//     The client term type is split on whitespace to extract a program name
//     plus optional version (for example, "ghostty 1.2.3"), while the client
//     term name becomes the TERM capability string.
//   - Otherwise, TERM_PROGRAM (plus TERM_PROGRAM_VERSION) drives the detected
//     terminal name. This means TERM_PROGRAM can mask later probes (for example
//     WT_SESSION).
//   - Next, terminal-specific variables (WEZTERM, iTerm2, Apple Terminal, kitty,
//     etc.) are checked.
//   - Finally, TERM is used as the capability fallback with TerminalUnknown.
//
// tmux client term info is only consulted when a tmux multiplexer is detected,
// and it is derived from `tmux display-message` to surface the underlying
// terminal program instead of reporting tmux itself.
func detectFromEnv(env environment) TerminalInfo {
	mux := detectMultiplexer(env)

	if termProgram := varNonEmpty(env, "TERM_PROGRAM"); termProgram != "" {
		if isTmuxTermProgram(termProgram) && mux.Kind == MultiplexerTmux {
			if terminal, ok := terminalFromTmuxClientInfo(env.tmuxClientInfo(), mux); ok {
				return terminal
			}
		}

		version := varNonEmpty(env, "TERM_PROGRAM_VERSION")
		name := terminalNameFromTermProgram(termProgram)
		return infoFromTermProgram(name, termProgram, version, mux)
	}

	if has(env, "WEZTERM_VERSION") {
		version := varNonEmpty(env, "WEZTERM_VERSION")
		return infoFromName(TerminalWezTerm, version, mux)
	}

	if has(env, "ITERM_SESSION_ID") || has(env, "ITERM_PROFILE") || has(env, "ITERM_PROFILE_NAME") {
		return infoFromName(TerminalIterm2, "", mux)
	}

	if has(env, "TERM_SESSION_ID") {
		return infoFromName(TerminalAppleTerminal, "", mux)
	}

	if has(env, "KITTY_WINDOW_ID") || strings.Contains(varValue(env, "TERM"), "kitty") {
		return infoFromName(TerminalKitty, "", mux)
	}

	if has(env, "ALACRITTY_SOCKET") || varValue(env, "TERM") == "alacritty" {
		return infoFromName(TerminalAlacritty, "", mux)
	}

	if has(env, "KONSOLE_VERSION") {
		version := varNonEmpty(env, "KONSOLE_VERSION")
		return infoFromName(TerminalKonsole, version, mux)
	}

	if has(env, "GNOME_TERMINAL_SCREEN") {
		return infoFromName(TerminalGnomeTerminal, "", mux)
	}

	if has(env, "VTE_VERSION") {
		version := varNonEmpty(env, "VTE_VERSION")
		return infoFromName(TerminalVte, version, mux)
	}

	if has(env, "WT_SESSION") {
		return infoFromName(TerminalWindowsTerminal, "", mux)
	}

	if term := varNonEmpty(env, "TERM"); term != "" {
		return infoFromTerm(term, mux)
	}

	return TerminalInfo{Name: TerminalUnknown, Multiplexer: mux}
}

// detectMultiplexer detects an active terminal multiplexer from the
// environment, returning a zero-value Multiplexer (MultiplexerNone) when none
// is present.
func detectMultiplexer(env environment) Multiplexer {
	if hasNonEmpty(env, "TMUX") || hasNonEmpty(env, "TMUX_PANE") {
		return Multiplexer{Kind: MultiplexerTmux, Version: tmuxVersionFromEnv(env)}
	}

	if hasNonEmpty(env, "ZELLIJ") ||
		hasNonEmpty(env, "ZELLIJ_SESSION_NAME") ||
		hasNonEmpty(env, "ZELLIJ_VERSION") {
		return Multiplexer{Kind: MultiplexerZellij, Version: env.zellijVersion()}
	}

	return Multiplexer{Kind: MultiplexerNone}
}

// isTmuxTermProgram reports whether a TERM_PROGRAM value identifies tmux,
// ignoring ASCII case.
func isTmuxTermProgram(value string) bool {
	return strings.EqualFold(value, "tmux")
}

// terminalFromTmuxClientInfo derives terminal metadata from tmux client info.
//
// It returns ok=false when neither the client term type nor term name is
// available, matching the Rust function's Option<TerminalInfo> result.
func terminalFromTmuxClientInfo(client tmuxClientInfo, mux Multiplexer) (TerminalInfo, bool) {
	termtype := noneIfWhitespace(client.termtype)
	termname := noneIfWhitespace(client.termname)

	if termtype != "" {
		program, version := splitTermProgramAndVersion(termtype)
		name := terminalNameFromTermProgram(program)
		return infoFromTermProgramAndTerm(name, program, version, termname, mux), true
	}

	if termname != "" {
		return infoFromTerm(termname, mux), true
	}

	return TerminalInfo{}, false
}

// tmuxVersionFromEnv extracts the tmux version from TERM_PROGRAM_VERSION, but
// only when TERM_PROGRAM identifies tmux.
func tmuxVersionFromEnv(env environment) string {
	termProgram, ok := env.lookup("TERM_PROGRAM")
	if !ok || !isTmuxTermProgram(termProgram) {
		return ""
	}
	return varNonEmpty(env, "TERM_PROGRAM_VERSION")
}

// splitTermProgramAndVersion splits a tmux client term-type string into a
// program name and optional version on the first run of whitespace (for example
// "ghostty 1.2.3" -> "ghostty", "1.2.3"). The version is "" when absent.
func splitTermProgramAndVersion(value string) (program, version string) {
	parts := strings.Fields(value)
	if len(parts) > 0 {
		program = parts[0]
	}
	if len(parts) > 1 {
		version = parts[1]
	}
	return program, version
}

// terminalNameFromTermProgram maps a TERM_PROGRAM value to a known terminal
// name, returning TerminalUnknown when unrecognized.
//
// The value is normalized by trimming, dropping spaces/dashes/underscores/dots,
// and lowercasing before matching, so "Apple_Terminal", "gnome-terminal", and
// "iTerm.app" all match their canonical forms.
func terminalNameFromTermProgram(value string) TerminalName {
	var b strings.Builder
	for _, c := range strings.TrimSpace(value) {
		switch c {
		case ' ', '-', '_', '.':
			continue
		}
		b.WriteRune(toASCIILower(c))
	}

	switch b.String() {
	case "appleterminal":
		return TerminalAppleTerminal
	case "ghostty":
		return TerminalGhostty
	case "iterm", "iterm2", "itermapp":
		return TerminalIterm2
	case "warp", "warpterminal":
		return TerminalWarpTerminal
	case "vscode":
		return TerminalVsCode
	case "wezterm":
		return TerminalWezTerm
	case "kitty":
		return TerminalKitty
	case "alacritty":
		return TerminalAlacritty
	case "konsole":
		return TerminalKonsole
	case "gnometerminal":
		return TerminalGnomeTerminal
	case "vte":
		return TerminalVte
	case "windowsterminal":
		return TerminalWindowsTerminal
	case "dumb":
		return TerminalDumb
	default:
		return TerminalUnknown
	}
}

// toASCIILower lowercases an ASCII letter and leaves all other runes
// unchanged, matching Rust's char::to_ascii_lowercase.
func toASCIILower(c rune) rune {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
