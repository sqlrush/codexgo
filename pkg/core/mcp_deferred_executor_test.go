package core

// Tests for the deferred (defer_loading) MCP tool runtimes, porting the deferred
// branch of spec_plan::add_mcp_runtime_tools + McpHandler::search_info: deferred
// MCP tools are dispatch-only (hidden spec), surface through tool_search for a
// matching query as a defer_loading namespace spec, and render their per-server
// source into the tool_search description (deduplicated alongside the collab
// sources).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// calendarMcpToolInfo mirrors the tools-package fixture: a calendar tool on the
// codex-apps server, namespaced under mcp__calendar__.
func calendarMcpToolInfo() tools.McpToolInfo {
	title := "Create event"
	description := "Create a calendar event."
	namespaceDescription := "Plan events."
	connectorName := "Calendar"
	inputSchema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"start_time": map[string]any{"type": "string"},
			"attendees":  map[string]any{"type": "string"},
		},
	})
	return tools.McpToolInfo{
		ServerName:           "codex-apps",
		CallableName:         "_create_event",
		CallableNamespace:    "mcp__calendar__",
		NamespaceDescription: &namespaceDescription,
		ConnectorName:        &connectorName,
		PluginDisplayNames:   []string{"Calendar plugin"},
		Tool: protocol.Tool{
			Name:        "createEvent",
			Title:       &title,
			Description: &description,
			InputSchema: inputSchema,
		},
	}
}

// deferredMcpRouter builds a router with the calendar tool registered as a
// deferred MCP runtime.
func deferredMcpRouter(t *testing.T) *DefaultToolRouter {
	t.Helper()
	router, err := BuiltinToolRouter(BuiltinToolDeps{
		Mcp:              &mockMcpCaller{},
		DeferredMcpTools: []tools.McpToolInfo{calendarMcpToolInfo()},
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	return router
}

// TestDeferredMcpToolNotAdvertisedDirectly asserts a deferred MCP tool is never
// in the model-visible spec list (it is dispatch-only / ToolExposure::Deferred).
func TestDeferredMcpToolNotAdvertisedDirectly(t *testing.T) {
	router := deferredMcpRouter(t)
	names := strings.Join(specNames(t, router, newSearchToolTurn(t)), ",")
	if strings.Contains(names, "mcp__calendar__") {
		t.Errorf("deferred MCP tool should not be advertised directly; names = %s", names)
	}
	// tool_search must still advertise (the deferred MCP source is present).
	if !strings.Contains(names, "tool_search") {
		t.Errorf("tool_search should advertise with a deferred MCP source; names = %s", names)
	}
}

// runToolSearch dispatches a tool_search call and returns the matched
// LoadableToolSpec JSON values.
func runToolSearch(t *testing.T, router *DefaultToolRouter, turn *TurnContext, query string) []json.RawMessage {
	t.Helper()
	res, err := router.dispatch(context.Background(), nil, turn, "call-search",
		protocol.PlainToolName(tools.ToolSearchToolName),
		tools.ToolSearchPayload(tools.SearchToolCallParams{Query: query}))
	if err != nil {
		t.Fatalf("dispatch tool_search: %v", err)
	}
	out, ok := res.Output.(toolSearchOutput)
	if !ok {
		t.Fatalf("output is %T, want toolSearchOutput", res.Output)
	}
	return out.tools
}

// TestDeferredMcpToolAppearsInToolSearch asserts a matching query returns the
// deferred MCP tool as a defer_loading namespace spec with the output schema
// stripped from the inner function tool.
func TestDeferredMcpToolAppearsInToolSearch(t *testing.T) {
	router := deferredMcpRouter(t)
	specs := runToolSearch(t, router, newSearchToolTurn(t), "calendar createEvent")

	var calendar map[string]any
	for _, raw := range specs {
		var spec map[string]any
		if err := json.Unmarshal(raw, &spec); err != nil {
			t.Fatalf("decode spec: %v", err)
		}
		if spec["type"] == "namespace" && spec["name"] == "mcp__calendar__" {
			calendar = spec
			break
		}
	}
	if calendar == nil {
		t.Fatalf("deferred MCP namespace not returned for matching query; got %d specs", len(specs))
	}

	inner, _ := calendar["tools"].([]any)
	if len(inner) != 1 {
		t.Fatalf("expected one inner tool, got %d", len(inner))
	}
	tool, _ := inner[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("inner tool type = %v, want function", tool["type"])
	}
	if defer_, ok := tool["defer_loading"].(bool); !ok || !defer_ {
		t.Errorf("defer_loading = %v, want true", tool["defer_loading"])
	}
	if _, present := tool["output_schema"]; present {
		t.Errorf("output_schema should be stripped from the deferred spec")
	}
}

// TestDeferredMcpSourceRendersIntoDescription asserts the deferred MCP tool's
// per-server source (Calendar) renders into the tool_search description
// alongside the multi-agent source, deduplicated and name-sorted.
func TestDeferredMcpSourceRendersIntoDescription(t *testing.T) {
	router := deferredMcpRouter(t)
	specs, err := router.SpecsForTurn(context.Background(), newSearchToolTurn(t))
	if err != nil {
		t.Fatalf("SpecsForTurn: %v", err)
	}
	var description string
	for _, spec := range specs {
		if spec.Kind == tools.ToolSpecKindToolSearch {
			description = spec.ToolSearch.Description
			break
		}
	}
	if description == "" {
		t.Fatal("tool_search spec not found")
	}
	if !strings.Contains(description, "- Calendar: Plan events.") {
		t.Errorf("description missing Calendar source line:\n%s", description)
	}
	if !strings.Contains(description, "- Multi-agent tools: Spawn and manage sub-agents.") {
		t.Errorf("description missing multi-agent source line:\n%s", description)
	}
	// Sources are name-sorted: "Calendar" precedes "Multi-agent tools".
	if strings.Index(description, "- Calendar") > strings.Index(description, "- Multi-agent tools") {
		t.Errorf("sources not name-sorted (Calendar should precede Multi-agent tools):\n%s", description)
	}
}

// TestDeferredMcpSourceDedup asserts two deferred MCP tools sharing the same
// connector contribute a single deduplicated source line.
func TestDeferredMcpSourceDedup(t *testing.T) {
	first := calendarMcpToolInfo()
	second := calendarMcpToolInfo()
	second.CallableName = "_delete_event"
	second.Tool.Name = "deleteEvent"

	router, err := BuiltinToolRouter(BuiltinToolDeps{
		Mcp:              &mockMcpCaller{},
		DeferredMcpTools: []tools.McpToolInfo{first, second},
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	specs, err := router.SpecsForTurn(context.Background(), newSearchToolTurn(t))
	if err != nil {
		t.Fatalf("SpecsForTurn: %v", err)
	}
	var description string
	for _, spec := range specs {
		if spec.Kind == tools.ToolSpecKindToolSearch {
			description = spec.ToolSearch.Description
		}
	}
	if n := strings.Count(description, "- Calendar:"); n != 1 {
		t.Errorf("Calendar source appeared %d times, want 1 (deduplicated):\n%s", n, description)
	}
}

// TestDeferredMcpToolDispatchesThroughCaller asserts a loaded deferred MCP tool
// routes a direct call through the injected MCP caller (the same path the eager
// mcpExecutor uses).
func TestDeferredMcpToolDispatchesThroughCaller(t *testing.T) {
	caller := &mockMcpCaller{result: protocol.CallToolResult{
		Content: []json.RawMessage{json.RawMessage(`{"type":"text","text":"ok"}`)},
	}}
	router, err := BuiltinToolRouter(BuiltinToolDeps{
		Mcp:              caller,
		DeferredMcpTools: []tools.McpToolInfo{calendarMcpToolInfo()},
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	_, err = router.dispatch(context.Background(), nil, newSearchToolTurn(t), "call-mcp",
		protocol.NamespacedToolName("mcp__calendar__", "_create_event"),
		tools.FunctionPayload(`{"start_time":"now"}`))
	if err != nil {
		t.Fatalf("dispatch deferred MCP tool: %v", err)
	}
	if caller.gotQN != "mcp__calendar___create_event" {
		t.Errorf("caller invoked %q, want mcp__calendar___create_event", caller.gotQN)
	}
}
