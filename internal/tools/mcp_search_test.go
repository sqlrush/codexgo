package tools

// Ports codex-rs/core/src/tools/handlers/mcp_search_tests.rs: the deferred MCP
// tool's search text uses the tool metadata + sorted parameter names, and the
// source info / output namespace description follow the connector-name fallback.

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// calendarToolInfo mirrors the Rust mcp_search_tests `tool_info()` fixture: the
// calendar createEvent tool on the codex-apps server under the mcp__calendar__
// namespace, with a connector name and plugin display names (one padded, one
// blank).
func calendarToolInfo(t *testing.T) McpToolInfo {
	t.Helper()
	title := "Create event"
	description := "Create a calendar event."
	namespaceDescription := "Plan events."
	connectorName := "Calendar"
	inputSchema, err := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"start_time": map[string]any{"type": "string"},
			"attendees":  map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	})
	if err != nil {
		t.Fatalf("marshal input schema: %v", err)
	}
	return McpToolInfo{
		ServerName:           "codex-apps",
		CallableName:         "_create_event",
		CallableNamespace:    "mcp__calendar__",
		NamespaceDescription: &namespaceDescription,
		ConnectorName:        &connectorName,
		PluginDisplayNames:   []string{" Calendar plugin ", " "},
		Tool: protocol.Tool{
			Name:        "createEvent",
			Title:       &title,
			Description: &description,
			InputSchema: inputSchema,
		},
	}
}

// TestMcpSearchInfoUsesMetadataAndParameterNames mirrors the Rust
// `search_info_uses_mcp_tool_metadata_and_parameter_names`.
func TestMcpSearchInfoUsesMetadataAndParameterNames(t *testing.T) {
	info, ok := McpToolSearchInfo(calendarToolInfo(t))
	if !ok {
		t.Fatal("McpToolSearchInfo returned not-ok for a function/namespace spec")
	}

	wantText := "mcp__calendar___create_event _create_event createEvent codex-apps Create event Create a calendar event. Calendar Plan events. Calendar plugin attendees start_time"
	if info.Entry.SearchText != wantText {
		t.Errorf("search text mismatch\n got: %q\nwant: %q", info.Entry.SearchText, wantText)
	}

	if info.SourceInfo == nil {
		t.Fatal("expected non-nil source info")
	}
	if info.SourceInfo.Name != "Calendar" {
		t.Errorf("source name = %q, want Calendar", info.SourceInfo.Name)
	}
	if info.SourceInfo.Description == nil || *info.SourceInfo.Description != "Plan events." {
		t.Errorf("source description = %v, want \"Plan events.\"", info.SourceInfo.Description)
	}
}

// TestMcpSearchInfoUsesConnectorNameForNamespaceDescription mirrors the Rust
// `search_info_uses_connector_name_for_output_namespace_description`: with no
// namespace description, the output namespace description falls back to the
// connector name and the source description becomes nil.
func TestMcpSearchInfoUsesConnectorNameForNamespaceDescription(t *testing.T) {
	fixture := calendarToolInfo(t)
	fixture.NamespaceDescription = nil

	info, ok := McpToolSearchInfo(fixture)
	if !ok {
		t.Fatal("McpToolSearchInfo returned not-ok")
	}

	if info.Entry.Output.Kind != LoadableToolSpecKindNamespace {
		t.Fatalf("expected namespace output, got kind %d", info.Entry.Output.Kind)
	}
	if got := info.Entry.Output.Namespace.Description; got != "Tools for working with Calendar." {
		t.Errorf("namespace description = %q, want %q", got, "Tools for working with Calendar.")
	}
	if info.SourceInfo == nil {
		t.Fatal("expected non-nil source info")
	}
	if info.SourceInfo.Name != "Calendar" {
		t.Errorf("source name = %q, want Calendar", info.SourceInfo.Name)
	}
	if info.SourceInfo.Description != nil {
		t.Errorf("source description = %v, want nil", info.SourceInfo.Description)
	}
}

// TestMcpDeferredSpecStripsOutputSchema asserts the deferred loadable spec marks
// defer_loading=true and strips the output schema on the inner function tool,
// mirroring ToolSearchInfo::from_spec's namespace arm.
func TestMcpDeferredSpecStripsOutputSchema(t *testing.T) {
	info, ok := McpToolSearchInfo(calendarToolInfo(t))
	if !ok {
		t.Fatal("McpToolSearchInfo returned not-ok")
	}
	ns := info.Entry.Output.Namespace
	if ns == nil || len(ns.Tools) != 1 {
		t.Fatalf("expected one namespace tool, got %+v", ns)
	}
	tool := ns.Tools[0].Function
	if tool.DeferLoading == nil || !*tool.DeferLoading {
		t.Errorf("defer_loading = %v, want true", tool.DeferLoading)
	}
	if tool.OutputSchema != nil {
		t.Errorf("output schema = %v, want nil (stripped)", tool.OutputSchema)
	}
}
