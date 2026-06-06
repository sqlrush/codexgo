package paritytest

import "testing"

// These tests verify the dependency-free ANSI/VT100 emulator used by the TUI
// frame-differential harness. They are pure (no binaries, no PTY) so they always
// run, keeping the interpreter itself trustworthy regardless of CODEX_PARITY_BIN.

func TestVTGridPlainText(t *testing.T) {
	g := RenderGrid([]byte("hello"), 3, 10)
	if got := g.Row(0); got != "hello" {
		t.Fatalf("row0 = %q, want %q", got, "hello")
	}
	if got := g.Row(1); got != "" {
		t.Fatalf("row1 = %q, want empty", got)
	}
}

func TestVTGridCUPPositioning(t *testing.T) {
	// ESC[2;3H positions at row 2 col 3 (1-based), then writes "X".
	g := RenderGrid([]byte("\x1b[2;3HX"), 4, 10)
	if got := g.Row(1); got != "  X" {
		t.Fatalf("row1 = %q, want %q", got, "  X")
	}
}

func TestVTGridCRLF(t *testing.T) {
	g := RenderGrid([]byte("ab\r\ncd"), 4, 10)
	if got := g.Row(0); got != "ab" {
		t.Fatalf("row0 = %q", got)
	}
	if got := g.Row(1); got != "cd" {
		t.Fatalf("row1 = %q", got)
	}
}

func TestVTGridEraseLine(t *testing.T) {
	// Write "hello", move to col 3 (0-based col 2), erase to EOL.
	g := RenderGrid([]byte("hello\x1b[1;3H\x1b[K"), 2, 10)
	if got := g.Row(0); got != "he" {
		t.Fatalf("row0 = %q, want %q", got, "he")
	}
}

func TestVTGridEraseDisplayAll(t *testing.T) {
	g := RenderGrid([]byte("line1\r\nline2\x1b[2J"), 3, 10)
	if got := g.String(); got != "\n\n" {
		t.Fatalf("grid = %q, want all blank", got)
	}
}

func TestVTGridBackspace(t *testing.T) {
	g := RenderGrid([]byte("abc\b\bX"), 2, 10)
	if got := g.Row(0); got != "aXc" {
		t.Fatalf("row0 = %q, want %q", got, "aXc")
	}
}

func TestVTGridSGRDropped(t *testing.T) {
	// SGR codes must not leak into the grid; only "red" should remain.
	g := RenderGrid([]byte("\x1b[31mred\x1b[0m"), 2, 10)
	if got := g.Row(0); got != "red" {
		t.Fatalf("row0 = %q, want %q", got, "red")
	}
}

func TestVTGridOSCConsumed(t *testing.T) {
	// An OSC title sequence (terminated by BEL) must be consumed entirely.
	g := RenderGrid([]byte("\x1b]0;my title\x07text"), 2, 20)
	if got := g.Row(0); got != "text" {
		t.Fatalf("row0 = %q, want %q", got, "text")
	}
}

func TestVTGridOSCStringTerminator(t *testing.T) {
	// OSC terminated by ST (ESC \) — e.g. background-color query echo.
	g := RenderGrid([]byte("\x1b]11;rgb:0/0/0\x1b\\ok"), 2, 20)
	if got := g.Row(0); got != "ok" {
		t.Fatalf("row0 = %q, want %q", got, "ok")
	}
}

func TestVTGridReverseIndexScrollsDown(t *testing.T) {
	// Set a scroll region rows 1..3 (1-based), home to top, then RI at top
	// scrolls the region down, opening a blank top line and pushing content down.
	in := "\x1b[1;3r" + // scroll region rows 1..3
		"\x1b[1;1Hrow1\r\n" + // row1 (0-based 0)
		"row2\r\n" + // row2 (0-based 1)
		"\x1b[1;1H" + // home
		"\x1bM" // RI at top → scroll down
	g := RenderGrid([]byte(in), 4, 10)
	if got := g.Row(0); got != "" {
		t.Fatalf("row0 = %q, want blank after scroll-down", got)
	}
	if got := g.Row(1); got != "row1" {
		t.Fatalf("row1 = %q, want %q", got, "row1")
	}
}

func TestVTGridAlternateScreenIsolated(t *testing.T) {
	// Write to primary, enter alt screen, write different text. The visible grid
	// (alt) must show only the alt content.
	in := "primary\x1b[?1049h\x1b[2J\x1b[1;1Halt"
	g := RenderGrid([]byte(in), 2, 20)
	if got := g.Row(0); got != "alt" {
		t.Fatalf("alt row0 = %q, want %q", got, "alt")
	}
}

func TestVTGridWideRune(t *testing.T) {
	// A box-drawing/CJK-width rune: '世' is width 2. Following text must land two
	// cells over.
	g := RenderGrid([]byte("世x"), 2, 10)
	if got := g.Row(0); got != "世x" {
		t.Fatalf("row0 = %q, want %q", got, "世x")
	}
}

func TestVTGridLineFeedScrolls(t *testing.T) {
	// Three rows in a 2-row grid: the first line scrolls off the top.
	g := RenderGrid([]byte("a\r\nb\r\nc"), 2, 10)
	if got := g.Row(0); got != "b" {
		t.Fatalf("row0 = %q, want %q", got, "b")
	}
	if got := g.Row(1); got != "c" {
		t.Fatalf("row1 = %q, want %q", got, "c")
	}
}
