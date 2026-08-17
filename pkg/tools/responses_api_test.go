package tools

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestToolDefinitionToResponsesApiToolOmitsFalseDeferLoading(t *testing.T) {
	def := ToolDefinition{
		Name:        "lookup_order",
		Description: "Look up an order",
		InputSchema: ObjectSchema(
			map[string]JsonSchema{"order_id": StringSchema(nil)},
			[]string{"order_id"},
			BoolAdditionalProperties(false),
		),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		DeferLoading: false,
	}
	got := ToolDefinitionToResponsesApiTool(def)
	if got.DeferLoading != nil {
		t.Fatalf("expected nil defer_loading, got %v", *got.DeferLoading)
	}
	if got.Strict {
		t.Fatalf("expected strict=false")
	}
	if string(got.OutputSchema) != `{"type":"object"}` {
		t.Fatalf("output schema mismatch: %s", got.OutputSchema)
	}
}

func TestDynamicToolToResponsesApiToolPreservesDeferLoading(t *testing.T) {
	tool := protocol.DynamicToolSpec{
		Name:        "lookup_order",
		Description: "Look up an order",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": { "order_id": { "type": "string" } },
			"required": ["order_id"],
			"additionalProperties": false
		}`),
		DeferLoading: true,
	}
	got, err := DynamicToolToResponsesApiTool(tool)
	if err != nil {
		t.Fatalf("DynamicToolToResponsesApiTool: %v", err)
	}
	if got.DeferLoading == nil || !*got.DeferLoading {
		t.Fatalf("expected defer_loading=true")
	}
	if got.OutputSchema != nil {
		t.Fatalf("expected nil output schema, got %s", got.OutputSchema)
	}
	wantParams := ObjectSchema(
		map[string]JsonSchema{"order_id": StringSchema(nil)},
		[]string{"order_id"},
		BoolAdditionalProperties(false),
	)
	if !reflect.DeepEqual(got.Parameters, wantParams) {
		gp, _ := json.Marshal(got.Parameters)
		wp, _ := json.Marshal(wantParams)
		t.Fatalf("parameters mismatch\n got: %s\nwant: %s", gp, wp)
	}
}

func TestMcpToolToDeferredResponsesApiToolSetsDeferLoading(t *testing.T) {
	tool := protocol.Tool{
		Name:        "lookup_order",
		Description: strp("Look up an order"),
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": { "order_id": { "type": "string" } },
			"required": ["order_id"],
			"additionalProperties": false
		}`),
	}
	got, err := McpToolToDeferredResponsesApiTool(
		protocol.NamespacedToolName("mcp__codex_apps__", "lookup_order"),
		tool,
	)
	if err != nil {
		t.Fatalf("McpToolToDeferredResponsesApiTool: %v", err)
	}
	if got.Name != "lookup_order" {
		t.Fatalf("expected renamed tool, got %q", got.Name)
	}
	if got.DeferLoading == nil || !*got.DeferLoading {
		t.Fatalf("expected defer_loading=true")
	}
	if got.OutputSchema != nil {
		t.Fatalf("expected nil output schema after deferral")
	}
}

func TestLoadableToolSpecNamespaceSerializes(t *testing.T) {
	deferLoading := true
	ns := NamespaceLoadableToolSpec(ResponsesApiNamespace{
		Name:        "mcp__codex_apps__calendar",
		Description: "Plan events",
		Tools: []ResponsesApiNamespaceTool{
			FunctionNamespaceTool(ResponsesApiTool{
				Name:         "create_event",
				Description:  "Create a calendar event.",
				DeferLoading: &deferLoading,
				Parameters:   ObjectSchema(map[string]JsonSchema{}, nil, nil),
			}),
		},
	})
	raw, err := json.Marshal(ns)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonEqual(t, raw, `{
		"type": "namespace",
		"name": "mcp__codex_apps__calendar",
		"description": "Plan events",
		"tools": [
			{
				"type": "function",
				"name": "create_event",
				"description": "Create a calendar event.",
				"strict": false,
				"defer_loading": true,
				"parameters": { "type": "object", "properties": {} }
			}
		]
	}`)
}

func TestCoalesceLoadableToolSpecs(t *testing.T) {
	first := NamespaceLoadableToolSpec(ResponsesApiNamespace{
		Name:  "ns",
		Tools: []ResponsesApiNamespaceTool{FunctionNamespaceTool(ResponsesApiTool{Name: "a"})},
	})
	second := NamespaceLoadableToolSpec(ResponsesApiNamespace{
		Name:  "ns",
		Tools: []ResponsesApiNamespaceTool{FunctionNamespaceTool(ResponsesApiTool{Name: "b"})},
	})
	fn := FunctionLoadableToolSpec(ResponsesApiTool{Name: "standalone"})

	out := CoalesceLoadableToolSpecs([]LoadableToolSpec{first, fn, second})
	if len(out) != 2 {
		t.Fatalf("expected 2 coalesced specs, got %d", len(out))
	}
	if out[0].Kind != LoadableToolSpecKindNamespace || len(out[0].Namespace.Tools) != 2 {
		t.Fatalf("expected merged namespace with 2 tools, got %+v", out[0])
	}
	if out[1].Kind != LoadableToolSpecKindFunction || out[1].Function.Name != "standalone" {
		t.Fatalf("expected standalone function preserved, got %+v", out[1])
	}
}

func TestDefaultNamespaceDescription(t *testing.T) {
	if got := DefaultNamespaceDescription("mcp__demo__"); got != "Tools in the mcp__demo__ namespace." {
		t.Fatalf("unexpected description: %q", got)
	}
}

func TestResponsesApiToolDoesNotSerializeOutputSchema(t *testing.T) {
	tool := ResponsesApiTool{
		Name:         "t",
		Description:  "d",
		Parameters:   ObjectSchema(map[string]JsonSchema{}, nil, nil),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	raw, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["output_schema"]; ok {
		t.Fatalf("output_schema must not be serialized")
	}
}
