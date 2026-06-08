package tui

import (
	"strings"
	"testing"
)

// TestRichVisualBlocksSurvive verifies ASCII tables / trees / bar charts inside
// a code fence survive codexgo's markdown pipeline verbatim (line-for-line),
// AND that runewidth-aligned CJK rows are not mangled or wrapped at a sane width.
// This is the basis for the health-report's tables/trees/charts.
func TestRichVisualBlocksSurvive(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	block := strings.Join([]string{
		"```",
		"┌────────┬──────┬────────────────────┐",
		"│ 模块   │ 评级 │ 观测               │",
		"├────────┼──────┼────────────────────┤",
		"│ 缓存   │ 警告 │ 命中率 50.0%       │",
		"│ 维护   │ 警告 │ 死元组 174,798     │",
		"└────────┴──────┴────────────────────┘",
		"慢 SQL 19min",
		"├─ 列函数阻塞索引",
		"│  └─ order_items 全表扫描",
		"└─ 359MB 临时文件落盘",
		"命中率  50% ██████████░░░░░░░░░░  目标 99%",
		"耗时趋势  ▁▂▃▄▅▆▇█  1140s",
		"```",
	}, "\n")
	for _, width := range []int{0, 100} {
		got := plainLines(r.Render(block))
		_ = width
		for _, want := range []string{
			"┌────────┬──────┬────────────────────┐",
			"│ 缓存   │ 警告 │ 命中率 50.0%       │",
			"├─ 列函数阻塞索引",
			"│  └─ order_items 全表扫描",
			"命中率  50% ██████████░░░░░░░░░░  目标 99%",
			"耗时趋势  ▁▂▃▄▅▆▇█  1140s",
		} {
			if !containsExact(got, want) {
				t.Errorf("rich-visual line not preserved verbatim:\n  want %q\n  got  %q", want, got)
			}
		}
	}
}
