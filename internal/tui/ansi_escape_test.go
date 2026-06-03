package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAnsiEscapeLinePlain(t *testing.T) {
	line := AnsiEscapeLine("hello world")
	if got := line.String(); got != "hello world" {
		t.Fatalf("plain text = %q", got)
	}
	if len(line.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(line.Spans))
	}
	if line.Spans[0].Style != (Style{}) {
		t.Fatalf("plain span should have empty style, got %+v", line.Spans[0].Style)
	}
}

func TestAnsiEscapeLineExpandsTabs(t *testing.T) {
	line := AnsiEscapeLine("a\tb")
	if got := line.String(); got != "a    b" {
		t.Fatalf("tab expansion = %q, want %q", got, "a    b")
	}
}

func TestAnsiEscapeLineBold(t *testing.T) {
	// ESC[1m bold ESC[0m reset
	line := AnsiEscapeLine("\x1b[1mbold\x1b[0mplain")
	if got := line.String(); got != "boldplain" {
		t.Fatalf("text = %q", got)
	}
	if len(line.Spans) != 2 {
		t.Fatalf("expected 2 spans, got %d (%+v)", len(line.Spans), line.Spans)
	}
	if !line.Spans[0].Style.Bold {
		t.Fatalf("first span should be bold: %+v", line.Spans[0])
	}
	if line.Spans[1].Style.Bold {
		t.Fatalf("second span should not be bold: %+v", line.Spans[1])
	}
}

func TestAnsiEscapeLineColors(t *testing.T) {
	// 31 = red fg, 42 = green bg.
	line := AnsiEscapeLine("\x1b[31;42mx\x1b[0m")
	if len(line.Spans) == 0 {
		t.Fatal("expected at least one span")
	}
	sp := line.Spans[0]
	if sp.Style.Fg != lipgloss.Color("1") {
		t.Fatalf("fg = %v, want index 1", sp.Style.Fg)
	}
	if sp.Style.Bg != lipgloss.Color("2") {
		t.Fatalf("bg = %v, want index 2", sp.Style.Bg)
	}
}

func TestAnsiEscapeLineTrueColor(t *testing.T) {
	// 38;2;r;g;b truecolor foreground.
	line := AnsiEscapeLine("\x1b[38;2;18;52;86mx")
	if line.Spans[0].Style.Fg != lipgloss.Color("#123456") {
		t.Fatalf("truecolor fg = %v, want #123456", line.Spans[0].Style.Fg)
	}
}

func TestAnsiEscapeLine256Color(t *testing.T) {
	// 38;5;n indexed foreground.
	line := AnsiEscapeLine("\x1b[38;5;208mx")
	if line.Spans[0].Style.Fg != lipgloss.Color("208") {
		t.Fatalf("256 fg = %v, want 208", line.Spans[0].Style.Fg)
	}
}

func TestAnsiEscapeMultiLineTakesFirst(t *testing.T) {
	line := AnsiEscapeLine("first\nsecond")
	if got := line.String(); got != "first" {
		t.Fatalf("multi-line first = %q, want first", got)
	}
}

func TestAnsiEscapeDropsOSC(t *testing.T) {
	// OSC hyperlink sequence around text; the visible text should remain.
	in := "\x1b]8;;https://example.com\x07link\x1b]8;;\x07"
	line := AnsiEscapeLine(in)
	if got := line.String(); got != "link" {
		t.Fatalf("OSC stripped text = %q, want link", got)
	}
}

func TestAnsiEscapeUnterminatedCSI(t *testing.T) {
	// Should not panic and should drop the dangling sequence.
	line := AnsiEscapeLine("text\x1b[")
	if got := line.String(); got != "text" {
		t.Fatalf("unterminated CSI = %q, want text", got)
	}
}

func TestApplySGRReset(t *testing.T) {
	s := Style{Bold: true, Fg: lipgloss.Color("1")}
	got := applySGR(s, "0")
	if got != (Style{}) {
		t.Fatalf("reset = %+v, want empty", got)
	}
	// Empty params is also a reset.
	if applySGR(s, "") != (Style{}) {
		t.Fatal("empty params should reset")
	}
}
