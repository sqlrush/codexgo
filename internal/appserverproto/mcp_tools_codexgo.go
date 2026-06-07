package appserverproto

import "encoding/json"

// codexgo-specific app-server methods for the deterministic "slash → tool call"
// entry: list the connected MCP tools, and invoke one directly (no LLM turn).
// These are NOT part of the upstream codex protocol; they are registered here so
// the TUI can drive plugin tools deterministically. Registering them in the open
// method registry does not affect the codex method set / parity.

func init() {
	Register(MethodSpec{
		Method:    "mcp/listTools",
		NewParams: func() any { return new(McpListToolsParams) },
		NewResult: func() any { return new(McpListToolsResponse) },
	})
	Register(MethodSpec{
		Method:    "mcp/callTool",
		NewParams: func() any { return new(McpCallToolParams) },
		NewResult: func() any { return new(McpCallToolResponse) },
	})
}

// McpListToolsParams has no fields; it lists every connected MCP tool.
type McpListToolsParams struct{}

// McpToolDescriptor is one connected MCP tool, model-visible name plus the
// canonical "mcp__<server>__<tool>" used to invoke it.
type McpToolDescriptor struct {
	// QualifiedName is the canonical dispatch name ("mcp__<server>__<tool>").
	QualifiedName string `json:"qualified_name"`
	// Server is the MCP server name.
	Server string `json:"server"`
	// Tool is the raw tool name (the model-visible name).
	Tool string `json:"tool"`
	// Description is the tool's human description, if any.
	Description string `json:"description,omitempty"`
	// InputSchema is the tool's raw JSON Schema for arguments, if any.
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// McpListToolsResponse returns the connected tools sorted by qualified name.
type McpListToolsResponse struct {
	Tools []McpToolDescriptor `json:"tools"`
}

// McpCallToolParams invokes one MCP tool deterministically.
type McpCallToolParams struct {
	// QualifiedName is the canonical "mcp__<server>__<tool>" name.
	QualifiedName string `json:"qualified_name"`
	// Arguments is the tool's JSON arguments object (may be empty/null).
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// McpCallToolResponse carries the tool result, flattened for display: Text is
// the concatenated text content blocks; IsError mirrors the MCP isError flag.
type McpCallToolResponse struct {
	IsError bool   `json:"is_error"`
	Text    string `json:"text"`
}
