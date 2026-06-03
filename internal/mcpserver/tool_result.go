package mcpserver

import "encoding/json"

// contentBlock is one MCP content block. The MCP server only emits text blocks.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// textContent builds a single text content block.
func textContent(text string) contentBlock {
	return contentBlock{Type: "text", Text: text}
}

// callToolResult is the MCP tools/call response payload. It carries content
// blocks, an optional isError flag, and (for clients that prefer it)
// structuredContent mirroring the text alongside the threadId. Mirrors rmcp's
// CallToolResult.
type callToolResult struct {
	Content           []contentBlock  `json:"content"`
	IsError           *bool           `json:"isError,omitempty"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
}

// errorCallToolResult builds a CallToolResult flagged as an error with a single
// text block. Mirrors CallToolResult::error.
func errorCallToolResult(text string) callToolResult {
	isErr := true
	return callToolResult{
		Content: []contentBlock{textContent(text)},
		IsError: &isErr,
	}
}

// callToolResultWithThreadID builds a CallToolResult whose structuredContent
// mirrors the text content alongside the threadId. It is the faithful port of
// create_call_tool_result_with_thread_id: some MCP clients ignore content when
// structuredContent is present, so the text is mirrored into both.
func callToolResultWithThreadID(threadID, text string, isError *bool) callToolResult {
	structured, _ := json.Marshal(map[string]any{
		"threadId": threadID,
		"content":  text,
	})
	return callToolResult{
		Content:           []contentBlock{textContent(text)},
		IsError:           isError,
		StructuredContent: structured,
	}
}
