package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
)

// Highlighter turns fenced code-block source into styled [Line]s using chroma,
// the Go replacement for the Rust TUI's syntect-based render/highlight.rs.
//
// Highlighting is best-effort: when the language is unknown or the input is
// pathologically large, the source is returned as plain (unstyled) lines so the
// transcript never blocks or panics. This mirrors the Rust guardrails that fall
// back to plain text on oversized inputs.
type Highlighter struct {
	// theme maps chroma token categories to colors. It is derived from the
	// resolved [Theme] so highlighting respects the user's theme/capabilities.
	theme highlightTheme
}

// highlightLimits bound the work the highlighter will do, matching the Rust
// guardrails (512 KB / 10 000 lines).
const (
	highlightMaxBytes = 512 * 1024
	highlightMaxLines = 10_000
)

// highlightTheme is the small palette the token mapper draws from.
type highlightTheme struct {
	keyword  lipgloss.TerminalColor
	str      lipgloss.TerminalColor
	number   lipgloss.TerminalColor
	comment  lipgloss.TerminalColor
	function lipgloss.TerminalColor
	typ      lipgloss.TerminalColor
	builtin  lipgloss.TerminalColor
	operator lipgloss.TerminalColor
	deflt    lipgloss.TerminalColor
}

// NewHighlighter builds a highlighter whose colors are derived from the theme.
func NewHighlighter(theme Theme) Highlighter {
	return Highlighter{theme: highlightTheme{
		keyword:  theme.Primary,
		str:      theme.Success,
		number:   theme.Warning,
		comment:  theme.Dim,
		function: theme.Info,
		typ:      theme.Info,
		builtin:  theme.Primary,
		operator: theme.Foreground,
		deflt:    theme.Foreground,
	}}
}

// Highlight tokenizes source in the given language (a fenced-code-block info
// string, e.g. "go" or "python") and returns one styled [Line] per source line.
// An empty or unrecognized language falls back to chroma's analyser; failures
// fall back to plain text.
func (h Highlighter) Highlight(source, language string) []Line {
	if len(source) > highlightMaxBytes {
		return plainCodeLines(source)
	}
	lexer := h.lexerFor(source, language)
	if lexer == nil {
		return plainCodeLines(source)
	}
	lexer = chroma.Coalesce(lexer)
	iter, err := lexer.Tokenise(nil, source)
	if err != nil {
		return plainCodeLines(source)
	}
	tokens := iter.Tokens()

	var lines []Line
	var cur []Span
	emit := func() {
		lines = append(lines, Line{Spans: cur})
		cur = nil
		if len(lines) > highlightMaxLines {
			cur = nil
		}
	}
	for _, t := range tokens {
		style := h.styleFor(t.Type)
		// A token's value may contain embedded newlines; split so each line is a
		// separate [Line].
		parts := strings.Split(t.Value, "\n")
		for i, p := range parts {
			if p != "" {
				cur = append(cur, Span{Text: p, Style: style})
			}
			if i < len(parts)-1 {
				emit()
			}
		}
	}
	if len(cur) > 0 {
		lines = append(lines, Line{Spans: cur})
	}
	// chroma's tokenizer keeps a trailing newline as a final empty line; drop it
	// so the rendered block height matches the source line count.
	if n := len(lines); n > 0 && len(lines[n-1].Spans) == 0 {
		lines = lines[:n-1]
	}
	if len(lines) == 0 {
		return plainCodeLines(source)
	}
	return lines
}

// lexerFor resolves a chroma lexer from a language hint, falling back to content
// analysis and then to nil (caller renders plain).
func (h Highlighter) lexerFor(source, language string) chroma.Lexer {
	language = strings.TrimSpace(strings.ToLower(language))
	if language != "" {
		if l := lexers.Get(language); l != nil {
			return l
		}
	}
	if l := lexers.Analyse(source); l != nil {
		return l
	}
	return nil
}

// styleFor maps a chroma token type to a foundation [Style].
func (h Highlighter) styleFor(t chroma.TokenType) Style {
	cat := t.Category()
	sub := t.SubCategory()
	switch {
	case t == chroma.Comment || cat == chroma.Comment:
		return Style{Fg: h.theme.comment, Italic: true}
	case cat == chroma.Keyword:
		return Style{Fg: h.theme.keyword, Bold: true}
	case sub == chroma.LiteralString || cat == chroma.LiteralString:
		return Style{Fg: h.theme.str}
	case sub == chroma.LiteralNumber || cat == chroma.LiteralNumber:
		return Style{Fg: h.theme.number}
	case t == chroma.NameFunction || sub == chroma.NameFunction:
		return Style{Fg: h.theme.function}
	case t == chroma.NameClass || t == chroma.NameNamespace || t == chroma.KeywordType:
		return Style{Fg: h.theme.typ}
	case t == chroma.NameBuiltin || sub == chroma.NameBuiltin:
		return Style{Fg: h.theme.builtin}
	case cat == chroma.Operator:
		return Style{Fg: h.theme.operator}
	default:
		return Style{Fg: h.theme.deflt}
	}
}

// plainCodeLines splits source into unstyled lines.
func plainCodeLines(source string) []Line {
	src := strings.TrimSuffix(source, "\n")
	parts := strings.Split(src, "\n")
	out := make([]Line, len(parts))
	for i, p := range parts {
		if p == "" {
			out[i] = Line{}
			continue
		}
		out[i] = Line{Spans: []Span{{Text: p}}}
	}
	return out
}
