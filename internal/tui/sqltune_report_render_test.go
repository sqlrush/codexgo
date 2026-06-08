package tui

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// TestTuningReportFixedWidthBlocksSurvive verifies the gaussdb plugin's SQL
// Tuning Report renders correctly through codexgo's TUI markdown pipeline: the
// deterministic fixed-width data blocks (inside ``` fences) survive line-for-line
// — NOT collapsed/reflowed the way a markdown table would be. This is the whole
// reason the report uses code fences instead of | tables.
func TestTuningReportFixedWidthBlocksSurvive(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	report := strings.Join([]string{
		"## 3. 关键证据",
		"",
		"### 表 / 索引 / 统计信息",
		"",
		"```",
		"1/5 order_items",
		"  行数 : 1000000",
		"  大小 : 72 MB",
		"  索引 : order_items_pkey(scans=0)",
		"",
		"2/5 orders",
		"  行数 : 500000",
		"```",
		"",
	}, "\n")

	got := plainLines(r.Render(report))

	// Every fixed-width line must appear verbatim, on its own line.
	for _, want := range []string{
		"1/5 order_items",
		"  行数 : 1000000",
		"  大小 : 72 MB",
		"  索引 : order_items_pkey(scans=0)",
		"2/5 orders",
		"  行数 : 500000",
	} {
		found := false
		for _, line := range got {
			if line == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("fixed-width line not preserved verbatim: %q\nrendered lines: %q", want, got)
		}
	}

	// The headings must render.
	if !containsLineWith(got, "关键证据") || !containsLineWith(got, "表 / 索引") {
		t.Errorf("headings missing in: %q", got)
	}
}

// TestTuningReportPlanTreeIndentSurvives checks the annotated plan tree keeps its
// leading-space indentation (the visual tree shape) through the fenced block.
func TestTuningReportPlanTreeIndentSurvives(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	report := strings.Join([]string{
		"## 2. 执行计划",
		"",
		"```plan",
		"Limit cost=29166 rows=100",
		"  - [P1] Sort cost=29174 rows=3200",
		"    - Aggregate cost=29043 rows=3200",
		"      - [P2] Hash Join cost=28867 rows=3200",
		"```",
	}, "\n")

	got := plainLines(r.Render(report))
	for _, want := range []string{
		"Limit cost=29166 rows=100",
		"  - [P1] Sort cost=29174 rows=3200",
		"    - Aggregate cost=29043 rows=3200",
		"      - [P2] Hash Join cost=28867 rows=3200",
	} {
		if !containsExact(got, want) {
			t.Errorf("plan line lost indentation/content: %q\nrendered: %q", want, got)
		}
	}
}

// TestMarkdownTableRendersAligned verifies GFM markdown tables now render as a
// runewidth-aligned ASCII box table. Previously goldmark had no table extension
// and the pipes collapsed into one garbled line (the screenshot bug); now the
// MarkdownRenderer draws a real aligned box, including CJK headers.
func TestMarkdownTableRendersAligned(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	src := "| 排名 | 平均耗时 | 调用 |\n|---|---|---:|\n| 1 | 1140s | 1 |\n| 2 | 2.27s | 1 |\n"
	got := plainLines(r.Render(src))

	var tbl []string
	for _, l := range got {
		if strings.ContainsAny(l, "+|") { // ASCII frame: + corners/seps, | verticals
			tbl = append(tbl, l)
		}
	}
	// top + header + sep + 2 rows + bottom = 6 lines
	if len(tbl) < 6 {
		t.Fatalf("table not rendered as a multi-line ASCII box (got %d box lines):\n%q", len(tbl), got)
	}
	w := runewidth.StringWidth(tbl[0])
	for i, l := range tbl {
		if got := runewidth.StringWidth(l); got != w {
			t.Errorf("table line %d display width %d != %d:\n  %q", i, got, w, l)
		}
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"排名", "平均耗时", "1140s", "2.27s"} {
		if !strings.Contains(joined, want) {
			t.Errorf("table content missing %q\n%q", want, got)
		}
	}
}

func containsExact(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func containsLineWith(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
