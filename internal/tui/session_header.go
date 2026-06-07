package tui

import (
	"fmt"
	"strings"
)

// SessionHeaderCell renders the bordered welcome/session-header card shown at the
// top of a fresh session. It is the Go analogue of
// SessionHeaderHistoryCell::display_lines + the with_border / card_inner_width
// helpers in codex-rs/tui/src/history_cell/session.rs.
//
// The card layout (characters) is reproduced byte-for-byte against codex 0.136.0:
//
//	╭──────────────────────────────────────────────────────────╮
//	│ >_ OpenAI Codex (vX.Y.Z)                                   │
//	│                                                            │
//	│ model:     <model>   /model to change                      │
//	│ directory: <dir>                                           │
//	╰──────────────────────────────────────────────────────────╯
//
// The inner width is min(width-4, 56) clamped to the widest content row, matching
// SESSION_HEADER_MAX_INNER_WIDTH. The "model:" and "directory:" labels are left
// padded to the width of "directory:" (10) and each gets one trailing space, then
// the value. SGR styling is ported cell-for-cell from session.rs: the border,
// the ">_ " prefix, the " " gap, the "(v…)" version, and the labels are all DIM
// (SGR 2); "OpenAI Codex" is BOLD; "/model" is ratatui's NAMED cyan (SGR 36, via
// [ANSICyan]); the model value and directory value are unstyled. The character
// grid is identical regardless of styling.
//
// Wave-1 scope: reasoning effort, fast-status, and YOLO-mode rows are omitted
// (they require state codexgo's headless TUI host does not surface at startup);
// the directory value is rendered without center-truncation parity (the harness
// masks the directory row because it is a volatile per-run path).
type SessionHeaderCell struct {
	theme     Theme
	version   string
	model     string
	directory string
}

// sessionHeaderMaxInnerWidth mirrors SESSION_HEADER_MAX_INNER_WIDTH.
const sessionHeaderMaxInnerWidth = 56

// NewSessionHeaderCell builds the session-header card cell.
func NewSessionHeaderCell(theme Theme, version, model, directory string) SessionHeaderCell {
	return SessionHeaderCell{theme: theme, version: version, model: model, directory: directory}
}

// cardInnerWidth ports card_inner_width: min(width-4, max) for width >= 4.
func cardInnerWidth(width, maxInner int) (int, bool) {
	if width < 4 {
		return 0, false
	}
	inner := width - 4
	if inner > maxInner {
		inner = maxInner
	}
	return inner, true
}

// Lines implements HistoryCell, rendering the bordered card for the given width.
func (c SessionHeaderCell) Lines(width int) []Line {
	innerWidth, ok := cardInnerWidth(width, sessionHeaderMaxInnerWidth)
	if !ok {
		return nil
	}

	dim := Style{Dim: true}
	bold := Style{Bold: true}
	// codex styles the "/model" hint with ratatui's NAMED `Color::Cyan`
	// (session.rs: CHANGE_MODEL_HINT_COMMAND.cyan()), which renders as SGR 36 at
	// every color depth — not the theme's info accent. Use the named ANSI cyan so
	// the cell attribute matches byte-for-byte.
	cyan := Style{Fg: ANSICyan}

	// Content rows (without border chrome), as span lists.
	//
	// Branding deviation: the welcome card shows codexgo's own product name and
	// version (the version comes from cli.Version via RunConfig.Version), NOT the
	// upstream "OpenAI Codex (v0.136.0)" the parity frames assert. The title row
	// is therefore masked in the parity frame tests (see frameMask ">_ ").
	title := []Span{
		{Text: ">_ ", Style: dim},
		{Text: "CodexGO", Style: bold},
		{Text: " ", Style: dim},
		{Text: fmt.Sprintf("(v%s)", c.version), Style: dim},
	}

	const dirLabel = "directory:"
	labelWidth := len(dirLabel)

	modelLabel := padLabel("model:", labelWidth)
	modelRow := []Span{
		{Text: modelLabel + " ", Style: dim},
		{Text: c.model},
		{Text: "   ", Style: dim},
		{Text: "/model", Style: cyan},
		{Text: " to change", Style: dim},
	}

	dirPrefix := padLabel(dirLabel, labelWidth) + " "
	dirMaxWidth := innerWidth - uwidth(dirPrefix)
	dirRow := []Span{
		{Text: dirPrefix, Style: dim},
		{Text: centerTruncatePath(c.directory, dirMaxWidth)},
	}

	content := [][]Span{title, {}, modelRow, dirRow}

	// Like codex's with_border (forced_inner_width = None): the border is sized to
	// the widest content row, NOT to innerWidth. innerWidth is only used above to
	// bound the directory truncation.
	return withBorder(content, dim)
}

// padLabel left-justifies label to at least width characters (space padded),
// mirroring Rust's "{:<width$}" formatting.
func padLabel(label string, width int) string {
	if len(label) >= width {
		return label
	}
	return label + strings.Repeat(" ", width-len(label))
}

// withBorder wraps content rows in a dim border sized to the widest content row.
// It ports with_border_internal (forced_inner_width = None): each content row
// becomes "│ " + spans + right-padding + " │"; the top/bottom borders use
// box-drawing characters spanning contentWidth+2.
func withBorder(content [][]Span, dim Style) []Line {
	contentWidth := 0
	for _, row := range content {
		if w := spansWidth(row); w > contentWidth {
			contentWidth = w
		}
	}

	borderInner := contentWidth + 2
	top := Line{Spans: []Span{{Text: "╭" + strings.Repeat("─", borderInner) + "╮", Style: dim}}}
	bottom := Line{Spans: []Span{{Text: "╰" + strings.Repeat("─", borderInner) + "╯", Style: dim}}}

	out := make([]Line, 0, len(content)+2)
	out = append(out, top)
	for _, row := range content {
		used := spansWidth(row)
		spans := make([]Span, 0, len(row)+3)
		spans = append(spans, Span{Text: "│ ", Style: dim})
		spans = append(spans, row...)
		if used < contentWidth {
			spans = append(spans, Span{Text: strings.Repeat(" ", contentWidth-used), Style: dim})
		}
		spans = append(spans, Span{Text: " │", Style: dim})
		out = append(out, Line{Spans: spans})
	}
	out = append(out, bottom)
	return out
}

// spansWidth returns the display width of a span list under the fixed
// unicode-width policy (matching codex's ratatui width math for the card).
func spansWidth(spans []Span) int {
	w := 0
	for _, s := range spans {
		w += uwidth(s.Text)
	}
	return w
}

// compile-time assertion.
var _ HistoryCell = SessionHeaderCell{}
