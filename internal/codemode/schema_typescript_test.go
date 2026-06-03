package codemode

import (
	"encoding/json"
	"testing"
)

// renderJSON decodes a JSON schema literal and renders it to TypeScript.
func renderJSON(t *testing.T, raw string) string {
	t.Helper()
	var schema any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return RenderJSONSchemaToTypeScript(schema)
}

// TestRenderJSONSchemaToTypeScript covers the primitive, object, array, union,
// literal, and additional-properties branches of the renderer.
func TestRenderJSONSchemaToTypeScript(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"string", `{"type":"string"}`, "string"},
		{"integer-is-number", `{"type":"integer"}`, "number"},
		{"number", `{"type":"number"}`, "number"},
		{"boolean", `{"type":"boolean"}`, "boolean"},
		{"null", `{"type":"null"}`, "null"},
		{"bool-true-schema", `true`, "unknown"},
		{"bool-false-schema", `false`, "never"},
		{"array-of-strings", `{"type":"array","items":{"type":"string"}}`, "Array<string>"},
		{"array-no-items", `{"type":"array"}`, "unknown[]"},
		{
			"tuple-prefix-items",
			`{"type":"array","prefixItems":[{"type":"string"},{"type":"number"}]}`,
			"[string, number]",
		},
		{
			"object-required-and-optional",
			`{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"number"}},"required":["a"]}`,
			"{ a: string; b?: number; }",
		},
		{
			"object-empty-closed",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			"{}",
		},
		{
			"object-empty-open-defaults-index",
			`{"type":"object","properties":{}}`,
			"{ [key: string]: unknown; }",
		},
		{
			"object-additional-typed",
			`{"type":"object","properties":{},"additionalProperties":{"type":"number"}}`,
			"{ [key: string]: number; }",
		},
		{"enum-union", `{"enum":["a","b",3]}`, `"a" | "b" | 3`},
		{"const-literal", `{"const":"x"}`, `"x"`},
		{
			"anyOf-union",
			`{"anyOf":[{"type":"string"},{"type":"number"}]}`,
			"string | number",
		},
		{
			"allOf-intersection",
			`{"allOf":[{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]},{"type":"object","properties":{"b":{"type":"number"}},"required":["b"]}]}`,
			"{ a: string; } & { b: number; }",
		},
		{
			"type-array-union",
			`{"type":["string","null"]}`,
			"string | null",
		},
		{"unknown-fallthrough", `{}`, "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderJSON(t, tc.raw); got != tc.want {
				t.Fatalf("render = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRenderJSONSchemaWithDescriptions verifies property descriptions become
// inline comments and that sorting is deterministic.
func TestRenderJSONSchemaWithDescriptions(t *testing.T) {
	got := renderJSON(t, `{
		"type":"object",
		"properties":{
			"zeta":{"type":"string","description":"the zeta field"},
			"alpha":{"type":"number"}
		},
		"required":["alpha","zeta"]
	}`)
	want := `{
  alpha: number;
  // the zeta field
  zeta: string;
}`
	if got != want {
		t.Fatalf("render =\n%s\nwant\n%s", got, want)
	}
}

// TestRenderSchemaQuotedPropertyName verifies that property names that are not
// valid identifiers are JSON-quoted.
func TestRenderSchemaQuotedPropertyName(t *testing.T) {
	got := renderJSON(t, `{
		"type":"object",
		"properties":{"weird-key":{"type":"string"}},
		"required":["weird-key"]
	}`)
	want := `{ "weird-key": string; }`
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}
