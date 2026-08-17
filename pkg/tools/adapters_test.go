package tools

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestParseDynamicToolSanitizesInputSchema(t *testing.T) {
	tool := protocol.DynamicToolSpec{
		Name:        "lookup_ticket",
		Description: "Fetch a ticket",
		InputSchema: json.RawMessage(`{
			"properties": { "id": { "description": "Ticket identifier" } }
		}`),
		DeferLoading: false,
	}
	got, err := ParseDynamicTool(tool)
	if err != nil {
		t.Fatalf("ParseDynamicTool: %v", err)
	}
	// "id" has only a description and no recognized type hint, so it collapses to
	// an empty schema (Rust JsonSchema::default).
	want := ToolDefinition{
		Name:        "lookup_ticket",
		Description: "Fetch a ticket",
		InputSchema: ObjectSchema(
			map[string]JsonSchema{"id": {}},
			nil, nil,
		),
		OutputSchema: nil,
		DeferLoading: false,
	}
	if !reflect.DeepEqual(got, want) {
		gp, _ := json.Marshal(got.InputSchema)
		t.Fatalf("definition mismatch; input schema got: %s", gp)
	}
}

func TestParseDynamicToolPreservesDeferLoading(t *testing.T) {
	tool := protocol.DynamicToolSpec{
		Name:         "lookup_ticket",
		Description:  "Fetch a ticket",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{}}`),
		DeferLoading: true,
	}
	got, err := ParseDynamicTool(tool)
	if err != nil {
		t.Fatalf("ParseDynamicTool: %v", err)
	}
	if !got.DeferLoading {
		t.Fatalf("expected defer_loading=true")
	}
}

func TestParseMcpToolInsertsEmptyProperties(t *testing.T) {
	tool := protocol.Tool{
		Name:        "no_props",
		Description: strp("No properties"),
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	got, err := ParseMcpTool(tool)
	if err != nil {
		t.Fatalf("ParseMcpTool: %v", err)
	}
	wantInput := ObjectSchema(map[string]JsonSchema{}, nil, nil)
	if !reflect.DeepEqual(got.InputSchema, wantInput) {
		gp, _ := json.Marshal(got.InputSchema)
		t.Fatalf("input schema mismatch: %s", gp)
	}
	wantOutput, _ := McpCallToolResultOutputSchema(json.RawMessage("{}"))
	jsonEqual(t, got.OutputSchema, string(wantOutput))
	if got.DeferLoading {
		t.Fatalf("expected defer_loading=false")
	}
}

func TestParseMcpToolPreservesTopLevelOutputSchema(t *testing.T) {
	out := json.RawMessage(`{"properties":{"result":{"properties":{"nested":{}}}},"required":["result"]}`)
	tool := protocol.Tool{
		Name:         "with_output",
		Description:  strp("Has output schema"),
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: &out,
	}
	got, err := ParseMcpTool(tool)
	if err != nil {
		t.Fatalf("ParseMcpTool: %v", err)
	}
	wantOutput, _ := McpCallToolResultOutputSchema(out)
	jsonEqual(t, got.OutputSchema, string(wantOutput))
}

func TestMcpCallToolResultOutputSchemaShape(t *testing.T) {
	raw, err := McpCallToolResultOutputSchema(json.RawMessage(`{"enum":["ok","error"]}`))
	if err != nil {
		t.Fatalf("McpCallToolResultOutputSchema: %v", err)
	}
	jsonEqual(t, raw, `{
		"type": "object",
		"properties": {
			"content": { "type": "array", "items": { "type": "object" } },
			"structuredContent": { "enum": ["ok", "error"] },
			"isError": { "type": "boolean" },
			"_meta": { "type": "object" }
		},
		"required": ["content"],
		"additionalProperties": false
	}`)
}

func TestToolDefinitionRenamedAndDeferred(t *testing.T) {
	base := ToolDefinition{
		Name:         "lookup_order",
		Description:  "Look up an order",
		InputSchema:  ObjectSchema(map[string]JsonSchema{}, nil, nil),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		DeferLoading: false,
	}

	renamed := base.Renamed("mcp__orders__lookup_order")
	if renamed.Name != "mcp__orders__lookup_order" {
		t.Fatalf("Renamed did not set name: %q", renamed.Name)
	}
	if base.Name != "lookup_order" {
		t.Fatalf("Renamed mutated the original")
	}

	deferred := base.IntoDeferred()
	if !deferred.DeferLoading || deferred.OutputSchema != nil {
		t.Fatalf("IntoDeferred result wrong: %+v", deferred)
	}
	if base.OutputSchema == nil || base.DeferLoading {
		t.Fatalf("IntoDeferred mutated the original")
	}
}
