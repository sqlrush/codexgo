// Package tools ports the codex-tools framework: the ToolSpec model serialized
// to the OpenAI Responses API tool format, ToolName/ToolCall parsing, tool
// output shapes, and the registry/adapter types that the built-in tool specs
// (added in a later stage) reference.
//
// This file ports `json_schema.rs`: the generic JSON-Schema subset used by tool
// definitions, plus the sanitization/pruning/compaction pipeline that lowers
// arbitrary input schemas into that subset.
package tools

import (
	"encoding/json"
	"fmt"
)

// JsonSchemaPrimitiveType is a primitive JSON Schema type name supported in tool
// definitions. It mirrors the OpenAI Structured Outputs subset for the JSON
// Schema `type` keyword. Rust: `#[serde(rename_all = "lowercase")]`.
type JsonSchemaPrimitiveType string

// JsonSchemaPrimitiveType variants.
const (
	JsonSchemaPrimitiveTypeString  JsonSchemaPrimitiveType = "string"
	JsonSchemaPrimitiveTypeNumber  JsonSchemaPrimitiveType = "number"
	JsonSchemaPrimitiveTypeBoolean JsonSchemaPrimitiveType = "boolean"
	JsonSchemaPrimitiveTypeInteger JsonSchemaPrimitiveType = "integer"
	JsonSchemaPrimitiveTypeObject  JsonSchemaPrimitiveType = "object"
	JsonSchemaPrimitiveTypeArray   JsonSchemaPrimitiveType = "array"
	JsonSchemaPrimitiveTypeNull    JsonSchemaPrimitiveType = "null"
)

// JsonSchemaType is the JSON Schema `type` keyword, which supports either a
// single type name or a union of names. Rust: `#[serde(untagged)]` over
// `Single(..)` and `Multiple(Vec<..>)`.
type JsonSchemaType struct {
	// Single holds a single primitive type when Multiple is nil.
	Single *JsonSchemaPrimitiveType
	// Multiple holds a union of primitive types.
	Multiple []JsonSchemaPrimitiveType
}

// SingleType constructs a single-valued JsonSchemaType.
func SingleType(t JsonSchemaPrimitiveType) *JsonSchemaType {
	return &JsonSchemaType{Single: &t}
}

// MultipleType constructs a union-valued JsonSchemaType.
func MultipleType(types []JsonSchemaPrimitiveType) *JsonSchemaType {
	return &JsonSchemaType{Multiple: types}
}

// MarshalJSON emits the untagged form: a bare string for Single, an array of
// strings for Multiple.
func (t JsonSchemaType) MarshalJSON() ([]byte, error) {
	if t.Single != nil {
		return json.Marshal(*t.Single)
	}
	return json.Marshal(t.Multiple)
}

// UnmarshalJSON decodes the untagged form, accepting either a string or an array
// of strings.
func (t *JsonSchemaType) UnmarshalJSON(data []byte) error {
	var single JsonSchemaPrimitiveType
	if err := json.Unmarshal(data, &single); err == nil {
		*t = JsonSchemaType{Single: &single}
		return nil
	}
	var multiple []JsonSchemaPrimitiveType
	if err := json.Unmarshal(data, &multiple); err != nil {
		return fmt.Errorf("json schema type is neither string nor string array: %w", err)
	}
	*t = JsonSchemaType{Multiple: multiple}
	return nil
}

// AdditionalProperties describes whether additional properties are allowed, and
// if so, any required schema. Rust: `#[serde(untagged)]` over `Boolean(bool)`
// and `Schema(Box<JsonSchema>)`.
type AdditionalProperties struct {
	// Bool holds the boolean form when Schema is nil.
	Bool *bool
	// Schema holds a nested schema form.
	Schema *JsonSchema
}

// BoolAdditionalProperties constructs the boolean form.
func BoolAdditionalProperties(v bool) *AdditionalProperties {
	return &AdditionalProperties{Bool: &v}
}

// SchemaAdditionalProperties constructs the schema form.
func SchemaAdditionalProperties(schema JsonSchema) *AdditionalProperties {
	return &AdditionalProperties{Schema: &schema}
}

// MarshalJSON emits the untagged form: a bare boolean or a nested schema object.
func (a AdditionalProperties) MarshalJSON() ([]byte, error) {
	if a.Schema != nil {
		return json.Marshal(a.Schema)
	}
	if a.Bool != nil {
		return json.Marshal(*a.Bool)
	}
	return []byte("null"), nil
}

// UnmarshalJSON decodes the untagged form, preferring the boolean reading.
func (a *AdditionalProperties) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*a = AdditionalProperties{Bool: &b}
		return nil
	}
	var schema JsonSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("additionalProperties is neither bool nor schema: %w", err)
	}
	*a = AdditionalProperties{Schema: &schema}
	return nil
}

// JsonSchema is the generic JSON-Schema subset needed for tool definitions.
//
// Serialization (Rust derives Serialize): every field uses
// `skip_serializing_if = "Option::is_none"` and several are renamed. Field
// declaration order is preserved by encoding/json, matching serde's output
// order: $ref, type, description, enum, items, properties, required,
// additionalProperties, anyOf, $defs, definitions.
// A nil slice/map field denotes Rust's `None` (omitted from the wire), while a
// non-nil but empty slice/map denotes `Some(empty)` and serializes as `[]`/`{}`.
// Standard unmarshaling preserves this distinction (absent key -> nil), so only
// MarshalJSON is customized.
type JsonSchema struct {
	Ref                  *string               `json:"$ref"`
	Type                 *JsonSchemaType       `json:"type"`
	Description          *string               `json:"description"`
	Enum                 []json.RawMessage     `json:"enum"`
	Items                *JsonSchema           `json:"items"`
	Properties           map[string]JsonSchema `json:"properties"`
	Required             []string              `json:"required"`
	AdditionalProperties *AdditionalProperties `json:"additionalProperties"`
	AnyOf                []JsonSchema          `json:"anyOf"`
	Defs                 map[string]JsonSchema `json:"$defs"`
	Definitions          map[string]JsonSchema `json:"definitions"`
}

// MarshalJSON emits the schema in serde field order, omitting only fields that
// are None (nil). Non-nil empty collections are emitted as `[]`/`{}` to match
// Rust's `Some(empty)` semantics.
func (s JsonSchema) MarshalJSON() ([]byte, error) {
	m := orderedMap{}
	if s.Ref != nil {
		m.set("$ref", *s.Ref)
	}
	if s.Type != nil {
		m.set("type", s.Type)
	}
	if s.Description != nil {
		m.set("description", *s.Description)
	}
	if s.Enum != nil {
		m.set("enum", s.Enum)
	}
	if s.Items != nil {
		m.set("items", s.Items)
	}
	if s.Properties != nil {
		m.set("properties", s.Properties)
	}
	if s.Required != nil {
		m.set("required", s.Required)
	}
	if s.AdditionalProperties != nil {
		m.set("additionalProperties", s.AdditionalProperties)
	}
	if s.AnyOf != nil {
		m.set("anyOf", s.AnyOf)
	}
	if s.Defs != nil {
		m.set("$defs", s.Defs)
	}
	if s.Definitions != nil {
		m.set("definitions", s.Definitions)
	}
	return m.MarshalJSON()
}

// UnmarshalJSON decodes a schema, preserving the nil-vs-empty distinction. It
// must be explicit because the type has a custom MarshalJSON; this uses an alias
// to avoid recursing into MarshalJSON/UnmarshalJSON.
func (s *JsonSchema) UnmarshalJSON(data []byte) error {
	type rawSchema JsonSchema
	var raw rawSchema
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = JsonSchema(raw)
	return nil
}

// TypedSchema constructs a scalar/object/array schema with a single JSON Schema
// type. Mirrors Rust `JsonSchema::typed`.
func TypedSchema(t JsonSchemaPrimitiveType, description *string) JsonSchema {
	return JsonSchema{Type: SingleType(t), Description: description}
}

// AnyOfSchema mirrors Rust `JsonSchema::any_of`.
func AnyOfSchema(variants []JsonSchema, description *string) JsonSchema {
	return JsonSchema{Description: description, AnyOf: variants}
}

// BooleanSchema mirrors Rust `JsonSchema::boolean`.
func BooleanSchema(description *string) JsonSchema {
	return TypedSchema(JsonSchemaPrimitiveTypeBoolean, description)
}

// StringSchema mirrors Rust `JsonSchema::string`.
func StringSchema(description *string) JsonSchema {
	return TypedSchema(JsonSchemaPrimitiveTypeString, description)
}

// NumberSchema mirrors Rust `JsonSchema::number`.
func NumberSchema(description *string) JsonSchema {
	return TypedSchema(JsonSchemaPrimitiveTypeNumber, description)
}

// IntegerSchema mirrors Rust `JsonSchema::integer`.
func IntegerSchema(description *string) JsonSchema {
	return TypedSchema(JsonSchemaPrimitiveTypeInteger, description)
}

// NullSchema mirrors Rust `JsonSchema::null`.
func NullSchema(description *string) JsonSchema {
	return TypedSchema(JsonSchemaPrimitiveTypeNull, description)
}

// StringEnumSchema mirrors Rust `JsonSchema::string_enum`.
func StringEnumSchema(values []json.RawMessage, description *string) JsonSchema {
	return JsonSchema{
		Type:        SingleType(JsonSchemaPrimitiveTypeString),
		Description: description,
		Enum:        values,
	}
}

// ArraySchema mirrors Rust `JsonSchema::array`.
func ArraySchema(items JsonSchema, description *string) JsonSchema {
	return JsonSchema{
		Type:        SingleType(JsonSchemaPrimitiveTypeArray),
		Description: description,
		Items:       &items,
	}
}

// ObjectSchema mirrors Rust `JsonSchema::object`.
func ObjectSchema(
	properties map[string]JsonSchema,
	required []string,
	additionalProperties *AdditionalProperties,
) JsonSchema {
	return JsonSchema{
		Type:                 SingleType(JsonSchemaPrimitiveTypeObject),
		Properties:           properties,
		Required:             required,
		AdditionalProperties: additionalProperties,
	}
}

// ParseToolInputSchema parses an arbitrary tool `input_schema` value into the
// JsonSchema subset, running sanitization, pruning, and large-schema compaction.
// It mirrors Rust `parse_tool_input_schema` and wraps decode errors with %w.
func ParseToolInputSchema(inputSchema json.RawMessage) (JsonSchema, error) {
	prepared, err := prepareToolInputSchema(inputSchema)
	if err != nil {
		return JsonSchema{}, fmt.Errorf("prepare tool input schema: %w", err)
	}
	if err := compactLargeToolSchema(&prepared); err != nil {
		return JsonSchema{}, fmt.Errorf("compact tool input schema: %w", err)
	}
	schema, err := deserializeToolInputSchema(prepared)
	if err != nil {
		return JsonSchema{}, err
	}
	return schema, nil
}

// ParseToolInputSchemaWithoutCompaction parses a trusted tool `input_schema`
// without running large-schema compaction. Mirrors Rust
// `parse_tool_input_schema_without_compaction`.
func ParseToolInputSchemaWithoutCompaction(inputSchema json.RawMessage) (JsonSchema, error) {
	prepared, err := prepareToolInputSchema(inputSchema)
	if err != nil {
		return JsonSchema{}, fmt.Errorf("prepare tool input schema: %w", err)
	}
	schema, err := deserializeToolInputSchema(prepared)
	if err != nil {
		return JsonSchema{}, err
	}
	return schema, nil
}
