package tui

import (
	"strings"
	"testing"
)

// stripSGR removes ANSI escape sequences so layout assertions compare plain text.
func stripSGR(s string) string {
	out := s
	for {
		i := strings.IndexByte(out, 0x1b)
		if i < 0 {
			break
		}
		j := i + 1
		for j < len(out) && out[j] != 'm' {
			j++
		}
		if j < len(out) {
			j++
		}
		out = out[:i] + out[j:]
	}
	return out
}

func newIdleBottomPane(placeholder string) ChatBottomPane {
	p := NewChatBottomPane(ChatBottomPaneConfig{
		Theme:      LoadTheme(nil, Capabilities{}),
		FileSearch: nil,
		Footer:     ComposerFooter{Model: "gpt-5.5", Directory: "~/work"},
	})
	p.composer = p.composer.WithPlaceholder(placeholder)
	return p
}

func TestIdleComposerBlockStructure(t *testing.T) {
	p := newIdleBottomPane("Explain this codebase")
	out := p.View(Rect{X: 0, Y: 0, Width: 80, Height: 8})
	rows := strings.Split(out, "\n")
	for i := range rows {
		rows[i] = stripSGR(rows[i])
	}

	// Idle composer: top pad, "› <placeholder>", bottom pad, footer status line.
	// This mirrors codex's Min(3) composer block (textarea inset top=1/bottom=1)
	// followed by the default status-line footer (FOOTER_SPACING_HEIGHT = 0).
	want := []string{
		"",
		"› Explain this codebase",
		"",
		"  gpt-5.5 default · ~/work",
	}
	if len(rows) != len(want) {
		t.Fatalf("row count = %d, want %d\nrows=%q", len(rows), len(want), rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}

func TestIdleComposerDesiredHeightIsFour(t *testing.T) {
	p := newIdleBottomPane("x")
	if h := p.DesiredHeight(80); h != 4 {
		t.Fatalf("idle DesiredHeight = %d, want 4 (top-pad + prompt + bottom-pad + footer)", h)
	}
}

func TestComposerBlockWithMultilineText(t *testing.T) {
	p := newIdleBottomPane("ph")
	p.composer.text = "line1\nline2"
	out := p.View(Rect{X: 0, Y: 0, Width: 80, Height: 10})
	rows := strings.Split(out, "\n")
	for i := range rows {
		rows[i] = stripSGR(rows[i])
	}
	// top-pad, "› line1", "  line2", bottom-pad, footer.
	if len(rows) != 5 {
		t.Fatalf("row count = %d, want 5\nrows=%q", len(rows), rows)
	}
	if rows[0] != "" || rows[3] != "" {
		t.Errorf("expected blank top/bottom padding, got %q", rows)
	}
	if rows[1] != "› line1" || rows[2] != "  line2" {
		t.Errorf("text rows = %q, %q; want %q, %q", rows[1], rows[2], "› line1", "  line2")
	}
}
