package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func mkContent(text string, audience ...string) json.RawMessage {
	item := map[string]any{"type": "text", "text": text}
	if len(audience) > 0 {
		item["annotations"] = map[string]any{"audience": audience}
	}
	b, _ := json.Marshal(item)
	return b
}

// okResult wraps a CallToolResult in the externally-tagged Result<Ok,Err> form
// the event carries.
func okResult(content ...json.RawMessage) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"Ok": protocol.CallToolResult{Content: content}})
	return b
}

// TestSplitUserAudience verifies the routing: user+assistant => autoShow (a
// deliberate report, shown immediately), user-only => gated (a monitoring table,
// held for @show), assistant-only / untagged => neither.
func TestSplitUserAudience(t *testing.T) {
	raw := okResult(
		mkContent("# 报告\nREPORT_BODY", "user", "assistant"), // report -> autoShow
		mkContent("MONITOR_TABLE", "user"),                  // monitoring -> gated
		mkContent("analysis instruction", "assistant"),      // model-only -> neither
		mkContent("legacy untagged"),                        // untagged -> neither
	)
	auto, gated := splitUserAudience(raw)
	if !strings.Contains(auto, "REPORT_BODY") {
		t.Errorf("user+assistant block should be auto-shown: %q", auto)
	}
	if strings.Contains(auto, "MONITOR_TABLE") || strings.Contains(auto, "instruction") || strings.Contains(auto, "legacy") {
		t.Errorf("autoShow leaked non-report content: %q", auto)
	}
	if !strings.Contains(gated, "MONITOR_TABLE") {
		t.Errorf("user-only block should be gated: %q", gated)
	}
	if strings.Contains(gated, "REPORT_BODY") || strings.Contains(gated, "instruction") {
		t.Errorf("gated leaked non-monitoring content: %q", gated)
	}
}

func TestSplitUserAudienceNoneWhenUntagged(t *testing.T) {
	auto, gated := splitUserAudience(okResult(mkContent("legacy")))
	if auto != "" || gated != "" {
		t.Errorf("untagged content should be neither, got auto=%q gated=%q", auto, gated)
	}
	if a, g := splitUserAudience(json.RawMessage(`{"Err":"boom"}`)); a != "" || g != "" {
		t.Errorf("error result should be empty, got %q/%q", a, g)
	}
	if a, g := splitUserAudience(nil); a != "" || g != "" {
		t.Errorf("nil should be empty, got %q/%q", a, g)
	}
}

// TestMcpDirectCellRendersMarkdown checks the cell renders its markdown source
// (fixed-width report blocks survive — same pipeline as the report test).
func TestMcpDirectCellRendersMarkdown(t *testing.T) {
	cell := NewMcpDirectCell(NewMarkdownRenderer(testTheme()), "## 报告\n\n```\n1/1 orders\n  行数 : 500\n```\n")
	got := plainLines(cell.Lines(0))
	if !containsExact(got, "1/1 orders") || !containsExact(got, "  行数 : 500") {
		t.Errorf("fixed-width block not preserved: %q", got)
	}
}
