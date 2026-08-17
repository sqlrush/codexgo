package tools

// Tests for the tool_search spec builder, mirroring the Rust
// `tool_search_spec.rs` unit test (source dedup + sorted rendering) plus the
// bare-run spec captured from codex 0.136.0 (multi-agent source only).

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCreateToolSearchToolDeduplicatesAndRendersEnabledSources mirrors the Rust
// `create_tool_search_tool_deduplicates_and_renders_enabled_sources`.
func TestCreateToolSearchToolDeduplicatesAndRendersEnabledSources(t *testing.T) {
	spec := CreateToolSearchTool([]ToolSearchSourceInfo{
		{Name: "Google Drive", Description: strPtr("Use Google Drive as the single entrypoint for Drive, Docs, Sheets, and Slides work.")},
		{Name: "Google Drive", Description: nil},
		{Name: "docs", Description: nil},
	}, 8)

	if spec.Kind != ToolSpecKindToolSearch || spec.ToolSearch == nil {
		t.Fatalf("spec kind = %v, want tool_search", spec.Kind)
	}
	if spec.ToolSearch.Execution != "client" {
		t.Errorf("execution = %q, want client", spec.ToolSearch.Execution)
	}
	wantDescription := "# Tool discovery\n\nSearches over deferred tool metadata with BM25 and exposes matching tools for the next model call.\n\nYou have access to tools from the following sources:\n- Google Drive: Use Google Drive as the single entrypoint for Drive, Docs, Sheets, and Slides work.\n- docs\nSome of the tools may not have been provided to you upfront, and you should use this tool (`tool_search`) to search for the required tools. For MCP tool discovery, always use `tool_search` instead of `list_mcp_resources` or `list_mcp_resource_templates`."
	if spec.ToolSearch.Description != wantDescription {
		t.Errorf("description mismatch\n got:  %q\n want: %q", spec.ToolSearch.Description, wantDescription)
	}
}

// TestCreateToolSearchToolEmptySources renders the no-sources placeholder.
func TestCreateToolSearchToolEmptySources(t *testing.T) {
	spec := CreateToolSearchTool(nil, 8)
	want := "You have access to tools from the following sources:\nNone currently enabled.\n"
	if got := spec.ToolSearch.Description; !strings.Contains(got, want) {
		t.Errorf("description %q does not contain %q", got, want)
	}
}

// TestCreateToolSearchToolWireBytes locks the serialized bare-run spec to the
// bytes captured from codex 0.136.0 (multi-agent deferred source, limit 8).
func TestCreateToolSearchToolWireBytes(t *testing.T) {
	spec := CreateToolSearchTool([]ToolSearchSourceInfo{
		{Name: "Multi-agent tools", Description: strPtr("Spawn and manage sub-agents.")},
	}, 8)

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Canonicalize for field-order-independent comparison.
	var got, want any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode got: %v", err)
	}
	captured := `{
  "description": "# Tool discovery\n\nSearches over deferred tool metadata with BM25 and exposes matching tools for the next model call.\n\nYou have access to tools from the following sources:\n- Multi-agent tools: Spawn and manage sub-agents.\nSome of the tools may not have been provided to you upfront, and you should use this tool (` + "`tool_search`" + `) to search for the required tools. For MCP tool discovery, always use ` + "`tool_search`" + ` instead of ` + "`list_mcp_resources`" + ` or ` + "`list_mcp_resource_templates`" + `.",
  "execution": "client",
  "parameters": {
    "additionalProperties": false,
    "properties": {
      "limit": {
        "description": "Maximum number of tools to return. Defaults to 8.",
        "type": "number"
      },
      "query": {
        "description": "Search query for deferred tools.",
        "type": "string"
      }
    },
    "required": ["query"],
    "type": "object"
  },
  "type": "tool_search"
}`
	if err := json.Unmarshal([]byte(captured), &want); err != nil {
		t.Fatalf("decode want: %v", err)
	}
	gotC, _ := json.Marshal(got)
	wantC, _ := json.Marshal(want)
	if string(gotC) != string(wantC) {
		t.Errorf("wire bytes mismatch\n got:  %s\n want: %s", gotC, wantC)
	}
}
