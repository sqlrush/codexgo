package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// tabReplacement is the fixed substitution applied to tab characters before
// parsing, matching the Rust `expand_tabs` (each tab -> four spaces). This
// avoids gutter collisions in transcript rendering without tab-stop math.
const tabReplacement = "    "

// AnsiEscapeLine parses a single line of text that may contain ANSI SGR escape
// sequences into a styled [Line], preserving foreground/background color, bold,
// dim, italic, underline, reverse, and strikethrough.
//
// It is the Go analogue of codex-ansi-escape's `ansi_escape_line`. Tabs are
// normalized to spaces first. If the input spans multiple lines, only the first
// is returned (the trailing lines are dropped, matching the Rust warn-and-take-
// first behavior).
//
// Port of codex-rs/ansi-escape/src/lib.rs `ansi_escape_line`.
func AnsiEscapeLine(s string) Line {
	lines := AnsiEscape(s)
	switch len(lines) {
	case 0:
		return Line{}
	default:
		return lines[0]
	}
}

// AnsiEscape parses text containing ANSI SGR escape sequences and newlines into
// one styled [Line] per physical line. Unsupported escape sequences (cursor
// movement, OSC, etc.) are consumed and discarded rather than rendered.
//
// Port of codex-rs/ansi-escape/src/lib.rs `ansi_escape`.
func AnsiEscape(s string) []Line {
	s = strings.ReplaceAll(s, "\t", tabReplacement)

	var lines []Line
	var spans []Span
	var cur strings.Builder
	style := Style{}

	flushSpan := func() {
		if cur.Len() == 0 {
			return
		}
		spans = append(spans, StyledSpan(cur.String(), style))
		cur.Reset()
	}
	flushLine := func() {
		flushSpan()
		lines = append(lines, Line{Spans: spans})
		spans = nil
	}

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\n':
			flushLine()
		case r == '\r':
			// Carriage return: ignore for line-oriented rendering.
		case r == 0x1b: // ESC
			consumed, newStyle, isSGR := parseEscape(runes, i, style)
			if consumed > 0 {
				if isSGR {
					flushSpan()
					style = newStyle
				}
				i += consumed - 1 // -1 because the loop will i++
				continue
			}
			// Lone ESC with no recognizable sequence: drop it.
		default:
			cur.WriteRune(r)
		}
	}
	// Final line (only if we have buffered content or any spans, or no lines yet).
	if cur.Len() > 0 || len(spans) > 0 || len(lines) == 0 {
		flushLine()
	}
	return lines
}

// parseEscape attempts to parse an ANSI escape sequence starting at runes[i]
// (which must be ESC). It returns the number of runes consumed (0 if not a
// recognized sequence), the resulting style if it was an SGR sequence, and
// whether it was an SGR sequence (m terminator in a CSI).
func parseEscape(runes []rune, i int, style Style) (consumed int, newStyle Style, isSGR bool) {
	if i+1 >= len(runes) {
		return 0, style, false
	}
	switch runes[i+1] {
	case '[':
		return parseCSI(runes, i, style)
	case ']':
		return parseOSC(runes, i), style, false
	default:
		// Two-char escape (e.g. ESC c, ESC =). Consume both, no style change.
		return 2, style, false
	}
}

// parseCSI parses a CSI sequence (ESC [ ... final). Only SGR (final 'm') alters
// style; other CSI sequences are consumed and ignored.
func parseCSI(runes []rune, i int, style Style) (consumed int, newStyle Style, isSGR bool) {
	j := i + 2
	start := j
	for j < len(runes) {
		r := runes[j]
		if r >= 0x40 && r <= 0x7e { // final byte
			params := string(runes[start:j])
			if r == 'm' {
				return j - i + 1, applySGR(style, params), true
			}
			return j - i + 1, style, false
		}
		j++
	}
	// Unterminated CSI: consume the rest.
	return len(runes) - i, style, false
}

// parseOSC parses an OSC sequence (ESC ] ... terminated by BEL or ESC \), and
// returns the runes consumed. OSC sequences never alter SGR style.
func parseOSC(runes []rune, i int) int {
	j := i + 2
	for j < len(runes) {
		if runes[j] == 0x07 { // BEL terminator
			return j - i + 1
		}
		if runes[j] == 0x1b && j+1 < len(runes) && runes[j+1] == '\\' { // ST
			return j - i + 2
		}
		j++
	}
	return len(runes) - i
}

// applySGR applies a CSI SGR parameter string to a style, returning the new
// style. An empty parameter string is treated as "0" (reset).
func applySGR(style Style, params string) Style {
	if params == "" {
		return Style{}
	}
	codes := strings.Split(params, ";")
	for idx := 0; idx < len(codes); idx++ {
		n, err := strconv.Atoi(codes[idx])
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			style = Style{}
		case n == 1:
			style.Bold = true
		case n == 2:
			style.Dim = true
		case n == 3:
			style.Italic = true
		case n == 4:
			style.Underline = true
		case n == 7:
			style.Reverse = true
		case n == 9:
			style.Strikethrough = true
		case n == 21 || n == 22:
			style.Bold = false
			style.Dim = false
		case n == 23:
			style.Italic = false
		case n == 24:
			style.Underline = false
		case n == 27:
			style.Reverse = false
		case n == 29:
			style.Strikethrough = false
		case n >= 30 && n <= 37:
			style.Fg = lipgloss.Color(itoa(n - 30))
		case n == 38:
			c, adv := parseExtendedColor(codes, idx)
			if c != nil {
				style.Fg = c
			}
			idx += adv
		case n == 39:
			style.Fg = nil
		case n >= 40 && n <= 47:
			style.Bg = lipgloss.Color(itoa(n - 40))
		case n == 48:
			c, adv := parseExtendedColor(codes, idx)
			if c != nil {
				style.Bg = c
			}
			idx += adv
		case n == 49:
			style.Bg = nil
		case n >= 90 && n <= 97:
			style.Fg = lipgloss.Color(itoa(n - 90 + 8))
		case n >= 100 && n <= 107:
			style.Bg = lipgloss.Color(itoa(n - 100 + 8))
		}
	}
	return style
}

// parseExtendedColor parses a 38/48 extended color spec starting at codes[idx]
// (the "38"/"48"). It returns the parsed color (or nil) and the number of extra
// codes consumed beyond idx.
//
//	5;n        -> 256-color index n
//	2;r;g;b    -> truecolor
func parseExtendedColor(codes []string, idx int) (lipgloss.TerminalColor, int) {
	if idx+1 >= len(codes) {
		return nil, 0
	}
	mode := codes[idx+1]
	switch mode {
	case "5":
		if idx+2 >= len(codes) {
			return nil, 1
		}
		n, err := strconv.Atoi(codes[idx+2])
		if err != nil || n < 0 || n > 255 {
			return nil, 2
		}
		return lipgloss.Color(itoa(n)), 2
	case "2":
		if idx+4 >= len(codes) {
			return nil, len(codes) - idx - 1
		}
		r, e1 := strconv.Atoi(codes[idx+2])
		g, e2 := strconv.Atoi(codes[idx+3])
		b, e3 := strconv.Atoi(codes[idx+4])
		if e1 != nil || e2 != nil || e3 != nil {
			return nil, 4
		}
		return RGBColor(RGB{R: clampByte(r), G: clampByte(g), B: clampByte(b)}), 4
	default:
		return nil, 1
	}
}

// clampByte clamps an int to the 0..=255 byte range.
func clampByte(n int) uint8 {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return uint8(n)
}
