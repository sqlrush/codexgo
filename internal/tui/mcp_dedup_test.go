package tui

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// mcpEnd builds an McpToolCallEnd event whose result carries a user-addressed
// body (the deterministic render the framework stashes for the relevance gate).
func mcpEnd(callID, tool, userBody string) protocol.Event {
	return protocol.Event{Msg: protocol.EventMsg{
		Type: protocol.EventMsgKindMcpToolCallEnd,
		McpToolCallEnd: &protocol.McpToolCallEndEvent{
			CallID:     callID,
			Invocation: protocol.McpInvocation{Server: "gaussdb", Tool: tool},
			Result:     okResult(mkContent(userBody, "user")),
		},
	}}
}

func agentMsg(text string) protocol.Event {
	return protocol.Event{Msg: protocol.EventMsg{
		Type:         protocol.EventMsgKindAgentMessage,
		AgentMessage: &protocol.AgentMessageEvent{Message: text},
	}}
}

// mcpEndReport builds an McpToolCallEnd whose body is addressed to BOTH user and
// assistant — a deliberately-invoked report (health/sqltune/wdranalyze).
func mcpEndReport(callID, tool, body string) protocol.Event {
	return protocol.Event{Msg: protocol.EventMsg{
		Type: protocol.EventMsgKindMcpToolCallEnd,
		McpToolCallEnd: &protocol.McpToolCallEndEvent{
			CallID:     callID,
			Invocation: protocol.McpInvocation{Server: "gaussdb", Tool: tool},
			Result:     okResult(mkContent(body, "user", "assistant")),
		},
	}}
}

// TestReportToolAutoShownWithoutDirective verifies a report tool (user+assistant
// audience, e.g. wdranalyze) renders immediately and is NOT gated behind @show —
// so its result never vanishes when the model omits the directive.
func TestReportToolAutoShownWithoutDirective(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	tr = tr.applyEvent(mcpEndReport("c1", "wdranalyze", "```\nWDR_REPORT_BODY\n```"))
	// Even before any agent message, the report is already on screen.
	out := tr.View(Rect{Width: 60, Height: 20})
	if !strings.Contains(out, "WDR_REPORT_BODY") {
		t.Fatalf("report tool must auto-show without @show:\n%s", out)
	}
	// A follow-up agent message with no @show must not double-render it.
	tr = tr.applyEvent(agentMsg("回滚率偏高。"))
	out = tr.View(Rect{Width: 60, Height: 30})
	if n := strings.Count(out, "WDR_REPORT_BODY"); n != 1 {
		t.Fatalf("report should render exactly once, got %d:\n%s", n, out)
	}
}

// TestRelevanceGateShowsOnlyDeclared verifies tool tables are NOT auto-rendered;
// only the tools the model declares via "@show:" render, and the directive line
// is stripped from the displayed analysis.
func TestRelevanceGateShowsOnlyDeclared(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	tr = tr.applyEvent(mcpEnd("c1", "ash", "```\nASH_BODY_ROW\n```"))
	tr = tr.applyEvent(mcpEnd("c2", "hotkey", "```\nHOTKEY_BODY_ROW\n```"))
	// Nothing rendered yet — only chips would show (no McpDirectCell bodies).
	mid := tr.View(Rect{Width: 60, Height: 20})
	if strings.Contains(mid, "ASH_BODY_ROW") || strings.Contains(mid, "HOTKEY_BODY_ROW") {
		t.Fatalf("tool bodies must not auto-render before @show:\n%s", mid)
	}
	tr = tr.applyEvent(agentMsg("无明显等待事件。\n@show: ash"))
	out := tr.View(Rect{Width: 60, Height: 30})
	if strings.Count(out, "ASH_BODY_ROW") != 1 {
		t.Errorf("declared tool ash should render exactly once:\n%s", out)
	}
	if strings.Contains(out, "HOTKEY_BODY_ROW") {
		t.Errorf("undeclared tool hotkey must not render:\n%s", out)
	}
	if strings.Contains(out, "@show") {
		t.Errorf("the @show directive must be stripped from display:\n%s", out)
	}
	if !strings.Contains(out, "无明显等待事件") {
		t.Errorf("analysis text missing:\n%s", out)
	}
}

// TestRelevanceGateSingleToolFallback: a turn that stashed exactly one tool and
// declared nothing still shows it (unambiguous single-tool answer).
func TestRelevanceGateSingleToolFallback(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	tr = tr.applyEvent(mcpEnd("c1", "space", "```\nSPACE_ONLY_ROW\n```"))
	tr = tr.applyEvent(agentMsg("当前有 3 个库。"))
	out := tr.View(Rect{Width: 60, Height: 20})
	if !strings.Contains(out, "SPACE_ONLY_ROW") {
		t.Errorf("single stashed tool should render without @show:\n%s", out)
	}
}

// TestRelevanceGateNoFallbackWhenMany: multiple tools stashed, no @show → render
// nothing (cannot guess relevance).
func TestRelevanceGateNoFallbackWhenMany(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	tr = tr.applyEvent(mcpEnd("c1", "ash", "```\nASH_X\n```"))
	tr = tr.applyEvent(mcpEnd("c2", "space", "```\nSPACE_X\n```"))
	tr = tr.applyEvent(agentMsg("综述。"))
	out := tr.View(Rect{Width: 60, Height: 20})
	if strings.Contains(out, "ASH_X") || strings.Contains(out, "SPACE_X") {
		t.Errorf("nothing should render when many tools and no @show:\n%s", out)
	}
}

// TestRelevanceGateDedup: declaring the same tool twice renders it once.
func TestRelevanceGateDedup(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	tr = tr.applyEvent(mcpEnd("c1", "ash", "```\nASH_DEDUP\n```"))
	tr = tr.applyEvent(agentMsg("x\n@show: ash, ash"))
	out := tr.View(Rect{Width: 60, Height: 20})
	if n := strings.Count(out, "ASH_DEDUP"); n != 1 {
		t.Errorf("expected one render after dedup, got %d:\n%s", n, out)
	}
}

func TestExtractShowDirective(t *testing.T) {
	cases := []struct {
		in    string
		names []string
	}{
		{"分析文本\n@show: ash, lwlocks", []string{"ash", "lwlocks"}},
		{"x\n@show：ash、sessions", []string{"ash", "sessions"}},  // full-width colon + 、
		{"y\n[[show: mcp__gaussdb__space]]", []string{"space"}}, // bracket + qualified name
		{"z\n> @show: /hotkey", []string{"hotkey"}},             // quote-prefixed, slash
		{"no directive here", nil},
		{"a line with show: but no at-marker", nil},
	}
	for _, c := range cases {
		_, got := extractShowDirective(c.in)
		if strings.Join(got, ",") != strings.Join(c.names, ",") {
			t.Errorf("extractShowDirective(%q) = %v, want %v", c.in, got, c.names)
		}
	}
}
