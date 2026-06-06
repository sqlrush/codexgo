package tui

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// HistoryCell is the unit of display in the transcript: one committed (or
// in-flight, streaming) conversation entry. It is the Go analogue of the Rust
// `HistoryCell` trait in history_cell/mod.rs.
//
// A cell renders itself to styled [Line]s for a given content width. The width
// parameter lets cells reflow on resize. Cells are values rendered through a
// small interface; concrete cell types live in this file.
type HistoryCell interface {
	// Lines renders the cell to styled lines for the given content width. A
	// width <= 0 means "no wrapping".
	Lines(width int) []Line
}

// --- User message cell ------------------------------------------------------

// UserCell renders a user message echoed into history on submit. User text is
// shown verbatim (not parsed as markdown), wrapped, then prefixed with a "› "
// marker on the first line and a two-space indent on continuation lines, with a
// blank line before and after the message block.
//
// This is the Go analogue of UserHistoryCell::display_lines in codex-rs/tui/src/
// history_cell/messages.rs: a leading blank line, the wrapped message body run
// through prefix_lines("› " bold+dim, "  "), then a trailing blank line. The
// body itself carries no style (user_message_style() resolves to the terminal
// default when no terminal background is known), so only the marker glyph and
// its trailing gutter space are bold+dim — matching codex's captured cell
// attributes (marker "[bold,dim]", body "[·]").
type UserCell struct {
	theme Theme
	text  string
}

// userMarker is codex's user-prompt marker glyph followed by a gutter space
// ("› "), styled bold+dim; userIndent is the two-space continuation prefix.
const (
	userMarker = "› "
	userIndent = "  "
)

// NewUserCell builds a user message cell.
func NewUserCell(theme Theme, text string) UserCell {
	return UserCell{theme: theme, text: text}
}

// Lines implements HistoryCell.
//
// The wrapping order mirrors codex: the message body is wrapped FIRST (so the
// prefix columns are accounted for as a right margin), THEN the marker/indent is
// prepended to each wrapped line. The leading/trailing blank lines and the body
// spans carry no style; only the marker span is bold+dim.
func (c UserCell) Lines(width int) []Line {
	// Trim trailing blank lines from the source the way codex does before
	// wrapping, so an empty submission renders nothing.
	body := strings.TrimRight(c.text, "\r\n")
	if strings.TrimSpace(body) == "" {
		return nil
	}

	marker := Style{Bold: true, Dim: true}

	// Build the raw (unprefixed) body lines, then wrap to the content width
	// (width minus the two prefix columns and a one-column right margin, matching
	// codex's wrap_width = width - (LIVE_PREFIX_COLS + 1)).
	var bodyLines []Line
	for _, raw := range strings.Split(body, "\n") {
		bodyLines = append(bodyLines, Line{Spans: []Span{{Text: raw}}})
	}
	if width > 0 {
		wrapWidth := width - 3 // 2 prefix cols + 1 right margin
		if wrapWidth < 1 {
			wrapWidth = 1
		}
		bodyLines = WordWrapLines(bodyLines, wrapWidth)
	}

	out := []Line{{}} // leading blank line
	for i, bl := range bodyLines {
		prefix := userIndent
		var prefixStyle Style
		if i == 0 {
			prefix = userMarker
			prefixStyle = marker
		}
		spans := append([]Span{{Text: prefix, Style: prefixStyle}}, bl.Spans...)
		out = append(out, Line{Spans: spans})
	}
	out = append(out, Line{}) // trailing blank line
	return out
}

// --- Agent (markdown) cell --------------------------------------------------

// AgentCell renders an agent message as streamed markdown. It owns a markdown
// renderer and caches the source so it can reflow on resize.
//
// During streaming the cell accumulates committed source (via the stream
// collector in the chat surface) and re-renders the whole source each commit,
// which is how the Rust streaming controller keeps width changes correct.
type AgentCell struct {
	renderer MarkdownRenderer
	source   string
}

// NewAgentCell builds an agent markdown cell from committed source.
func NewAgentCell(renderer MarkdownRenderer, source string) AgentCell {
	return AgentCell{renderer: renderer, source: source}
}

// WithSource returns a copy of the cell with new committed source (immutability).
func (c AgentCell) WithSource(source string) AgentCell {
	c.source = source
	return c
}

// Source returns the cell's committed markdown source.
func (c AgentCell) Source() string { return c.source }

// agentBullet is codex's assistant-message bullet glyph followed by a gutter
// space ("• "), styled dim; agentIndent is the two-space continuation prefix.
const (
	agentBullet = "• "
	agentIndent = "  "
)

// Lines implements HistoryCell.
//
// The rendered markdown is wrapped to the content width (reserving the two
// prefix columns), then prefixed with a dim "• " bullet on the first line and a
// two-space indent on continuation lines — the Go analogue of
// AgentMarkdownCell::display_lines's prefix_hyperlink_lines("• " dim, "  ") in
// codex-rs/tui/src/history_cell/messages.rs. Only the bullet span is dim; the
// body inherits the markdown renderer's own styling.
func (c AgentCell) Lines(width int) []Line {
	lines := c.renderer.Render(c.source)
	if width > 0 {
		wrapWidth := width - 2 // reserve the two prefix columns
		if wrapWidth < 1 {
			wrapWidth = 1
		}
		lines = WordWrapLines(lines, wrapWidth)
	}
	return prefixAgentLines(lines)
}

// prefixAgentLines prepends the dim "• " bullet to the first line and a
// two-space indent to the rest, mirroring codex's prefix_hyperlink_lines.
func prefixAgentLines(lines []Line) []Line {
	out := make([]Line, 0, len(lines))
	for i, l := range lines {
		prefix := agentIndent
		var prefixStyle Style
		if i == 0 {
			prefix = agentBullet
			prefixStyle = Style{Dim: true}
		}
		spans := append([]Span{{Text: prefix, Style: prefixStyle}}, l.Spans...)
		out = append(out, Line{Spans: spans})
	}
	return out
}

// --- Reasoning cell ---------------------------------------------------------

// ReasoningCell renders agent reasoning (chain-of-thought summary) in a dim,
// italic style, matching the Rust reasoning_message style.
type ReasoningCell struct {
	theme  Theme
	source string
}

// NewReasoningCell builds a reasoning cell.
func NewReasoningCell(theme Theme, source string) ReasoningCell {
	return ReasoningCell{theme: theme, source: source}
}

// WithSource returns a copy with new source.
func (c ReasoningCell) WithSource(source string) ReasoningCell {
	c.source = source
	return c
}

// Source returns the reasoning source.
func (c ReasoningCell) Source() string { return c.source }

// Lines implements HistoryCell.
func (c ReasoningCell) Lines(width int) []Line {
	style := Style{Fg: c.theme.Dim, Italic: true}
	var out []Line
	out = append(out, Line{Spans: []Span{{Text: "thinking", Style: Style{Fg: c.theme.Dim, Italic: true, Bold: true}}}})
	for _, raw := range strings.Split(strings.TrimRight(c.source, "\n"), "\n") {
		out = append(out, Line{Spans: []Span{{Text: raw, Style: style}}})
	}
	if width > 0 {
		out = WordWrapLines(out, width)
	}
	return out
}

// --- Exec (command) cell ----------------------------------------------------

// ExecCellState is the lifecycle state of an exec cell.
type ExecCellState int

const (
	// ExecRunning means the command is still executing.
	ExecRunning ExecCellState = iota
	// ExecCompleted means the command finished successfully (exit 0).
	ExecCompleted
	// ExecFailed means the command exited non-zero.
	ExecFailed
)

// ExecCell renders an executed shell command and its (possibly streaming)
// output. It is the Go analogue of exec_cell/CommandOutput rendering.
type ExecCell struct {
	theme   Theme
	command []string
	output  string
	state   ExecCellState
	exit    int32
}

// NewExecCell builds a running exec cell from a command.
func NewExecCell(theme Theme, command []string) ExecCell {
	return ExecCell{theme: theme, command: command, state: ExecRunning}
}

// AppendOutput returns a copy of the cell with delta appended to its output.
func (c ExecCell) AppendOutput(delta string) ExecCell {
	c.output += delta
	return c
}

// Complete returns a copy of the cell marked complete with the given exit code.
func (c ExecCell) Complete(exit int32) ExecCell {
	c.exit = exit
	if exit == 0 {
		c.state = ExecCompleted
	} else {
		c.state = ExecFailed
	}
	return c
}

// maxExecOutputLines caps how many output lines a finished exec cell shows,
// mirroring the Rust TOOL_CALL_MAX_LINES truncation behavior.
const maxExecOutputLines = 20

// Lines implements HistoryCell.
func (c ExecCell) Lines(width int) []Line {
	var out []Line
	header := c.headerStyle()
	cmd := strings.Join(c.command, " ")
	out = append(out, Line{Spans: []Span{
		{Text: c.statusGlyph() + " ", Style: header},
		{Text: cmd, Style: Style{Fg: c.theme.Foreground, Bold: true}},
	}})

	outLines := splitTrimmed(c.output)
	if len(outLines) > maxExecOutputLines {
		hidden := len(outLines) - maxExecOutputLines
		outLines = outLines[len(outLines)-maxExecOutputLines:]
		out = append(out, Line{Spans: []Span{{Text: fmt.Sprintf("  … %d earlier lines hidden", hidden), Style: Style{Fg: c.theme.Dim}}}})
	}
	for _, l := range outLines {
		out = append(out, Line{Spans: []Span{{Text: "  " + l, Style: Style{Fg: c.theme.Dim}}}})
	}
	if width > 0 {
		out = WordWrapLines(out, width)
	}
	return out
}

func (c ExecCell) statusGlyph() string {
	switch c.state {
	case ExecCompleted:
		return "✔"
	case ExecFailed:
		return "✘"
	default:
		return "▸"
	}
}

func (c ExecCell) headerStyle() Style {
	switch c.state {
	case ExecCompleted:
		return Style{Fg: c.theme.Success, Bold: true}
	case ExecFailed:
		return Style{Fg: c.theme.Error, Bold: true}
	default:
		return Style{Fg: c.theme.Info, Bold: true}
	}
}

// --- Tool-call cell ---------------------------------------------------------

// ToolCallCell renders an MCP/dynamic tool invocation and its result.
type ToolCallCell struct {
	theme  Theme
	title  string
	result string
	done   bool
	failed bool
}

// NewToolCallCell builds a running tool-call cell.
func NewToolCallCell(theme Theme, title string) ToolCallCell {
	return ToolCallCell{theme: theme, title: title}
}

// Complete returns a copy marked complete with the given result text.
func (c ToolCallCell) Complete(result string, failed bool) ToolCallCell {
	c.result = result
	c.done = true
	c.failed = failed
	return c
}

// Lines implements HistoryCell.
func (c ToolCallCell) Lines(width int) []Line {
	glyph := "▸"
	style := Style{Fg: c.theme.Info, Bold: true}
	if c.done {
		if c.failed {
			glyph = "✘"
			style = Style{Fg: c.theme.Error, Bold: true}
		} else {
			glyph = "✔"
			style = Style{Fg: c.theme.Success, Bold: true}
		}
	}
	out := []Line{{Spans: []Span{
		{Text: glyph + " ", Style: style},
		{Text: c.title, Style: Style{Fg: c.theme.Foreground, Bold: true}},
	}}}
	for _, l := range splitTrimmed(c.result) {
		out = append(out, Line{Spans: []Span{{Text: "  " + l, Style: Style{Fg: c.theme.Dim}}}})
	}
	if width > 0 {
		out = WordWrapLines(out, width)
	}
	return out
}

// --- Notice / error cell ----------------------------------------------------

// NoticeKind classifies a NoticeCell's severity for styling.
type NoticeKind int

const (
	// NoticeInfo is a neutral system message.
	NoticeInfo NoticeKind = iota
	// NoticeError is an error message.
	NoticeError
	// NoticeWarning is a warning message.
	NoticeWarning
)

// NoticeCell renders a system notice, error, or warning.
type NoticeCell struct {
	theme Theme
	kind  NoticeKind
	text  string
}

// NewNoticeCell builds a notice cell.
func NewNoticeCell(theme Theme, kind NoticeKind, text string) NoticeCell {
	return NoticeCell{theme: theme, kind: kind, text: text}
}

// Lines implements HistoryCell.
func (c NoticeCell) Lines(width int) []Line {
	var style Style
	var prefix string
	switch c.kind {
	case NoticeError:
		style = Style{Fg: c.theme.Error, Bold: true}
		prefix = "error: "
	case NoticeWarning:
		style = Style{Fg: c.theme.Warning, Bold: true}
		prefix = "warning: "
	default:
		style = Style{Fg: c.theme.Info}
	}
	var out []Line
	for i, raw := range strings.Split(c.text, "\n") {
		p := prefix
		if i > 0 {
			p = strings.Repeat(" ", len(prefix))
		}
		out = append(out, Line{Spans: []Span{{Text: p + raw, Style: style}}})
	}
	if width > 0 {
		out = WordWrapLines(out, width)
	}
	return out
}

// --- Diff cell --------------------------------------------------------------

// DiffCell renders a unified diff or structured file-change map.
type DiffCell struct {
	renderer DiffRenderer
	diff     string
	changes  map[string]protocol.FileChange
}

// NewDiffCell builds a diff cell from a unified-diff string.
func NewDiffCell(renderer DiffRenderer, diff string) DiffCell {
	return DiffCell{renderer: renderer, diff: diff}
}

// NewFileChangesCell builds a diff cell from a structured file-change map.
func NewFileChangesCell(renderer DiffRenderer, changes map[string]protocol.FileChange) DiffCell {
	return DiffCell{renderer: renderer, changes: changes}
}

// Lines implements HistoryCell.
func (c DiffCell) Lines(width int) []Line {
	var lines []Line
	if c.changes != nil {
		lines = c.renderer.RenderFileChanges(c.changes)
	} else {
		lines = c.renderer.RenderUnifiedDiff(c.diff)
	}
	if width > 0 {
		lines = WordWrapLines(lines, width)
	}
	return lines
}

// --- helpers ----------------------------------------------------------------

// splitTrimmed splits s into lines, dropping a trailing newline and returning
// nil for empty input.
func splitTrimmed(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// WorkedForSeparatorCell is the completed-turn separator inserted after a turn
// that performed real work (port of history_cell/separators.rs
// FinalMessageSeparator, minus the runtime-metrics label which codexgo does
// not collect): a full-width dim rule, labeled "─ Worked for <elapsed> ─" when
// the turn ran longer than 60 seconds.
type WorkedForSeparatorCell struct {
	// ElapsedSeconds is the turn duration; values <= 60 render an unlabeled rule.
	ElapsedSeconds int64
}

// NewWorkedForSeparatorCell builds the separator for a completed turn.
func NewWorkedForSeparatorCell(elapsedSeconds int64) WorkedForSeparatorCell {
	return WorkedForSeparatorCell{ElapsedSeconds: elapsedSeconds}
}

// Lines implements HistoryCell.
func (c WorkedForSeparatorCell) Lines(width int) []Line {
	if width <= 0 {
		return nil
	}
	dim := Style{Dim: true}
	if c.ElapsedSeconds <= 60 {
		return []Line{{Spans: []Span{StyledSpan(strings.Repeat("─", width), dim)}}}
	}
	label := fmt.Sprintf("─ Worked for %s ─", FmtElapsedCompact(c.ElapsedSeconds))
	labelRunes := []rune(label)
	if len(labelRunes) > width {
		labelRunes = labelRunes[:width]
	}
	fill := width - len(labelRunes)
	text := string(labelRunes)
	if fill > 0 {
		text += strings.Repeat("─", fill)
	}
	return []Line{{Spans: []Span{StyledSpan(text, dim)}}}
}
