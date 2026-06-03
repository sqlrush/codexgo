package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// homeDir returns the user's home directory, or "" when unavailable. It backs
// FormatDirectoryDisplay's "~" collapsing (port of relativize_to_home's home
// lookup).
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// This file renders the session picker (port of resume_picker.rs
// render_session_lines / render_footer_lines and the header/footer chrome). It
// uses the foundation [Theme] and lipgloss styling.
//
// Cosmetic deviation: instead of ratatui's zebra-striped backgrounds and exact
// column packing, rows use a selection caret, a styled title, and a compact
// metadata footer. The footer field ordering (date, cwd, branch) and labels
// match the Rust source.

// View renders the picker frame. It is invoked with a known viewport via
// WindowSizeMsg; if no width is known yet a reasonable default is used.
func (p ResumePicker) View(theme Theme) string {
	width := p.viewWidth
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	b.WriteString(p.renderHeader(theme))
	b.WriteString("\n\n")
	b.WriteString(p.renderToolbar(theme))
	b.WriteString("\n\n")
	b.WriteString(p.renderRows(theme, width))
	if p.inlineError != "" {
		b.WriteString("\n")
		b.WriteString(lipFg(theme.Error).Render(p.inlineError))
	}
	b.WriteString("\n")
	b.WriteString(p.renderFooterHints(theme))
	return b.String()
}

// renderHeader renders the title and the live search query line.
func (p ResumePicker) renderHeader(theme Theme) string {
	title := theme.UserMessage.Render(p.action.Title())
	q := p.query
	if q == "" {
		q = theme.StatusLine.Render("(type to search)")
	}
	return title + "\n" + theme.StatusLine.Render("Search: ") + q
}

// renderToolbar renders the Filter/Sort controls with the focused one accented
// (port of the toolbar rendering).
func (p ResumePicker) renderToolbar(theme Theme) string {
	filter := "cwd"
	if p.filterMode == FilterAll {
		filter = "all"
	}
	sort := "updated"
	if p.sortKey == SortCreatedAt {
		sort = "created"
	}
	filterPart := toolbarPart("Filter", filter, p.toolbar == toolbarFilter, theme)
	sortPart := toolbarPart("Sort", sort, p.toolbar == toolbarSort, theme)
	density := "comfortable"
	if p.density == DensityDense {
		density = "dense"
	}
	densityPart := theme.StatusLine.Render("View: " + density)
	return filterPart + "   " + sortPart + "   " + densityPart
}

func toolbarPart(label, value string, focused bool, theme Theme) string {
	text := fmt.Sprintf("%s: %s", label, value)
	if focused {
		return lipFg(theme.Primary).Bold(true).Render("[" + text + "]")
	}
	return theme.StatusLine.Render(" " + text + " ")
}

// renderRows renders the visible session rows for the current viewport.
func (p ResumePicker) renderRows(theme Theme, width int) string {
	if len(p.filteredRows) == 0 {
		return theme.StatusLine.Render(p.emptyStateText())
	}
	rowsHeight := p.viewRows
	if rowsHeight <= 0 {
		rowsHeight = len(p.filteredRows)
	}
	end := p.scrollTop + rowsHeight
	if end > len(p.filteredRows) {
		end = len(p.filteredRows)
	}
	var lines []string
	for i := p.scrollTop; i < end; i++ {
		row := p.filteredRows[i]
		selected := i == p.selected
		expanded := row.hasThread && p.expanded[row.threadID]
		lines = append(lines, p.renderRow(row, theme, selected, expanded, width)...)
	}
	return strings.Join(lines, "\n")
}

// emptyStateText returns the message shown when no rows match (port of
// render_empty_state_line).
func (p ResumePicker) emptyStateText() string {
	if p.query != "" {
		if !p.loadedAll {
			return "Searching…"
		}
		return "No matching sessions"
	}
	if !p.loadedAll && len(p.allRows) == 0 {
		return "Loading sessions…"
	}
	return "No previous sessions"
}

// renderRow renders one session row in the active density (port of
// render_session_lines).
func (p ResumePicker) renderRow(row sessionRow, theme Theme, selected, expanded bool, width int) []string {
	if p.density == DensityDense {
		return p.renderDenseRow(row, theme, selected, expanded)
	}
	return p.renderComfortableRow(row, theme, selected, expanded, width)
}

// selectionMarker returns the row caret (port of selection_marker).
func selectionMarker(selected, expanded bool) string {
	switch {
	case selected && expanded:
		return "⌄ "
	case selected:
		return "❯ "
	default:
		return "  "
	}
}

// renderComfortableRow renders a multi-line row (port of
// render_comfortable_session_lines).
func (p ResumePicker) renderComfortableRow(row sessionRow, theme Theme, selected, expanded bool, width int) []string {
	marker := selectionMarker(selected, expanded)
	title := truncateRunes(row.displayPreview(), maxInt(width-2, 0))
	if selected {
		title = selectedTitleStyle(theme).Render(title)
	}
	lines := []string{marker + title}
	if expanded {
		lines = append(lines, p.previewLines(row, theme)...)
		return lines
	}
	footer := p.footerLines(row, theme, width)
	lines = append(lines, footer...)
	return lines
}

// renderDenseRow renders a single-line row (port of render_dense_session_lines).
func (p ResumePicker) renderDenseRow(row sessionRow, theme Theme, selected, expanded bool) []string {
	marker := selectionMarker(selected, expanded)
	date := p.activeDate(row)
	title := row.displayPreview()
	if selected {
		title = selectedTitleStyle(theme).Render(title)
	}
	line := marker + theme.StatusLine.Render(padRight(date, 12)+"  ") + title
	lines := []string{line}
	if expanded {
		lines = append(lines, p.previewLines(row, theme)...)
	}
	return lines
}

// previewLines renders an expanded row's preview placeholder. The synchronous
// store does not load transcript previews lazily, so this shows the stored
// preview text wrapped to a couple of lines (deviation from the lazy app-server
// transcript preview).
func (p ResumePicker) previewLines(row sessionRow, theme Theme) []string {
	text := strings.TrimSpace(row.preview)
	if text == "" {
		return []string{"    " + theme.StatusLine.Render("(no preview)")}
	}
	return []string{"    " + theme.StatusLine.Render(text)}
}

// footerLines renders the metadata footer (port of render_footer_lines): date,
// optional cwd, branch.
func (p ResumePicker) footerLines(row sessionRow, theme Theme, width int) []string {
	dim := theme.StatusLine
	date := p.activeDate(row)
	parts := []string{date}
	if p.filterMode == FilterAll {
		cwd := row.cwd
		if cwd == "" {
			cwd = "no cwd"
		} else {
			cwd = FormatDirectoryDisplay(cwd)
		}
		parts = append(parts, sessionMetaCwdIcon+" "+cwd)
	}
	branch := row.gitBranch
	if branch == "" {
		branch = "no branch"
	}
	if sessionMetaBranchIcon != "" {
		branch = sessionMetaBranchIcon + " " + branch
	}
	parts = append(parts, branch)
	return []string{"  " + dim.Render(strings.Join(parts, "  "))}
}

// activeDate returns the relative time for the active sort key (port of the
// date selection in render_footer_lines).
func (p ResumePicker) activeDate(row sessionRow) string {
	created := FormatRelativeTime(p.relativeRef, row.createdAt)
	updatedSrc := row.updatedAt
	if updatedSrc.IsZero() {
		updatedSrc = row.createdAt
	}
	updated := FormatRelativeTime(p.relativeRef, updatedSrc)
	if p.sortKey == SortCreatedAt {
		return created
	}
	return updated
}

// renderFooterHints renders the key-hint footer (port of footer_hint_lines).
func (p ResumePicker) renderFooterHints(theme Theme) string {
	dim := theme.StatusLine
	hints := []string{
		"↑/↓ navigate",
		"enter " + p.action.actionVerb(),
		"tab toolbar",
		"^o density",
		"^e expand",
		"esc back",
	}
	return dim.Render(strings.Join(hints, "  ·  "))
}

func (a SessionPickerAction) actionVerb() string {
	if a == ActionFork {
		return "fork"
	}
	return "resume"
}

// selectedTitleStyle returns the highlight style for selected titles (port of
// selected_session_style: magenta on light, yellow otherwise).
func selectedTitleStyle(theme Theme) lipgloss.Style {
	return lipFg(theme.Warning).Bold(true)
}

// --- relative time and path helpers ------------------------------------------

// FormatRelativeTime formats a timestamp relative to a reference, mirroring
// resume_picker.rs format_relative_time.
func FormatRelativeTime(reference, ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	seconds := int64(reference.Sub(ts).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	if seconds == 0 {
		return "now"
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds ago", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm ago", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	return fmt.Sprintf("%dd ago", hours/24)
}

// FormatDirectoryDisplay renders a directory path with the home prefix collapsed
// to "~", mirroring status/helpers.rs format_directory_display (without the
// max-width center truncation, applied by callers as needed).
func FormatDirectoryDisplay(dir string) string {
	home := homeDir()
	if home != "" {
		if dir == home {
			return "~"
		}
		prefix := home + "/"
		if strings.HasPrefix(dir, prefix) {
			return "~/" + dir[len(prefix):]
		}
	}
	return dir
}

// truncateRunes truncates s to at most width runes, appending an ellipsis when
// truncated (a simple analogue of text_formatting::truncate_text).
func truncateRunes(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}

// padRight pads s with spaces to at least width runes.
func padRight(s string, width int) string {
	n := width - len([]rune(s))
	if n <= 0 {
		return s
	}
	return s + strings.Repeat(" ", n)
}
