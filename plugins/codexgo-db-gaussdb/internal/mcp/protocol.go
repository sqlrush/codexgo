// Package mcp is a minimal Model Context Protocol server over stdio
// (newline-delimited JSON-RPC 2.0). It implements just the methods a tool
// server needs: initialize, tools/list, tools/call. It deliberately depends on
// nothing but the standard library so the plugin stays small and fully under
// our control (we do not bet on a third-party MCP SDK's evolving API).
package mcp

import "encoding/json"

// jsonrpcRequest is an incoming JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonrpcResponse is an outgoing JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcError is a JSON-RPC 2.0 error object.
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC standard error codes used here.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInternalError  = -32603
)

// --- MCP message shapes (subset) ------------------------------------------

// initializeResult is returned from initialize. Instructions carries the
// server-level domain knowledge codexgo's MCP client surfaces to the LLM
// (PLUGIN-DB-DESIGN: knowledge via MCP instructions/description, not SKILL.md).
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
	Instructions    string         `json:"instructions,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool is one MCP tool advertised in tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type listToolsResult struct {
	Tools []Tool `json:"tools"`
}

// callToolParams is the params of a tools/call request.
type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// CallToolResult is the result of a tools/call. Content is the MCP content
// array; codexgo renders it. We return one structured-JSON text item plus
// optional plain summary, so codexgo can render rich UI from the structured
// data (the key optimization over opendb's server-side pre-rendered text).
type CallToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem is one MCP content block (text variant).
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// TextContent builds a text content item.
func TextContent(s string) ContentItem { return ContentItem{Type: "text", Text: s} }
