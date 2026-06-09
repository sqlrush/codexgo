package tui

import "testing"

// TestMcpDirectCellRendersMarkdown checks the cell renders its markdown source
// (fixed-width report blocks survive). McpDirectCell is used by the deterministic
// slash path (McpToolResultMsg); model-invoked tool results are no longer
// auto-rendered — the model summarizes them instead.
func TestMcpDirectCellRendersMarkdown(t *testing.T) {
	cell := NewMcpDirectCell(NewMarkdownRenderer(testTheme()), "## 报告\n\n```\n1/1 orders\n  行数 : 500\n```\n")
	got := plainLines(cell.Lines(0))
	if !containsExact(got, "1/1 orders") || !containsExact(got, "  行数 : 500") {
		t.Errorf("fixed-width block not preserved: %q", got)
	}
}
