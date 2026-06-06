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

// statusLineSegments builds the ordered status-line segment values for the
// default item set, omitting empties. Mirrors refresh_status_line_from_selections
// (chatwidget/status_surfaces.rs) over DEFAULT_STATUS_LINE_ITEMS.
func (f ComposerFooter) statusLineSegments() []string {
	var segs []string
	if model := f.modelWithReasoning(); model != "" {
		segs = append(segs, model)
	}
	if f.Directory != "" {
		segs = append(segs, f.Directory)
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
