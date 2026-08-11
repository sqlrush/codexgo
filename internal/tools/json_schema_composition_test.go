package tools

import (
	"encoding/json"
	"testing"
)

// TestCompositionKeywordsPreserved covers spec 49 need 3: oneOf/allOf (not just
// anyOf) survive schema lowering with their children sanitized. Regression guard
// against the old schemaChildKeys=[items,anyOf] that dropped oneOf/allOf.
func TestCompositionKeywordsPreserved(t *testing.T) {
	for _, keyword := range []string{"anyOf", "oneOf", "allOf"} {
		t.Run(keyword, func(t *testing.T) {
			raw := `{"` + keyword + `":[{"type":"string"},{"type":"number","const":5}]}`
			var value any
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			sanitizeJSONSchema(&value)

			m, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("lowered schema is not an object: %T", value)
			}
			branches, ok := m[keyword].([]any)
			if !ok || len(branches) != 2 {
				t.Fatalf("%s not preserved: %+v", keyword, m)
			}
			// child sanitization ran: const → enum inside the composed branch
			second, ok := branches[1].(map[string]any)
			if !ok {
				t.Fatalf("branch not object: %+v", branches[1])
			}
			if _, hasConst := second["const"]; hasConst {
				t.Fatalf("child not sanitized (const should become enum): %+v", second)
			}
			if _, hasEnum := second["enum"]; !hasEnum {
				t.Fatalf("child sanitization did not run: %+v", second)
			}
		})
	}
}

// TestCompositionOnlySchemaKeepsNoType: a schema with only a composition keyword
// (no explicit type) must not be coerced to a concrete type or cleared.
func TestCompositionOnlySchemaKeepsNoType(t *testing.T) {
	var value any = map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	}
	sanitizeJSONSchema(&value)
	m := value.(map[string]any)
	if _, hasType := m["type"]; hasType {
		t.Fatalf("composition-only schema gained a type: %+v", m)
	}
	if _, hasOneOf := m["oneOf"]; !hasOneOf {
		t.Fatalf("oneOf dropped: %+v", m)
	}
}
