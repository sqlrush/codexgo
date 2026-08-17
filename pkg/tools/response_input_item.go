package tools

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ResponseInputItemKind discriminates a ResponseInputItem. Mirrors the Rust enum
// variants (`#[serde(tag = "type", rename_all = "snake_case")]`).
//
// This type lives in the tools package because the protocol package ports only
// the read-side ResponseItem enum, while tool outputs construct the input-side
// items the model receives back. The wire shapes match the Rust
// `ResponseInputItem` enum exactly.
type ResponseInputItemKind int

// ResponseInputItemKind variants.
const (
	// ResponseInputItemKindMessage is a role-tagged message ("message").
	ResponseInputItemKindMessage ResponseInputItemKind = iota
	// ResponseInputItemKindFunctionCallOutput is a function call result.
	ResponseInputItemKindFunctionCallOutput
	// ResponseInputItemKindMcpToolCallOutput is an MCP tool call result.
	ResponseInputItemKindMcpToolCallOutput
	// ResponseInputItemKindCustomToolCallOutput is a custom tool call result.
	ResponseInputItemKindCustomToolCallOutput
	// ResponseInputItemKindToolSearchOutput is a tool_search result.
	ResponseInputItemKindToolSearchOutput
)

// ResponseInputItem is an input-side conversation item produced by a tool
// runtime. Mirrors Rust `ResponseInputItem`.
type ResponseInputItem struct {
	Kind ResponseInputItemKind

	// Message variant.
	Role    string
	Content []protocol.ContentItem
	Phase   *protocol.MessagePhase

	// Shared call id (FunctionCallOutput / McpToolCallOutput /
	// CustomToolCallOutput / ToolSearchOutput).
	CallID string

	// FunctionCallOutput / CustomToolCallOutput variant.
	Output protocol.FunctionCallOutputPayload

	// McpToolCallOutput variant.
	McpOutput protocol.CallToolResult

	// CustomToolCallOutput variant (optional name).
	Name *string

	// ToolSearchOutput variant.
	Status    string
	Execution string
	Tools     []json.RawMessage
}

// MessageInput constructs a Message ResponseInputItem.
func MessageInput(role string, content []protocol.ContentItem, phase *protocol.MessagePhase) ResponseInputItem {
	return ResponseInputItem{
		Kind:    ResponseInputItemKindMessage,
		Role:    role,
		Content: content,
		Phase:   phase,
	}
}

// FunctionCallOutputInput constructs a FunctionCallOutput ResponseInputItem.
func FunctionCallOutputInput(callID string, output protocol.FunctionCallOutputPayload) ResponseInputItem {
	return ResponseInputItem{
		Kind:   ResponseInputItemKindFunctionCallOutput,
		CallID: callID,
		Output: output,
	}
}

// McpToolCallOutputInput constructs an McpToolCallOutput ResponseInputItem.
func McpToolCallOutputInput(callID string, output protocol.CallToolResult) ResponseInputItem {
	return ResponseInputItem{
		Kind:      ResponseInputItemKindMcpToolCallOutput,
		CallID:    callID,
		McpOutput: output,
	}
}

// CustomToolCallOutputInput constructs a CustomToolCallOutput ResponseInputItem.
func CustomToolCallOutputInput(callID string, name *string, output protocol.FunctionCallOutputPayload) ResponseInputItem {
	return ResponseInputItem{
		Kind:   ResponseInputItemKindCustomToolCallOutput,
		CallID: callID,
		Name:   name,
		Output: output,
	}
}

// ToolSearchOutputInput constructs a ToolSearchOutput ResponseInputItem.
func ToolSearchOutputInput(callID, status, execution string, tools []json.RawMessage) ResponseInputItem {
	return ResponseInputItem{
		Kind:      ResponseInputItemKindToolSearchOutput,
		CallID:    callID,
		Status:    status,
		Execution: execution,
		Tools:     tools,
	}
}

// MarshalJSON emits the internally-tagged wire shape with snake_case variant
// tags, matching the Rust serde output.
func (r ResponseInputItem) MarshalJSON() ([]byte, error) {
	m := orderedMap{}
	switch r.Kind {
	case ResponseInputItemKindMessage:
		m.set("type", "message")
		m.set("role", r.Role)
		m.set("content", r.Content)
		if r.Phase != nil {
			m.set("phase", *r.Phase)
		}
	case ResponseInputItemKindFunctionCallOutput:
		m.set("type", "function_call_output")
		m.set("call_id", r.CallID)
		m.set("output", r.Output)
	case ResponseInputItemKindMcpToolCallOutput:
		m.set("type", "mcp_tool_call_output")
		m.set("call_id", r.CallID)
		m.set("output", r.McpOutput)
	case ResponseInputItemKindCustomToolCallOutput:
		m.set("type", "custom_tool_call_output")
		m.set("call_id", r.CallID)
		if r.Name != nil {
			m.set("name", *r.Name)
		}
		m.set("output", r.Output)
	case ResponseInputItemKindToolSearchOutput:
		m.set("type", "tool_search_output")
		m.set("call_id", r.CallID)
		m.set("status", r.Status)
		m.set("execution", r.Execution)
		m.set("tools", r.Tools)
	default:
		return nil, fmt.Errorf("unknown ResponseInputItem kind: %d", r.Kind)
	}
	return m.MarshalJSON()
}
