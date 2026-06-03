package tools

// sanitizeJSONSchema lowers an arbitrary JSON Schema (as decoded JSON) into the
// limited subset this package represents. It mirrors Rust `sanitize_json_schema`:
//   - coerces boolean schema form into an accept-all string,
//   - preserves anyOf / $ref / reachable definitions,
//   - collapses const into a single-value enum,
//   - fills required child fields for object/array types,
//   - coerces object schemas with no recognized hints into {}.
func sanitizeJSONSchema(value *any) {
	switch v := (*value).(type) {
	case bool:
		// JSON Schema boolean form: coerce to an accept-all string schema.
		*value = map[string]any{"type": "string"}
	case []any:
		for i := range v {
			sanitizeJSONSchema(&v[i])
		}
	case map[string]any:
		sanitizeSchemaObject(v)
	}
}

func sanitizeSchemaObject(m map[string]any) {
	if props, ok := m["properties"].(map[string]any); ok {
		for _, k := range sortedKeys(props) {
			child := props[k]
			sanitizeJSONSchema(&child)
			props[k] = child
		}
	}
	if items, ok := m["items"]; ok {
		sanitizeJSONSchema(&items)
		m["items"] = items
	}
	if ap, ok := m["additionalProperties"]; ok {
		if _, isBool := ap.(bool); !isBool {
			sanitizeJSONSchema(&ap)
			m["additionalProperties"] = ap
		}
	}
	if v, ok := m["prefixItems"]; ok {
		sanitizeJSONSchema(&v)
		m["prefixItems"] = v
	}
	if v, ok := m["anyOf"]; ok {
		sanitizeJSONSchema(&v)
		m["anyOf"] = v
	}
	for _, table := range definitionTableKeys {
		sanitizeSchemaTable(m, table)
	}

	if constValue, ok := m["const"]; ok {
		delete(m, "const")
		m["enum"] = []any{constValue}
	}

	schemaTypes := normalizedSchemaTypes(m)

	_, hasRef := m["$ref"]
	_, hasAnyOf := m["anyOf"]
	if len(schemaTypes) == 0 && (hasRef || hasAnyOf) {
		return
	}

	if len(schemaTypes) == 0 {
		switch {
		case mapHasAny(m, "properties", "required", "additionalProperties"):
			schemaTypes = append(schemaTypes, JsonSchemaPrimitiveTypeObject)
		case mapHasAny(m, "items", "prefixItems"):
			schemaTypes = append(schemaTypes, JsonSchemaPrimitiveTypeArray)
		case mapHasAny(m, "enum", "format"):
			schemaTypes = append(schemaTypes, JsonSchemaPrimitiveTypeString)
		case mapHasAny(m, "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"):
			schemaTypes = append(schemaTypes, JsonSchemaPrimitiveTypeNumber)
		default:
			clearMap(m)
			return
		}
	}

	writeSchemaTypes(m, schemaTypes)
	ensureDefaultChildrenForSchemaTypes(m, schemaTypes)
}

// sanitizeSchemaTable recursively sanitizes a definition table, dropping
// malformed (non-object) tables. Mirrors Rust `sanitize_schema_table`.
func sanitizeSchemaTable(m map[string]any, key string) {
	switch table := m[key].(type) {
	case map[string]any:
		for _, k := range sortedKeys(table) {
			def := table[k]
			sanitizeJSONSchema(&def)
			table[k] = def
		}
	case nil:
		if _, present := m[key]; present {
			delete(m, key)
		}
	default:
		delete(m, key)
	}
}

func ensureDefaultChildrenForSchemaTypes(m map[string]any, schemaTypes []JsonSchemaPrimitiveType) {
	if containsType(schemaTypes, JsonSchemaPrimitiveTypeObject) {
		if _, ok := m["properties"]; !ok {
			m["properties"] = map[string]any{}
		}
	}
	if containsType(schemaTypes, JsonSchemaPrimitiveTypeArray) {
		if _, ok := m["items"]; !ok {
			m["items"] = map[string]any{"type": "string"}
		}
	}
}

func normalizedSchemaTypes(m map[string]any) []JsonSchemaPrimitiveType {
	schemaType, ok := m["type"]
	if !ok {
		return nil
	}
	switch t := schemaType.(type) {
	case string:
		if pt, ok := schemaTypeFromStr(t); ok {
			return []JsonSchemaPrimitiveType{pt}
		}
		return nil
	case []any:
		var out []JsonSchemaPrimitiveType
		for _, entry := range t {
			if s, ok := entry.(string); ok {
				if pt, ok := schemaTypeFromStr(s); ok {
					out = append(out, pt)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func writeSchemaTypes(m map[string]any, schemaTypes []JsonSchemaPrimitiveType) {
	switch len(schemaTypes) {
	case 0:
		delete(m, "type")
	case 1:
		m["type"] = string(schemaTypes[0])
	default:
		arr := make([]any, len(schemaTypes))
		for i, t := range schemaTypes {
			arr[i] = string(t)
		}
		m["type"] = arr
	}
}

func schemaTypeFromStr(s string) (JsonSchemaPrimitiveType, bool) {
	switch s {
	case "string":
		return JsonSchemaPrimitiveTypeString, true
	case "number":
		return JsonSchemaPrimitiveTypeNumber, true
	case "boolean":
		return JsonSchemaPrimitiveTypeBoolean, true
	case "integer":
		return JsonSchemaPrimitiveTypeInteger, true
	case "object":
		return JsonSchemaPrimitiveTypeObject, true
	case "array":
		return JsonSchemaPrimitiveTypeArray, true
	case "null":
		return JsonSchemaPrimitiveTypeNull, true
	default:
		return "", false
	}
}

func containsType(types []JsonSchemaPrimitiveType, want JsonSchemaPrimitiveType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

func mapHasAny(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func clearMap(m map[string]any) {
	for k := range m {
		delete(m, k)
	}
}

// definitionPointer identifies a reachable definition by table and name.
type definitionPointer struct {
	table string
	name  string
}

// pruneUnreachableDefinitions removes unused root definition entries. Mirrors
// Rust `prune_unreachable_definitions`.
func pruneUnreachableDefinitions(value *any) {
	reachable := collectReachableDefinitions(*value)
	m, ok := (*value).(map[string]any)
	if !ok {
		return
	}
	for _, table := range definitionTableKeys {
		pruneSchemaTable(m, table, reachable)
	}
}

func pruneSchemaTable(m map[string]any, table string, reachable map[definitionPointer]struct{}) {
	defs, ok := m[table].(map[string]any)
	if !ok {
		return
	}
	for name := range defs {
		if _, keep := reachable[definitionPointer{table: table, name: name}]; !keep {
			delete(defs, name)
		}
	}
	if len(defs) == 0 {
		delete(m, table)
	}
}

func collectReachableDefinitions(value any) map[definitionPointer]struct{} {
	reachable := make(map[definitionPointer]struct{})
	var pending []definitionPointer
	collectRefsOutsideDefinitions(value, &pending)

	for len(pending) > 0 {
		pointer := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, seen := reachable[pointer]; seen {
			continue
		}
		reachable[pointer] = struct{}{}
		if def, ok := definitionForPointer(value, pointer); ok {
			collectRefs(def, &pending)
		}
	}
	return reachable
}

func collectRefsOutsideDefinitions(value any, refs *[]definitionPointer) {
	switch v := value.(type) {
	case []any:
		for i := range v {
			collectRefsOutsideDefinitions(v[i], refs)
		}
	case map[string]any:
		collectRefFromMap(v, refs)
		forEachSchemaChild(v, definitionSkip, func(child any) {
			collectRefsOutsideDefinitions(child, refs)
		})
	}
}

func collectRefs(value any, refs *[]definitionPointer) {
	switch v := value.(type) {
	case []any:
		for i := range v {
			collectRefs(v[i], refs)
		}
	case map[string]any:
		collectRefFromMap(v, refs)
		for _, k := range sortedKeys(v) {
			collectRefs(v[k], refs)
		}
	}
}

func collectRefFromMap(m map[string]any, refs *[]definitionPointer) {
	if ref, ok := m["$ref"].(string); ok {
		if pointer, parsed := parseLocalDefinitionRef(ref); parsed {
			*refs = append(*refs, pointer)
		}
	}
}

func definitionForPointer(value any, pointer definitionPointer) (any, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	table, ok := m[pointer.table].(map[string]any)
	if !ok {
		return nil, false
	}
	def, ok := table[pointer.name]
	return def, ok
}
