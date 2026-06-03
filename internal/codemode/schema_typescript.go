package codemode

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderJSONSchemaToTypeScript renders a decoded JSON schema value into a
// TypeScript type string. Mirrors codex's `render_json_schema_to_typescript`.
//
// The schema value uses the `any` shapes produced by encoding/json:
// map[string]any for objects, []any for arrays, bool for boolean schemas,
// float64 for numbers, string, and nil.
func RenderJSONSchemaToTypeScript(schema any) string {
	return renderSchema(schema)
}

func renderSchema(schema any) string {
	switch typed := schema.(type) {
	case bool:
		if typed {
			return "unknown"
		}
		return "never"
	case map[string]any:
		return renderSchemaObject(typed)
	default:
		return "unknown"
	}
}

func renderSchemaObject(m map[string]any) string {
	if constValue, ok := m["const"]; ok {
		return renderSchemaLiteral(constValue)
	}

	if values, ok := m["enum"].([]any); ok {
		rendered := make([]string, 0, len(values))
		for _, value := range values {
			rendered = append(rendered, renderSchemaLiteral(value))
		}
		if len(rendered) > 0 {
			return strings.Join(rendered, " | ")
		}
	}

	for _, key := range []string{"anyOf", "oneOf"} {
		if variants, ok := m[key].([]any); ok {
			rendered := make([]string, 0, len(variants))
			for _, variant := range variants {
				rendered = append(rendered, renderSchema(variant))
			}
			if len(rendered) > 0 {
				return strings.Join(rendered, " | ")
			}
		}
	}

	if variants, ok := m["allOf"].([]any); ok {
		rendered := make([]string, 0, len(variants))
		for _, variant := range variants {
			rendered = append(rendered, renderSchema(variant))
		}
		if len(rendered) > 0 {
			return strings.Join(rendered, " & ")
		}
	}

	if schemaType, ok := m["type"]; ok {
		if types, ok := schemaType.([]any); ok {
			rendered := make([]string, 0, len(types))
			for _, entry := range types {
				if entryStr, ok := entry.(string); ok {
					rendered = append(rendered, renderSchemaTypeKeyword(m, entryStr))
				}
			}
			if len(rendered) > 0 {
				return strings.Join(rendered, " | ")
			}
		}
		if typeStr, ok := schemaType.(string); ok {
			return renderSchemaTypeKeyword(m, typeStr)
		}
	}

	if hasKey(m, "properties") || hasKey(m, "additionalProperties") || hasKey(m, "required") {
		return renderSchemaObjectShape(m)
	}

	if hasKey(m, "items") || hasKey(m, "prefixItems") {
		return renderSchemaArray(m)
	}

	return "unknown"
}

func renderSchemaTypeKeyword(m map[string]any, schemaType string) string {
	switch schemaType {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "boolean"
	case "null":
		return "null"
	case "array":
		return renderSchemaArray(m)
	case "object":
		return renderSchemaObjectShape(m)
	default:
		return "unknown"
	}
}

func renderSchemaArray(m map[string]any) string {
	if items, ok := m["items"]; ok {
		itemType := renderSchema(items)
		return fmt.Sprintf("Array<%s>", itemType)
	}
	if items, ok := m["prefixItems"].([]any); ok {
		itemTypes := make([]string, 0, len(items))
		for _, item := range items {
			itemTypes = append(itemTypes, renderSchema(item))
		}
		if len(itemTypes) > 0 {
			return fmt.Sprintf("[%s]", strings.Join(itemTypes, ", "))
		}
	}
	return "unknown[]"
}

func renderSchemaObjectShape(m map[string]any) string {
	required := requiredFields(m)
	properties, _ := m["properties"].(map[string]any)

	sortedNames := sortedKeys(properties)

	anyDescriptions := false
	for _, name := range sortedNames {
		if hasPropertyDescription(properties[name]) {
			anyDescriptions = true
			break
		}
	}

	if anyDescriptions {
		lines := []string{"{"}
		for _, name := range sortedNames {
			value := properties[name]
			if description, ok := propertyDescription(value); ok {
				for _, descLine := range strings.Split(description, "\n") {
					descLine = strings.TrimSpace(descLine)
					if descLine != "" {
						lines = append(lines, fmt.Sprintf("  // %s", descLine))
					}
				}
			}
			lines = append(lines, fmt.Sprintf("  %s", renderSchemaObjectProperty(name, value, required)))
		}
		lines = appendAdditionalPropertiesLine(lines, m, properties, "  ")
		lines = append(lines, "}")
		return strings.Join(lines, "\n")
	}

	lines := make([]string, 0, len(sortedNames))
	for _, name := range sortedNames {
		lines = append(lines, renderSchemaObjectProperty(name, properties[name], required))
	}
	lines = appendAdditionalPropertiesLine(lines, m, properties, "")

	if len(lines) == 0 {
		return "{}"
	}
	return fmt.Sprintf("{ %s }", strings.Join(lines, " "))
}

func requiredFields(m map[string]any) []string {
	rawRequired, ok := m["required"].([]any)
	if !ok {
		return nil
	}
	required := make([]string, 0, len(rawRequired))
	for _, entry := range rawRequired {
		if entryStr, ok := entry.(string); ok {
			required = append(required, entryStr)
		}
	}
	return required
}

func appendAdditionalPropertiesLine(lines []string, m map[string]any, properties map[string]any, prefix string) []string {
	if additional, present := m["additionalProperties"]; present {
		var propertyType string
		emit := true
		switch typed := additional.(type) {
		case bool:
			if typed {
				propertyType = "unknown"
			} else {
				emit = false
			}
		default:
			propertyType = renderSchema(additional)
		}
		if emit {
			lines = append(lines, fmt.Sprintf("%s[key: string]: %s;", prefix, propertyType))
		}
	} else if len(properties) == 0 {
		lines = append(lines, fmt.Sprintf("%s[key: string]: unknown;", prefix))
	}
	return lines
}

func hasPropertyDescription(value any) bool {
	description, ok := propertyDescription(value)
	return ok && description != ""
}

func propertyDescription(value any) (string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	description, ok := object["description"].(string)
	return description, ok
}

func renderSchemaObjectProperty(name string, value any, required []string) string {
	optional := "?"
	for _, requiredName := range required {
		if requiredName == name {
			optional = ""
			break
		}
	}
	propertyName := renderSchemaPropertyName(name)
	propertyType := renderSchema(value)
	return fmt.Sprintf("%s%s: %s;", propertyName, optional, propertyType)
}

func renderSchemaPropertyName(name string) string {
	if NormalizeCodeModeIdentifier(name) == name {
		return name
	}
	encoded, err := json.Marshal(name)
	if err != nil {
		return fmt.Sprintf("%q", strings.ReplaceAll(name, "\"", "\\\""))
	}
	return string(encoded)
}

func renderSchemaLiteral(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "unknown"
	}
	return string(encoded)
}

func hasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}
