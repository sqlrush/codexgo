package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// withANSIProfile forces lipgloss to emit SGR sequences for the test body so
// the reverse-video cursor cell is observable, then restores the profile.
func withANSIProfile(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

// TestComposerCursorOverPlaceholder verifies the empty composer renders the
// block cursor (SGR reverse) over the placeholder's first glyph — the visual
// codex produces by parking the hardware cursor on the first content cell.
func TestComposerCursorOverPlaceholder(t *testing.T) {
	withANSIProfile(t)
	p := newIdleBottomPane("Explain this codebase")
	rows := p.renderPromptRows(80)
	if len(rows) != 1 {
		t.Fatalf("prompt rows = %d, want 1 (%q)", len(rows), rows)
	}
	row := rows[0]
	reversedE := composerCursorStyle.Render("E")
	if !strings.Contains(row, reversedE) {
		t.Errorf("placeholder row %q missing reverse-video first glyph %q", row, reversedE)
	}
	if stripSGR(row) != "› Explain this codebase" {
		t.Errorf("stripped row = %q, want %q", stripSGR(row), "› Explain this codebase")
	}
}

// TestComposerCursorAtEndOfText verifies the common typing position: caret past
// the last rune renders as a block cell appended after the text.
func TestComposerCursorAtEndOfText(t *testing.T) {
	withANSIProfile(t)
	p := newIdleBottomPane("unused")
	p.composer.text = "hello"
	p.composer.cursor = len("hello")
	rows := p.renderPromptRows(80)
	if len(rows) != 1 {
		t.Fatalf("prompt rows = %d, want 1 (%q)", len(rows), rows)
	}
	wantSuffix := composerCursorStyle.Render(" ")
	if !strings.HasSuffix(rows[0], wantSuffix) {
		t.Errorf("row %q missing trailing block cursor %q", rows[0], wantSuffix)
	}
	if stripSGR(rows[0]) != "› hello " {
		t.Errorf("stripped row = %q, want %q", stripSGR(rows[0]), "› hello ")
	}
}

// TestCaretRowCol locks the byte-offset -> (row, rune-col) conversion,
// including multi-byte runes and multi-line buffers.
func TestCaretRowCol(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		offset  int
		wantRow int
		wantCol int
	}{
		{"start", "abc", 0, 0, 0},
		{"end", "abc", 3, 0, 3},
		{"clamped past end", "abc", 99, 0, 3},
		{"clamped negative", "abc", -1, 0, 0},
		{"second line", "ab\ncd", 4, 1, 1},
		{"multibyte cjk", "你好x", len("你好"), 0, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, col := caretRowCol(tc.text, tc.offset)
			if row != tc.wantRow || col != tc.wantCol {
				t.Errorf("caretRowCol(%q, %d) = (%d, %d), want (%d, %d)",
					tc.text, tc.offset, row, col, tc.wantRow, tc.wantCol)
			}
		})
	}
}
