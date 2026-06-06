package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newInlineModel(tr TranscriptView, bp BottomPane) Model {
	return NewModel(ModelConfig{
		Caps:       Capabilities{ColorLevel: ColorLevelTrueColor},
		Transcript: tr,
		Bottom:     bp,
		Inline:     true,
	})
}

func TestInlineViewRendersTopInsetThenBottom(t *testing.T) {
	// With an empty (drained) transcript, the inline view is a single blank
	// top-inset row followed by the bottom pane, matching codex's bottom-pane
	// inset(top=1) over a zero-height active cell.
	m := newInlineModel(noopTranscript{}, recordingBottom{desired: 4})
	m.width = 80
	m.height = 24
	out := m.inlineView()
	rows := strings.Split(out, "\n")
	if len(rows) != 2 {
		t.Fatalf("inline view rows = %d, want 2 (top inset + bottom)\n%q", len(rows), rows)
	}
	if rows[0] != "" {
		t.Errorf("row 0 = %q, want blank top inset", rows[0])
	}
	if rows[1] != "BOTTOM" {
		t.Errorf("row 1 = %q, want BOTTOM", rows[1])
	}
}

func TestInlineModelDrainsHeaderToScrollback(t *testing.T) {
	theme := LoadTheme(nil, Capabilities{})
	tr := NewChatTranscript(theme).WithSessionHeader("0.136.0", "gpt-5.5", "/work")
	m := newInlineModel(tr, recordingBottom{desired: 4})

	// The first window-size message sets the width and triggers a scrollback
	// drain of the seeded session-header card.
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil {
		t.Fatal("expected a scrollback (tea.Println) command after first resize")
	}
	// The drained card lines come back as a printLineMessage via tea.Println.
	msg := cmd()
	if !containsPrintLine(msg) {
		t.Fatalf("expected a print-line scrollback message, got %T", msg)
	}

	// After draining, the transcript no longer renders the header in the live
	// viewport: the inline view is just the top inset + bottom pane.
	gm := updated.(Model)
	out := gm.inlineView()
	if strings.Contains(out, "OpenAI Codex") {
		t.Fatalf("header card should be in scrollback, not the live viewport:\n%s", out)
	}

	// A second resize with no new cells produces no further scrollback command.
	_, cmd2 := gm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd2 != nil {
		if m2 := cmd2(); containsPrintLine(m2) {
			t.Fatal("no new cells should not re-drain the header")
		}
	}
}

// containsPrintLine reports whether msg (or any element of a batch) is a
// bubbletea print-line scrollback message.
func containsPrintLine(msg tea.Msg) bool {
	switch v := msg.(type) {
	case nil:
		return false
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if containsPrintLine(c()) {
				return true
			}
		}
		return false
	default:
		// printLineMessage is unexported; identify it by its type name.
		return strings.Contains(strings.ToLower(fmt.Sprintf("%T", msg)), "printline")
	}
}

func TestChatTranscriptDrainScrollback(t *testing.T) {
	theme := LoadTheme(nil, Capabilities{})
	tr := NewChatTranscript(theme).WithSessionHeader("0.136.0", "gpt-5.5", "/work")
	lines, view := tr.DrainScrollback(80)
	if len(lines) == 0 {
		t.Fatal("expected header card lines from first drain")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "OpenAI Codex") {
		t.Fatalf("drained lines missing header:\n%s", strings.Join(lines, "\n"))
	}
	// Draining again yields nothing (already flushed).
	lines2, _ := view.(ChatTranscript).DrainScrollback(80)
	if len(lines2) != 0 {
		t.Fatalf("second drain returned %d lines, want 0", len(lines2))
	}
}
