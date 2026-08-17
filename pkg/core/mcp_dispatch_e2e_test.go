package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// TestMcpFunctionCallResultReachesModel is the end-to-end guard for the bug
// where MCP tools returned empty to the model: a model-emitted function call to
// an MCP tool ("health") must flow through the real dispatch path
// (router.Dispatch -> AnyToolResult.ToToolResult -> toolOutputText) carrying the
// tool's content, then through functionCallOutputItem + the chat-completions
// converter as a non-empty tool message.
func TestMcpFunctionCallResultReachesModel(t *testing.T) {
	res := protocol.CallToolResult{
		Content: []json.RawMessage{
			json.RawMessage(`{"type":"text","text":"{\"score\":100,\"grade\":\"优\"}"}`),
		},
	}
	caller := &mockMcpCaller{result: res}
	router, err := BuiltinToolRouter(BuiltinToolDeps{
		Mcp: caller,
		McpTools: []tools.McpToolInfo{{
			ServerName:        "gaussdb",
			CallableName:      "health",
			CallableNamespace: "mcp__gaussdb__",
			Tool:              protocol.Tool{Name: "health"},
		}},
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}

	// The model calls the tool by its model-visible name "health".
	result, err := router.Dispatch(context.Background(), newTestTurn("."), ToolInvocation{
		CallID: "c1", Name: protocol.PlainToolName("health"), Arguments: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	want := `{"score":100,"grade":"优"}`
	if result.Output != want {
		t.Fatalf("ToolResult.Output = %q, want %q (was empty before the toolOutputText fix)", result.Output, want)
	}
	// Routed to the owning server via the canonical name.
	if caller.gotQN != "mcp__gaussdb__health" {
		t.Errorf("caller invoked %q, want mcp__gaussdb__health", caller.gotQN)
	}

	// Full chain to the chat wire: function_call_output -> non-empty tool message.
	item := functionCallOutputItem("c1", result)
	msgs := chatMessagesFromItems([]protocol.ResponseItem{item})
	if len(msgs) != 1 || msgs[0].Role != "tool" || msgs[0].Content == nil || *msgs[0].Content != want {
		t.Fatalf("chat tool message wrong: %+v", msgs)
	}
}

// TestDispatchFunctionCallRecordsMcpOutput exercises the EXACT live turn
// function (dispatchFunctionCall, session-aware path) to confirm a model
// function call to an MCP tool records a non-empty function_call_output.
func TestDispatchFunctionCallRecordsMcpOutput(t *testing.T) {
	sess, _ := newTestSession(t)
	router, err := BuiltinToolRouter(BuiltinToolDeps{
		Mcp: &mockMcpCaller{result: protocol.CallToolResult{
			Content: []json.RawMessage{json.RawMessage(`{"type":"text","text":"LIVE_HEALTH_OK"}`)},
		}},
		McpTools: []tools.McpToolInfo{{
			ServerName: "gaussdb", CallableName: "health",
			CallableNamespace: "mcp__gaussdb__", Tool: protocol.Tool{Name: "health"},
		}},
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	sess.services.ToolRouter = router

	var (
		toolOutputs   []protocol.ResponseItem
		needsFollowUp bool
		lastMsg       *string
		activeItem    *protocol.ResponseItem
		emitTokens    bool
		tokenUsage    *protocol.TokenUsage
	)
	st := &streamState{
		needsFollowUp: &needsFollowUp, toolOutputs: &toolOutputs,
		lastAgentMessage: &lastMsg, activeItem: &activeItem,
		shouldEmitTokens: &emitTokens, completedTokenUsage: &tokenUsage,
	}
	call := protocol.ResponseItem{
		Type: protocol.ResponseItemKindFunctionCall, Name: "health",
		CallID: "c1", Arguments: "{}",
	}
	if err := dispatchFunctionCall(context.Background(), sess, newTestTurn("."), call, st); err != nil {
		t.Fatalf("dispatchFunctionCall: %v", err)
	}
	if len(toolOutputs) != 1 {
		t.Fatalf("expected 1 recorded output, got %d", len(toolOutputs))
	}
	out := toolOutputs[0]
	if out.Type != protocol.ResponseItemKindFunctionCallOutput || out.Output == nil ||
		out.Output.Text == nil || *out.Output.Text != "LIVE_HEALTH_OK" {
		t.Fatalf("recorded output empty/wrong (the live bug): %+v", out)
	}
}
