package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// DisplayRow is the render-ready representation of one row in a selection popup.
//
// Port of selection_popup_common.rs GenericDisplayRow, reduced to the fields the
// behavioral port renders. MatchIndices are character offsets into Name that are
// highlighted; Description is optional grey trailing text.
type DisplayRow struct {
	// Name is the primary label.
	Name string
	// MatchIndices are character positions in Name to emphasize (fuzzy/prefix
	// highlight). May be nil.
	MatchIndices []int
	// Description is optional dimmed trailing text.
	Description string
	// DisplayShortcut is an optional shortcut label shown at the row's left.
	DisplayShortcut string
	// IsDisabled marks the row as non-selectable.
	IsDisabled bool
	// DisabledReason explains why the row is disabled.
	DisabledReason string
}

// renderRows renders up to MaxPopupRows display rows within the scroll window,
// highlighting the selected row. It returns a string of rendered lines (at most
// the visible window height). Cosmetic deviation: the Rust renderer uses a
// bordered menu surface with two-column auto-width layout; this port renders a
// flat highlighted list with a single space gutter, since pixel parity is not
// required.
func renderRows(theme Theme, rows []DisplayRow, state ScrollState, maxRows int, emptyMessage string) string {
	if len(rows) == 0 {
		return theme.Dimmed().Render(emptyMessage)
	}

	visible := maxRows
	if visible > len(rows) {
		visible = len(rows)
	}

	top := state.ScrollTop
	if top < 0 {
		top = 0
	}
	if top > len(rows)-visible {
		top = len(rows) - visible
	}
	if top < 0 {
		top = 0
	}

	var out []string
	for i := top; i < top+visible && i < len(rows); i++ {
		out = append(out, renderRow(theme, rows[i], i == state.Selected))
	}
	return strings.Join(out, "\n")
}

// renderRow renders one display row, applying selection and highlight styling.
func renderRow(theme Theme, row DisplayRow, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "› "
	}

	name := row.Name
	if len(row.MatchIndices) > 0 && !row.IsDisabled {
		name = highlightMatches(theme, row.Name, row.MatchIndices)
	} else if row.IsDisabled {
		name = theme.Dimmed().Render(row.Name)
	} else if selected {
		name = theme.Accent().Bold(true).Render(row.Name)
	}

	var b strings.Builder
	b.WriteString(theme.Accent().Render(cursor))
	if row.DisplayShortcut != "" {
		b.WriteString(theme.Dimmed().Render(row.DisplayShortcut + " "))
	}
	b.WriteString(name)
	if row.Description != "" {
		b.WriteString(theme.Dimmed().Render("  " + row.Description))
	}
	if row.DisabledReason != "" {
		b.WriteString(theme.Dimmed().Render(" (" + row.DisabledReason + ")"))
	}
	return b.String()
}

// highlightMatches bolds the runes at the given character indices.
func highlightMatches(theme Theme, name string, indices []int) string {
	set := make(map[int]bool, len(indices))
	for _, idx := range indices {
		set[idx] = true
	}
	bold := theme.Accent().Bold(true)
	plain := lipgloss.NewStyle().Foreground(theme.Foreground)
	var b strings.Builder
	for i, r := range []rune(name) {
		if set[i] {
			b.WriteString(bold.Render(string(r)))
		} else {
			b.WriteString(plain.Render(string(r)))
		}
	}
	return b.String()
}

// Dimmed returns the theme's dim style.
func (t Theme) Dimmed() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Dim)
}

// Accent returns the theme's primary accent style.
func (t Theme) Accent() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Primary)
}
