package tui

import "testing"

func TestWordWrapBreaksOnSpaces(t *testing.T) {
	l := Line{Spans: []Span{{Text: "the quick brown fox"}}}
	got := plainLines(WordWrapLine(l, 9))
	want := []string{"the quick", "brown fox"}
	if len(got) != len(want) {
		t.Fatalf("wrap = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWordWrapHardBreaksLongWord(t *testing.T) {
	l := Line{Spans: []Span{{Text: "abcdefghij"}}}
	got := plainLines(WordWrapLine(l, 4))
	if len(got) < 2 {
		t.Fatalf("expected hard-break into multiple rows, got %q", got)
	}
	for _, row := range got {
		if len([]rune(row)) > 4 {
			t.Fatalf("row %q exceeds width 4", row)
		}
	}
}

func TestWordWrapPreservesStyleAcrossBreak(t *testing.T) {
	style := Style{Bold: true}
	l := Line{Spans: []Span{{Text: "alpha beta gamma", Style: style}}}
	rows := WordWrapLine(l, 6)
	for _, r := range rows {
		for _, s := range r.Spans {
			if !s.Style.Bold {
				t.Fatalf("style not preserved on span %q", s.Text)
			}
		}
	}
}

func TestWordWrapNoChangeWhenFits(t *testing.T) {
	l := Line{Spans: []Span{{Text: "short"}}}
	got := WordWrapLine(l, 80)
	if len(got) != 1 || plain(got[0]) != "short" {
		t.Fatalf("unexpected wrap of fitting line: %q", plainLines(got))
	}
}
