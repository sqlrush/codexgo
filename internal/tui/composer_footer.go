package tui

import "strings"

// statusLineSeparator joins status-line segments, matching
// STATUS_LINE_SEPARATOR in codex-rs/tui/src/bottom_pane/status_line_style.rs.
const statusLineSeparator = " · "

// footerIndentCols is the left/right gutter for footer hint rows, mirroring
// FOOTER_INDENT_COLS (= LIVE_PREFIX_COLS = 2) in codex-rs/tui/src/ui_consts.rs.
const footerIndentCols = 2

// ComposerFooter holds the contextual data the idle composer footer renders:
// codex's default status line is `["model-with-reasoning", "current-dir"]`
// (DEFAULT_STATUS_LINE_ITEMS in chatwidget.rs) joined by " · ".
//
// Reasoning effort defaults to "default" when unset
// (status_line_reasoning_effort_label, chatwidget/status_controls.rs).
type ComposerFooter struct {
	// Model is the active model slug (e.g. "gpt-5.5").
	Model string
	// ReasoningLabel is the reasoning-effort label (e.g. "default", "high").
	// Empty resolves to "default".
	ReasoningLabel string
	// Directory is the display form of the current working directory (home
	// collapsed to "~", no max-width truncation — applied per-line below).
	Directory string
}

// statusLineAccent identifies which softened theme color a status-line segment
// carries, mirroring StatusLineAccent (status_line_style.rs). Only the two
// accents the default status line uses (Model, Path) are modeled here.
type statusLineAccent int

const (
	accentModel statusLineAccent = iota
	accentPath
)

// statusSegment is one ordered status-line segment with its accent.
type statusSegment struct {
	text   string
	accent statusLineAccent
}

// statusLineSegmentsTyped builds the ordered (value, accent) pairs for the
// default item set, omitting empties. Mirrors refresh_status_line_from_selections
// (chatwidget/status_surfaces.rs) over DEFAULT_STATUS_LINE_ITEMS, which is
// [ModelWithReasoning, CurrentDir].
func (f ComposerFooter) statusLineSegmentsTyped() []statusSegment {
	var segs []statusSegment
	if model := f.modelWithReasoning(); model != "" {
		segs = append(segs, statusSegment{text: model, accent: accentModel})
	}
	if f.Directory != "" {
		segs = append(segs, statusSegment{text: f.Directory, accent: accentPath})
	}
	return segs
}

// statusLineSegments builds the ordered status-line segment values for the
// default item set, omitting empties. Mirrors refresh_status_line_from_selections
// (chatwidget/status_surfaces.rs) over DEFAULT_STATUS_LINE_ITEMS.
func (f ComposerFooter) statusLineSegments() []string {
	typed := f.statusLineSegmentsTyped()
	segs := make([]string, 0, len(typed))
	for _, s := range typed {
		segs = append(segs, s.text)
	}
	return segs
}

// modelWithReasoning returns "<model> <reasoning>" (status_surfaces.rs
// model_with_reasoning_display_name). Returns "" when no model is configured.
func (f ComposerFooter) modelWithReasoning() string {
	if f.Model == "" {
		return ""
	}
	label := f.ReasoningLabel
	if label == "" {
		label = "default"
	}
	return f.Model + " " + label
}

// line renders the footer status line into at most width cells. The content
// (segments joined by " · ") is truncated to width-indent with a trailing "…",
// then the indent gutter is prefixed — matching render_footer_line over the
// passive status line in chat_composer.rs.
func (f ComposerFooter) line(width int) string {
	segs := f.statusLineSegments()
	if len(segs) == 0 || width <= footerIndentCols {
		return ""
	}
	content := strings.Join(segs, statusLineSeparator)
	avail := width - footerIndentCols
	content = truncateRunes(content, avail)
	return strings.Repeat(" ", footerIndentCols) + content
}

// styledLine renders the footer status line as a [Line] of styled spans for the
// given color level: each segment carries its softened theme accent (Model
// #f6e2b7 / Path #abdfa7, not dim), the " · " separator is dim, and the 2-col
// indent gutter is an unstyled leading span. The content is span-truncated to
// width-indent cells with a trailing dim "…", mirroring
// truncate_line_with_ellipsis_if_overflow over the passive status line.
//
// It returns the empty Line when there is nothing to show.
func (f ComposerFooter) styledLine(width int, level StdoutColorLevel) Line {
	segs := f.statusLineSegmentsTyped()
	if len(segs) == 0 || width <= footerIndentCols {
		return Line{}
	}

	// Build the colored spans joined by a dim separator (no indent yet).
	var spans []Span
	for i, s := range segs {
		if i > 0 {
			spans = append(spans, StyledSpan(statusLineSeparator, Style{Dim: true}))
		}
		spans = append(spans, StyledSpan(s.text, f.accentStyle(s.accent, level)))
	}

	avail := width - footerIndentCols
	spans = truncateSpansWithEllipsis(spans, avail)

	// Prepend the unstyled 2-col indent gutter.
	indent := PlainSpan(strings.Repeat(" ", footerIndentCols))
	return Line{Spans: append([]Span{indent}, spans...)}
}

// truncateSpansWithEllipsis truncates a span list to at most width runes,
// preserving each span's style and appending a dim "…" when truncation occurs.
// It mirrors the visible behavior of truncateRunes (so the styled and plain
// footer lines carry identical characters) while keeping per-span styles, like
// truncate_line_with_ellipsis_if_overflow over a styled status Line.
func truncateSpansWithEllipsis(spans []Span, width int) []Span {
	if width <= 0 {
		return nil
	}
	total := 0
	for _, s := range spans {
		total += len([]rune(s.Text))
	}
	if total <= width {
		return spans
	}
	// width == 1: a lone ellipsis would not fit a span char; emit a bare "…".
	limit := width - 1
	var out []Span
	used := 0
	for _, s := range spans {
		r := []rune(s.Text)
		if used >= limit {
			break
		}
		if used+len(r) <= limit {
			out = append(out, s)
			used += len(r)
			continue
		}
		// Partially include this span up to the limit.
		take := limit - used
		out = append(out, StyledSpan(string(r[:take]), s.Style))
		used = limit
		break
	}
	out = append(out, StyledSpan("…", Style{Dim: true}))
	return out
}

// accentStyle resolves a status-line accent to its softened theme color.
//
// codex's status-line segments are raw Color::Rgb spans (status_line_style.rs);
// ratatui emits those as 24-bit truecolor (\x1b[38;2;r;g;b m) UNCONDITIONALLY —
// it never downsamples Color::Rgb to a 256-indexed approximation the way codex's
// best_color helper does for other surfaces. So the footer emits the softened RGB
// verbatim whenever color is enabled, and only falls back to the default
// foreground when color is suppressed (NO_COLOR / dumb terminal → Unknown level).
func (f ComposerFooter) accentStyle(accent statusLineAccent, level StdoutColorLevel) Style {
	if level == ColorLevelUnknown {
		return Style{}
	}
	var rgb RGB
	switch accent {
	case accentPath:
		rgb = statusLinePathColor
	default:
		rgb = statusLineModelColor
	}
	return Style{Fg: RGBColor(rgb)}
}
