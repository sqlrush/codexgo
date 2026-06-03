package termdetect

import "strings"

// infoFromTermProgram builds terminal metadata from a TERM_PROGRAM match.
func infoFromTermProgram(name TerminalName, termProgram, version string, mux Multiplexer) TerminalInfo {
	return TerminalInfo{
		Name:        name,
		TermProgram: termProgram,
		Version:     version,
		Multiplexer: mux,
	}
}

// infoFromTermProgramAndTerm builds terminal metadata from a TERM_PROGRAM match
// plus a TERM capability value.
func infoFromTermProgramAndTerm(name TerminalName, termProgram, version, term string, mux Multiplexer) TerminalInfo {
	return TerminalInfo{
		Name:        name,
		TermProgram: termProgram,
		Version:     version,
		Term:        term,
		Multiplexer: mux,
	}
}

// infoFromName builds terminal metadata from a known terminal name and optional
// version.
func infoFromName(name TerminalName, version string, mux Multiplexer) TerminalInfo {
	return TerminalInfo{
		Name:        name,
		Version:     version,
		Multiplexer: mux,
	}
}

// infoFromTerm builds terminal metadata from a TERM capability value, mapping a
// few well-known TERM values to concrete terminal names.
func infoFromTerm(term string, mux Multiplexer) TerminalInfo {
	name := TerminalUnknown
	switch term {
	case "dumb":
		name = TerminalDumb
	case "wezterm", "wezterm-mux":
		name = TerminalWezTerm
	}
	return TerminalInfo{
		Name:        name,
		Term:        term,
		Multiplexer: mux,
	}
}

// userAgentToken formats the terminal info as a sanitized User-Agent token.
func (t TerminalInfo) userAgentToken() string {
	var raw string
	switch {
	case t.TermProgram != "":
		if t.Version != "" {
			raw = t.TermProgram + "/" + t.Version
		} else {
			raw = t.TermProgram
		}
	case t.Term != "":
		raw = t.Term
	default:
		raw = t.nameToken()
	}
	return sanitizeHeaderValue(raw)
}

// nameToken returns the User-Agent token for the detected terminal name,
// embedding the version for terminals that report one.
func (t TerminalInfo) nameToken() string {
	switch t.Name {
	case TerminalAppleTerminal:
		return formatTerminalVersion("Apple_Terminal", t.Version)
	case TerminalGhostty:
		return formatTerminalVersion("Ghostty", t.Version)
	case TerminalIterm2:
		return formatTerminalVersion("iTerm.app", t.Version)
	case TerminalWarpTerminal:
		return formatTerminalVersion("WarpTerminal", t.Version)
	case TerminalVsCode:
		return formatTerminalVersion("vscode", t.Version)
	case TerminalWezTerm:
		return formatTerminalVersion("WezTerm", t.Version)
	case TerminalKitty:
		return "kitty"
	case TerminalAlacritty:
		return "Alacritty"
	case TerminalKonsole:
		return formatTerminalVersion("Konsole", t.Version)
	case TerminalGnomeTerminal:
		return "gnome-terminal"
	case TerminalVte:
		return formatTerminalVersion("VTE", t.Version)
	case TerminalWindowsTerminal:
		return "WindowsTerminal"
	case TerminalDumb:
		return "dumb"
	default:
		return "unknown"
	}
}

// formatTerminalVersion joins a terminal name and version with "/", omitting the
// version when it is empty.
func formatTerminalVersion(name, version string) string {
	if version != "" {
		return name + "/" + version
	}
	return name
}

// sanitizeHeaderValue replaces every character that is not valid in a
// User-Agent header value with an underscore.
func sanitizeHeaderValue(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, c := range value {
		if isValidHeaderValueChar(c) {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// isValidHeaderValueChar reports whether a character is allowed unmodified in a
// User-Agent header value (ASCII alphanumerics plus "-", "_", ".", and "/").
func isValidHeaderValueChar(c rune) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c == '-' || c == '_' || c == '.' || c == '/':
		return true
	default:
		return false
	}
}
