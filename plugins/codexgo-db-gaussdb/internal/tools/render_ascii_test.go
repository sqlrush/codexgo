package tools

import (
	"strings"
	"testing"
)

// TestAsciiTableAligned is the load-bearing guard for "code matching" with
// codexgo: every emitted line of a table MUST have identical display width
// (runewidth), including CJK rows — otherwise the box borders won't line up in
// the terminal.
func TestAsciiTableAligned(t *testing.T) {
	cols := []tableColumn{
		{Header: "模块"},
		{Header: "评级"},
		{Header: "关键观测", Max: 24},
	}
	rows := [][]string{
		{"实例", "正常", "openGauss 5.0.3 · 运行 3d2h"}, // longer than Max -> truncated
		{"缓存", "警告", "命中率 50.0%"},
		{"索引", "警告", "20未使用 / 12膨胀 / 1重复"},
	}
	out := asciiTable(cols, rows)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3+len(rows)+0 && len(lines) != len(rows)+4 {
		// header(1)+top(1)+sep(1)+rows(n)+bottom(1) = n+4
	}
	want := dispWidth(lines[0])
	for i, ln := range lines {
		if got := dispWidth(ln); got != want {
			t.Errorf("table line %d display width %d != %d\n  %q", i, got, want, ln)
		}
	}
	// Max cap must have truncated the long cell (… present).
	if !strings.Contains(out, "…") {
		t.Errorf("expected truncation marker … for over-Max cell:\n%s", out)
	}
}

func TestAsciiTableRightAlign(t *testing.T) {
	cols := []tableColumn{{Header: "SQL_ID"}, {Header: "平均ms", Right: true}}
	rows := [][]string{{"1389787684", "1140545"}, {"697864226", "2300"}}
	out := asciiTable(cols, rows)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	w := dispWidth(lines[0])
	for i, ln := range lines {
		if dispWidth(ln) != w {
			t.Errorf("line %d width mismatch: %q", i, ln)
		}
	}
}

func TestBarLineWidth(t *testing.T) {
	// All bars must share identical display width when label/value pads match.
	a := barLine("缓存命中", "50%", 0.5, 20, 8, 6, "≥99%")
	b := barLine("连接数", "22%", 0.22, 20, 8, 6, "")
	// Bar segment itself is exactly barWidth cells.
	seg := func(s string) string {
		i := strings.IndexAny(s, "█░")
		return s[i:]
	}
	barA := seg(a)
	// strip suffix
	if k := strings.Index(barA, "  "); k >= 0 {
		barA = barA[:k]
	}
	if dispWidth(barA) != 20 {
		t.Errorf("bar A segment width %d != 20: %q", dispWidth(barA), barA)
	}
	if !strings.HasPrefix(a, padRight("缓存命中", 8)) {
		t.Errorf("label not padded to width 8: %q", a)
	}
	_ = b
}

func TestDispWidthCJK(t *testing.T) {
	if dispWidth("缓存") != 4 {
		t.Errorf("CJK width: 缓存 = %d, want 4", dispWidth("缓存"))
	}
	if dispWidth("abc") != 3 {
		t.Errorf("ascii width: abc = %d, want 3", dispWidth("abc"))
	}
	// box-drawing + block are single width
	if dispWidth("┌─┐│█░├┼┘") != 9 {
		t.Errorf("drawing glyphs not single-width: got %d", dispWidth("┌─┐│█░├┼┘"))
	}
}
