package core

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// TestMcpOutputCarriedToHistory guards the bug where an MCP tool result was
// dropped during history conversion (responseItemFromInput used in.Output, which
// is empty for MCP items, instead of in.McpOutput), so the model saw an empty
// tool result — especially on the chat-completions wire.
func TestMcpOutputCarriedToHistory(t *testing.T) {
	res := protocol.CallToolResult{
		Content: []json.RawMessage{
			json.RawMessage(`{"type":"text","text":"{\"score\":100}"}`),
			json.RawMessage(`{"type":"image","data":"ignored"}`),
		},
	}
	in := tools.McpToolCallOutputInput("call-1", res)

	item := responseItemFromInput(in)
	if item.Type != protocol.ResponseItemKindFunctionCallOutput {
		t.Fatalf("type = %v, want function_call_output", item.Type)
	}
	if item.Output == nil || item.Output.Text == nil {
		t.Fatal("output text is nil — MCP content was dropped")
	}
	if *item.Output.Text != `{"score":100}` {
		t.Errorf("output text = %q, want the MCP text block", *item.Output.Text)
	}
}

// TestMcpOutputReachesChatWire verifies the flattened MCP result becomes a
// non-empty "tool" message on the chat-completions wire (the glm/deepseek path).
func TestMcpOutputReachesChatWire(t *testing.T) {
	res := protocol.CallToolResult{
		Content: []json.RawMessage{json.RawMessage(`{"type":"text","text":"HEALTH_REPORT"}`)},
	}
	item := responseItemFromInput(tools.McpToolCallOutputInput("call-9", res))

	msgs := chatMessagesFromItems([]protocol.ResponseItem{item})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 chat message, got %d", len(msgs))
	}
	if msgs[0].Role != "tool" || msgs[0].ToolCallID != "call-9" {
		t.Errorf("message = %+v, want tool/call-9", msgs[0])
	}
	if msgs[0].Content == nil || *msgs[0].Content != "HEALTH_REPORT" {
		t.Errorf("chat tool content = %v, want HEALTH_REPORT", msgs[0].Content)
	}
}

func TestMcpResultTextFlattens(t *testing.T) {
	res := protocol.CallToolResult{Content: []json.RawMessage{
		json.RawMessage(`{"type":"text","text":"a"}`),
		json.RawMessage(`{"type":"text","text":"b"}`),
	}}
	if got := mcpResultText(res); got != "a\nb" {
		t.Errorf("mcpResultText = %q, want a\\nb", got)
	}
}
