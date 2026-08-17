package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseToolInputSchemaSanitizes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "boolean form coerced to string",
			input: `true`,
			want:  `{"type":"string"}`,
		},
		{
			name:  "object infers type and empty properties",
			input: `{"properties":{"a":{"type":"string"}}}`,
			want:  `{"type":"object","properties":{"a":{"type":"string"}}}`,
		},
		{
			name:  "const collapses to enum",
			input: `{"const":"x"}`,
			want:  `{"type":"string","enum":["x"]}`,
		},
		{
			name:  "array fills default items",
			input: `{"type":"array"}`,
			want:  `{"type":"array","items":{"type":"string"}}`,
		},
		{
			name:  "property with only description collapses to empty",
			input: `{"type":"object","properties":{"id":{"description":"x"}}}`,
			want:  `{"type":"object","properties":{"id":{}}}`,
		},
		{
			name:  "unknown hints coerce to empty object schema",
			input: `{"title":"ignored"}`,
			want:  `{}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := ParseToolInputSchema(json.RawMessage(tt.input))
			if err != nil {
				t.Fatalf("ParseToolInputSchema: %v", err)
			}
			raw, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			jsonEqual(t, raw, tt.want)
		})
	}
}

func TestParseToolInputSchemaRejectsSingletonNull(t *testing.T) {
	_, err := ParseToolInputSchema(json.RawMessage(`{"type":"null"}`))
	if err == nil {
		t.Fatalf("expected error for singleton null type")
	}
	if !strings.Contains(err.Error(), "singleton null") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseToolInputSchemaPrunesUnreachableDefinitions(t *testing.T) {
	input := `{
		"type": "object",
		"properties": { "a": { "$ref": "#/$defs/Used" } },
		"$defs": {
			"Used": { "type": "string" },
			"Unused": { "type": "number" }
		}
	}`
	schema, err := ParseToolInputSchema(json.RawMessage(input))
	if err != nil {
		t.Fatalf("ParseToolInputSchema: %v", err)
	}
	if _, ok := schema.Defs["Used"]; !ok {
		t.Fatalf("expected Used definition to be retained")
	}
	if _, ok := schema.Defs["Unused"]; ok {
		t.Fatalf("expected Unused definition to be pruned")
	}
}

func TestParseToolInputSchemaWithoutCompactionKeepsDescriptions(t *testing.T) {
	input := `{"type":"object","properties":{"a":{"type":"string","description":"keep me"}}}`
	schema, err := ParseToolInputSchemaWithoutCompaction(json.RawMessage(input))
	if err != nil {
		t.Fatalf("ParseToolInputSchemaWithoutCompaction: %v", err)
	}
	prop := schema.Properties["a"]
	if prop.Description == nil || *prop.Description != "keep me" {
		t.Fatalf("expected description retained, got %+v", prop.Description)
	}
}

func TestJsonSchemaTypeRoundTrip(t *testing.T) {
	single := SingleType(JsonSchemaPrimitiveTypeString)
	raw, err := json.Marshal(single)
	if err != nil {
		t.Fatalf("marshal single: %v", err)
	}
	if string(raw) != `"string"` {
		t.Fatalf("single type: got %s", raw)
	}

	multi := MultipleType([]JsonSchemaPrimitiveType{
		JsonSchemaPrimitiveTypeString, JsonSchemaPrimitiveTypeNull,
	})
	raw, err = json.Marshal(multi)
	if err != nil {
		t.Fatalf("marshal multiple: %v", err)
	}
	if string(raw) != `["string","null"]` {
		t.Fatalf("multiple type: got %s", raw)
	}

	var decoded JsonSchemaType
	if err := json.Unmarshal([]byte(`["string","null"]`), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Multiple) != 2 {
		t.Fatalf("expected 2 types, got %+v", decoded)
	}
}

func TestAdditionalPropertiesRoundTrip(t *testing.T) {
	raw, err := json.Marshal(BoolAdditionalProperties(false))
	if err != nil {
		t.Fatalf("marshal bool: %v", err)
	}
	if string(raw) != `false` {
		t.Fatalf("bool additionalProperties: got %s", raw)
	}

	schema := SchemaAdditionalProperties(StringSchema(nil))
	raw, err = json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	jsonEqual(t, raw, `{"type":"string"}`)

	var decoded AdditionalProperties
	if err := json.Unmarshal([]byte(`true`), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Bool == nil || !*decoded.Bool {
		t.Fatalf("expected bool true, got %+v", decoded)
	}
}

func TestCompactLargeToolSchemaDropsDescriptionsWhenOverBudget(t *testing.T) {
	// Build an object schema whose descriptions push it over the byte budget.
	bigDescription := strings.Repeat("x", maxCompactToolSchemaBytes)
	props := map[string]any{}
	for i := 0; i < 3; i++ {
		props[string(rune('a'+i))] = map[string]any{
			"type":        "string",
			"description": bigDescription,
		}
	}
	input, _ := json.Marshal(map[string]any{
		"type":       "object",
		"properties": props,
	})
	schema, err := ParseToolInputSchema(input)
	if err != nil {
		t.Fatalf("ParseToolInputSchema: %v", err)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), bigDescription) {
		t.Fatalf("expected descriptions to be stripped under budget pressure")
	}
}
