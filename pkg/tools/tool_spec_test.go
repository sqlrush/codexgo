package tools

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// jsonEqual reports whether two JSON byte slices are structurally equal,
// ignoring key order. It mirrors how the Rust tests compare serde_json::Value.
func jsonEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal got %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("unmarshal want %s: %v", want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("JSON mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestToolSpecName(t *testing.T) {
	high := protocol.WebSearchContextSizeHigh
	tests := []struct {
		name string
		spec ToolSpec
		want string
	}{
		{
			name: "function",
			spec: FunctionToolSpec(ResponsesApiTool{
				Name:        "lookup_order",
				Description: "Look up an order",
				Parameters:  ObjectSchema(nil, nil, nil),
			}),
			want: "lookup_order",
		},
		{
			name: "namespace",
			spec: NamespaceToolSpec(ResponsesApiNamespace{Name: "mcp__demo__", Description: "Demo tools"}),
			want: "mcp__demo__",
		},
		{
			name: "tool_search",
			spec: ToolSearchToolSpec(ToolSearchSpec{Execution: "sync", Description: "Search for tools"}),
			want: "tool_search",
		},
		{
			name: "image_generation",
			spec: ImageGenerationToolSpec(ImageGenerationSpec{OutputFormat: "png"}),
			want: "image_generation",
		},
		{
			name: "web_search",
			spec: WebSearchToolSpec(WebSearchSpec{ExternalWebAccess: boolPtr(true)}),
			want: "web_search",
		},
		{
			name: "custom",
			spec: FreeformToolSpec(FreeformTool{Name: "exec", Description: "Run a command"}),
			want: "exec",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.Name(); got != tt.want {
				t.Fatalf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
	_ = high
}

func TestFunctionToolSpecSerializes(t *testing.T) {
	spec := FunctionToolSpec(ResponsesApiTool{
		Name:        "demo",
		Description: "A demo tool",
		Parameters: ObjectSchema(
			map[string]JsonSchema{"foo": StringSchema(nil)},
			nil, nil,
		),
	})
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonEqual(t, raw, `{
		"type": "function",
		"name": "demo",
		"description": "A demo tool",
		"strict": false,
		"parameters": {
			"type": "object",
			"properties": { "foo": { "type": "string" } }
		}
	}`)
}

func TestNamespaceToolSpecSerializes(t *testing.T) {
	spec := NamespaceToolSpec(ResponsesApiNamespace{
		Name:        "mcp__demo__",
		Description: "Demo tools",
		Tools: []ResponsesApiNamespaceTool{
			FunctionNamespaceTool(ResponsesApiTool{
				Name:        "lookup_order",
				Description: "Look up an order",
				Parameters: ObjectSchema(
					map[string]JsonSchema{"order_id": StringSchema(nil)},
					nil, nil,
				),
			}),
		},
	})
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonEqual(t, raw, `{
		"type": "namespace",
		"name": "mcp__demo__",
		"description": "Demo tools",
		"tools": [
			{
				"type": "function",
				"name": "lookup_order",
				"description": "Look up an order",
				"strict": false,
				"parameters": {
					"type": "object",
					"properties": { "order_id": { "type": "string" } }
				}
			}
		]
	}`)
}

func TestWebSearchToolSpecSerializes(t *testing.T) {
	high := protocol.WebSearchContextSizeHigh
	domains := []string{"example.com"}
	spec := WebSearchToolSpec(WebSearchSpec{
		ExternalWebAccess: boolPtr(true),
		Filters:           &ResponsesApiWebSearchFilters{AllowedDomains: &domains},
		UserLocation: &ResponsesApiWebSearchUserLocation{
			Type:     protocol.WebSearchUserLocationTypeApproximate,
			Country:  strp("US"),
			Region:   strp("California"),
			City:     strp("San Francisco"),
			Timezone: strp("America/Los_Angeles"),
		},
		SearchContextSize:  &high,
		SearchContentTypes: []string{"text", "image"},
	})
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonEqual(t, raw, `{
		"type": "web_search",
		"external_web_access": true,
		"filters": { "allowed_domains": ["example.com"] },
		"user_location": {
			"type": "approximate",
			"country": "US",
			"region": "California",
			"city": "San Francisco",
			"timezone": "America/Los_Angeles"
		},
		"search_context_size": "high",
		"search_content_types": ["text", "image"]
	}`)
}

func TestWebSearchToolSpecOmitsNoneFields(t *testing.T) {
	spec := WebSearchToolSpec(WebSearchSpec{ExternalWebAccess: boolPtr(false)})
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonEqual(t, raw, `{"type":"web_search","external_web_access":false}`)
}

func TestToolSearchToolSpecSerializes(t *testing.T) {
	spec := ToolSearchToolSpec(ToolSearchSpec{
		Execution:   "sync",
		Description: "Search app tools",
		Parameters: ObjectSchema(
			map[string]JsonSchema{"query": StringSchema(strp("Tool search query"))},
			[]string{"query"},
			BoolAdditionalProperties(false),
		),
	})
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonEqual(t, raw, `{
		"type": "tool_search",
		"execution": "sync",
		"description": "Search app tools",
		"parameters": {
			"type": "object",
			"properties": {
				"query": { "type": "string", "description": "Tool search query" }
			},
			"required": ["query"],
			"additionalProperties": false
		}
	}`)
}

func TestImageGenerationToolSpecSerializes(t *testing.T) {
	spec := ImageGenerationToolSpec(ImageGenerationSpec{OutputFormat: "png"})
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonEqual(t, raw, `{"type":"image_generation","output_format":"png"}`)
}

func TestFreeformToolSpecSerializes(t *testing.T) {
	spec := FreeformToolSpec(FreeformTool{
		Name:        "exec",
		Description: "Run a command",
		Format: FreeformToolFormat{
			Type:       "grammar",
			Syntax:     "lark",
			Definition: "start: \"exec\"",
		},
	})
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonEqual(t, raw, `{
		"type": "custom",
		"name": "exec",
		"description": "Run a command",
		"format": { "type": "grammar", "syntax": "lark", "definition": "start: \"exec\"" }
	}`)
}

func TestCreateToolsJSONForResponsesAPI(t *testing.T) {
	tools := []ToolSpec{
		FunctionToolSpec(ResponsesApiTool{
			Name:        "demo",
			Description: "A demo tool",
			Parameters: ObjectSchema(
				map[string]JsonSchema{"foo": StringSchema(nil)}, nil, nil,
			),
		}),
	}
	out, err := CreateToolsJSONForResponsesAPI(tools)
	if err != nil {
		t.Fatalf("CreateToolsJSONForResponsesAPI: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out))
	}
	jsonEqual(t, out[0], `{
		"type": "function",
		"name": "demo",
		"description": "A demo tool",
		"strict": false,
		"parameters": {
			"type": "object",
			"properties": { "foo": { "type": "string" } }
		}
	}`)
}

func TestToolSpecFromLoadable(t *testing.T) {
	fn := FunctionToolSpec(ResponsesApiTool{Name: "fn"})
	got := ToolSpecFromLoadable(FunctionLoadableToolSpec(*fn.Function))
	if got.Kind != ToolSpecKindFunction || got.Function.Name != "fn" {
		t.Fatalf("function loadable conversion failed: %+v", got)
	}

	ns := ResponsesApiNamespace{Name: "ns"}
	gotNS := ToolSpecFromLoadable(NamespaceLoadableToolSpec(ns))
	if gotNS.Kind != ToolSpecKindNamespace || gotNS.Namespace.Name != "ns" {
		t.Fatalf("namespace loadable conversion failed: %+v", gotNS)
	}
}

func strp(s string) *string { return &s }
