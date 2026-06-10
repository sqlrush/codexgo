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

// TestUserAudienceMarkdown verifies only user-addressed content is extracted for
// direct rendering, and assistant-only / untagged content is NOT (so it doesn't
// double-render alongside the model's reply).
func TestUserAudienceMarkdown(t *testing.T) {
	raw := okResult(
		mkContent("# SQL 调优报告\nbody", "user"),
		mkContent("terse digest", "assistant"),
	)
	got := userAudienceMarkdown(raw)
	if !strings.Contains(got, "# SQL 调优报告") {
		t.Errorf("user content not extracted: %q", got)
	}
	if strings.Contains(got, "terse digest") {
		t.Errorf("assistant content leaked into direct render: %q", got)
	}
}

func TestUserAudienceMarkdownNoneWhenUntagged(t *testing.T) {
	// Ordinary tool result (no audience) must render nothing here — it flows to
	// the model as usual.
	if got := userAudienceMarkdown(okResult(mkContent("legacy"))); got != "" {
		t.Errorf("expected empty for untagged content, got %q", got)
	}
	// Error result / garbage -> empty, no panic.
	if got := userAudienceMarkdown(json.RawMessage(`{"Err":"boom"}`)); got != "" {
		t.Errorf("expected empty for error result, got %q", got)
	}
	if got := userAudienceMarkdown(nil); got != "" {
		t.Errorf("expected empty for nil, got %q", got)
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
