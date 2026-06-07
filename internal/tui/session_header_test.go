package tui

import (
	"strings"
	"testing"
)

func TestSessionHeaderCardLayout80(t *testing.T) {
	theme := LoadTheme(nil, Capabilities{})
	cell := NewSessionHeaderCell(theme, "0.136.0", "gpt-5.5", "/work")
	got := plainLines(cell.Lines(80))

	// Like codex's with_border (forced_inner_width = None): the box is sized to the
	// widest content row, not to card_inner_width. With a short directory the model
	// row "model:     gpt-5.5   /model to change" (37 cells) is widest, so the inner
	// content width is 37 and the box is 37+4 = 41 cells wide.
	const inner = len("model:     gpt-5.5   /model to change") // 37
	pad := func(s string) string { return s + strings.Repeat(" ", inner-len(s)) }
	want := []string{
		"╭" + strings.Repeat("─", inner+2) + "╮",
		"│ " + pad(">_ CodexGO (v0.136.0)") + " │",
		"│ " + pad("") + " │",
		"│ " + pad("model:     gpt-5.5   /model to change") + " │",
		"│ " + pad("directory: /work") + " │",
		"╰" + strings.Repeat("─", inner+2) + "╯",
	}

	if len(got) != len(want) {
		t.Fatalf("line count = %d, want %d\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

func TestSessionHeaderCardWidthsConsistent(t *testing.T) {
	theme := LoadTheme(nil, Capabilities{})
	cell := NewSessionHeaderCell(theme, "0.136.0", "gpt-5.5", "/work")
	got := plainLines(cell.Lines(80))
	// Every row must be the same display width (a well-formed box).
	w0 := len([]rune(got[0]))
	for i, row := range got {
		if w := len([]rune(row)); w != w0 {
			t.Errorf("row %d width = %d, want %d (%q)", i, w, w0, row)
		}
	}
}

func TestSessionHeaderTooNarrow(t *testing.T) {
	theme := LoadTheme(nil, Capabilities{})
	cell := NewSessionHeaderCell(theme, "0.136.0", "gpt-5.5", "/work")
	if lines := cell.Lines(3); lines != nil {
		t.Fatalf("width 3 should render nothing, got %d lines", len(lines))
	}
}
