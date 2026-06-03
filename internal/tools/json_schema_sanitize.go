package tools

import (
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
)

// definitionTableKeys are the JSON Schema keywords that hold definition tables.
var definitionTableKeys = [2]string{"$defs", "definitions"}

// schemaChildKeys are non-property schema-child keywords traversed during
// sanitization and ref collection.
var schemaChildKeys = [2]string{"items", "anyOf"}

// Use compact normalized JSON bytes as a cheap local proxy for the 1k-token
// schema budget.
const (
	maxCompactToolSchemaBytes = 4_000
	maxCompactToolSchemaDepth = 2
)

// prepareToolInputSchema clones, sanitizes, and prunes the input schema value.
// It mirrors Rust `prepare_tool_input_schema`, operating on decoded JSON.
func prepareToolInputSchema(inputSchema json.RawMessage) (any, error) {
	var value any
	dec := json.NewDecoder(strings.NewReader(string(inputSchema)))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	sanitizeJSONSchema(&value)
	pruneUnreachableDefinitions(&value)
	return value, nil
}

// deserializeToolInputSchema decodes the prepared value into a JsonSchema and
// rejects a singleton null type. Mirrors Rust `deserialize_tool_input_schema`.
func deserializeToolInputSchema(value any) (JsonSchema, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return JsonSchema{}, err
	}
	var schema JsonSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return JsonSchema{}, err
	}
	if schema.Type != nil &&
		schema.Type.Single != nil &&
		*schema.Type.Single == JsonSchemaPrimitiveTypeNull {
		return JsonSchema{}, errSingletonNullSchema
	}
	return schema, nil
}

// errSingletonNullSchema mirrors Rust `singleton_null_schema_error`.
var errSingletonNullSchema = errors.New("tool input schema must not be a singleton null type")

// largeSchemaCompactionPass is one lossy compaction step applied while a schema
// remains over budget.
type largeSchemaCompactionPass func(*any)

// largeSchemaCompactionPasses are applied in order until the schema fits budget.
var largeSchemaCompactionPasses = []largeSchemaCompactionPass{
	stripSchemaDescriptions,
	dropSchemaDefinitions,
	collapseDeepSchemaObjectsFromRoot,
}

// compactLargeToolSchema shrinks unusually large tool schemas while preserving
// the top-level argument surface. Mirrors Rust `compact_large_tool_schema`.
func compactLargeToolSchema(value *any) error {
	for _, pass := range largeSchemaCompactionPasses {
		fits, err := compactSchemaFitsBudget(*value)
		if err != nil {
			return err
		}
		if fits {
			break
		}
		pass(value)
	}
	return nil
}

func compactSchemaFitsBudget(value any) (bool, error) {
	n, err := compactNormalizedSchemaLen(value)
	if err != nil {
		return false, err
	}
	return n <= maxCompactToolSchemaBytes, nil
}

// compactNormalizedSchemaLen mirrors Rust `compact_normalized_schema_len`: it
// round-trips through JsonSchema and reports the serialized length, or 0 if the
// value cannot be represented as a JsonSchema.
func compactNormalizedSchemaLen(value any) (int, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	var schema JsonSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return 0, nil
	}
	out, err := json.Marshal(schema)
	if err != nil {
		return 0, nil
	}
	return len(out), nil
}

// definitionTraversal selects whether schema-child traversal descends into
// definition tables.
type definitionTraversal bool

const (
	definitionInclude definitionTraversal = true
	definitionSkip    definitionTraversal = false
)

// forEachSchemaChild visits read-only schema children of a map: property values,
// schema-child keys, non-boolean additionalProperties, and (optionally)
// definition-table entries. Mirrors Rust `for_each_schema_child`.
func forEachSchemaChild(m map[string]any, traversal definitionTraversal, visit func(any)) {
	if props, ok := m["properties"].(map[string]any); ok {
		for _, k := range sortedKeys(props) {
			visit(props[k])
		}
	}
	for _, key := range schemaChildKeys {
		if v, ok := m[key]; ok {
			visit(v)
		}
	}
	if ap, ok := m["additionalProperties"]; ok {
		if _, isBool := ap.(bool); !isBool {
			visit(ap)
		}
	}
	if traversal == definitionInclude {
		for _, key := range definitionTableKeys {
			if defs, ok := m[key].(map[string]any); ok {
				for _, k := range sortedKeys(defs) {
					visit(defs[k])
				}
			}
		}
	}
}

// forEachSchemaChildMut visits mutable schema children, replacing each in place
// via the visitor's reassignment of the pointed-to value. Mirrors Rust
// `for_each_schema_child_mut`.
func forEachSchemaChildMut(m map[string]any, traversal definitionTraversal, visit func(*any)) {
	if props, ok := m["properties"].(map[string]any); ok {
		for _, k := range sortedKeys(props) {
			v := props[k]
			visit(&v)
			props[k] = v
		}
	}
	for _, key := range schemaChildKeys {
		if v, ok := m[key]; ok {
			visit(&v)
			m[key] = v
		}
	}
	if ap, ok := m["additionalProperties"]; ok {
		if _, isBool := ap.(bool); !isBool {
			visit(&ap)
			m["additionalProperties"] = ap
		}
	}
	if traversal == definitionInclude {
		for _, key := range definitionTableKeys {
			if defs, ok := m[key].(map[string]any); ok {
				for _, k := range sortedKeys(defs) {
					v := defs[k]
					visit(&v)
					defs[k] = v
				}
			}
		}
	}
}

func stripSchemaDescriptions(value *any) {
	switch v := (*value).(type) {
	case []any:
		for i := range v {
			stripSchemaDescriptions(&v[i])
		}
	case map[string]any:
		delete(v, "description")
		forEachSchemaChildMut(v, definitionInclude, stripSchemaDescriptions)
	}
}

// dropSchemaDefinitions rewrites local definition refs to empty schemas, then
// removes root definition tables. Mirrors Rust `drop_schema_definitions`.
func dropSchemaDefinitions(value *any) {
	rewriteDefinitionRefsToEmptySchemas(value)
	m, ok := (*value).(map[string]any)
	if !ok {
		return
	}
	for _, key := range definitionTableKeys {
		delete(m, key)
	}
}

func rewriteDefinitionRefsToEmptySchemas(value *any) {
	switch v := (*value).(type) {
	case []any:
		for i := range v {
			rewriteDefinitionRefsToEmptySchemas(&v[i])
		}
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok {
			if _, parsed := parseLocalDefinitionRef(ref); parsed {
				*value = map[string]any{}
				return
			}
		}
		forEachSchemaChildMut(v, definitionSkip, rewriteDefinitionRefsToEmptySchemas)
	}
}

func collapseDeepSchemaObjectsFromRoot(value *any) {
	collapseDeepSchemaObjects(value, 0)
}

func collapseDeepSchemaObjects(value *any, depth int) {
	switch v := (*value).(type) {
	case []any:
		for i := range v {
			collapseDeepSchemaObjects(&v[i], depth)
		}
	case map[string]any:
		if depth >= maxCompactToolSchemaDepth && isComplexSchemaObject(v) {
			*value = map[string]any{}
			return
		}
		forEachSchemaChildMut(v, definitionSkip, func(child *any) {
			collapseDeepSchemaObjects(child, depth+1)
		})
	}
}

func isComplexSchemaObject(m map[string]any) bool {
	for _, key := range schemaChildKeys {
		if _, ok := m[key]; ok {
			return true
		}
	}
	_, hasProps := m["properties"]
	_, hasAdditional := m["additionalProperties"]
	_, hasRef := m["$ref"]
	return hasProps || hasAdditional || hasRef
}

// sortedKeys returns the keys of m sorted, so traversal is deterministic and
// matches the BTreeMap ordering used in Rust.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseLocalDefinitionRef parses a local `#/$defs/Name` (or `#/definitions/Name`)
// ref into a (table, name) pointer. Mirrors Rust `parse_local_definition_ref`,
// keeping the parent definition reachable for nested refs.
func parseLocalDefinitionRef(schemaRef string) (definitionPointer, bool) {
	fragment, ok := strings.CutPrefix(schemaRef, "#")
	if !ok {
		return definitionPointer{}, false
	}
	decoded, err := url.PathUnescape(fragment)
	if err != nil {
		return definitionPointer{}, false
	}
	tokens, ok := parseJSONPointer(decoded)
	if !ok || len(tokens) < 2 {
		return definitionPointer{}, false
	}
	table := ""
	for _, candidate := range definitionTableKeys {
		if tokens[0] == candidate {
			table = candidate
			break
		}
	}
	if table == "" {
		return definitionPointer{}, false
	}
	return definitionPointer{table: table, name: tokens[1]}, true
}

// parseJSONPointer decodes an RFC 6901 JSON Pointer into its reference tokens.
// An empty pointer ("") yields zero tokens; a leading "/" is required for
// non-empty pointers.
func parseJSONPointer(pointer string) ([]string, bool) {
	if pointer == "" {
		return nil, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	parts := strings.Split(pointer[1:], "/")
	tokens := make([]string, len(parts))
	for i, part := range parts {
		// RFC 6901 unescaping: ~1 -> /, ~0 -> ~ (order matters).
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		tokens[i] = part
	}
	return tokens, true
}
