package tui

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// TestModelInvokedMcpDedup verifies a tool the model calls twice in a turn with
// the same user-addressed result renders its rich body only once (no re-flooding
// the transcript), while still feeding the model every call.
func TestModelInvokedMcpDedup(t *testing.T) {
	res := okResult(mkContent("```\nUNIQ_SPACE_ROW 42\n```", "user"))
	end := func(id string) protocol.Event {
		return protocol.Event{Msg: protocol.EventMsg{
			Type: protocol.EventMsgKindMcpToolCallEnd,
			McpToolCallEnd: &protocol.McpToolCallEndEvent{
				CallID:     id,
				Invocation: protocol.McpInvocation{Server: "gaussdb", Tool: "space"},
				Result:     res,
			},
		}}
	}

	tr := NewChatTranscript(testTheme())
	tr = tr.applyEvent(end("c1"))
	tr = tr.applyEvent(end("c2"))
	out := tr.View(Rect{Width: 60, Height: 20})
	if n := strings.Count(out, "UNIQ_SPACE_ROW 42"); n != 1 {
		t.Fatalf("expected the report to render once after dedup, got %d:\n%s", n, out)
	}

	// A new turn resets the dedup set, so the same report can render again.
	tr = tr.applyEvent(protocol.Event{Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnStarted}})
	tr = tr.applyEvent(end("c3"))
	out = tr.View(Rect{Width: 60, Height: 40})
	if n := strings.Count(out, "UNIQ_SPACE_ROW 42"); n != 2 {
		t.Fatalf("expected a fresh render in the next turn (total 2), got %d:\n%s", n, out)
	}
}
