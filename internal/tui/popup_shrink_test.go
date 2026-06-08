package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPopupHeightAddsSacrificialRow(t *testing.T) {
	p := NewChatBottomPane(ChatBottomPaneConfig{Theme: testTheme()})
	// 1 match -> 2 rows (item + trailing blank); capped at maxPopupRows.
	if got := p.popupHeight(1); got != 2 {
		t.Errorf("popupHeight(1)=%d want 2", got)
	}
	if got := p.popupHeight(4); got != 5 {
		t.Errorf("popupHeight(4)=%d want 5", got)
	}
	if got := p.popupHeight(p.maxPopupRows); got != p.maxPopupRows {
		t.Errorf("popupHeight(max)=%d want %d (capped)", got, p.maxPopupRows)
	}
	if got := p.popupHeight(p.maxPopupRows + 3); got != p.maxPopupRows {
		t.Errorf("popupHeight(over)=%d want %d (capped)", got, p.maxPopupRows)
	}
}

func TestRenderPopupKeepsTrailingBlankOnNarrow(t *testing.T) {
	// Type "/mo" so exactly one built-in (/model) matches; the rendered popup
	// must include the match AND a trailing blank row so bubbletea's shrink
	// over-clear lands on the blank, not on /model.
	p := NewChatBottomPane(ChatBottomPaneConfig{Theme: testTheme()})
	for _, r := range "/mo" {
		bp, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		p = bp.(ChatBottomPane)
	}
	rows := p.renderPopup(80)
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 popup rows (match + blank), got %d: %q", len(rows), rows)
	}
	if !strings.Contains(rows[0], "/model") {
		t.Errorf("first row should be /model, got %q", rows[0])
	}
	last := rows[len(rows)-1]
	if strings.TrimSpace(last) != "" {
		t.Errorf("last popup row should be a blank sacrificial line, got %q", last)
	}
}
