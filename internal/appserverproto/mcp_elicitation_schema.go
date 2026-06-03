package appserverproto

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// This file ports the typed MCP elicitation form schema from mcp.rs. These types
// mirror the `requestedSchema` shape of an MCP `elicitation/create` request.
//
// Several layers are serde `#[serde(untagged)]` unions whose inner structs use
// `deny_unknown_fields`. serde decodes an untagged union by trying each variant
// in declaration order and accepting the first that deserializes successfully;
// `deny_unknown_fields` makes the structurally-distinct variants unambiguous. We
// reproduce that exactly: each candidate is decoded with a JSON decoder that has
// DisallowUnknownFields set, and variants are attempted in the same order as the
// Rust source.

// strictUnmarshal decodes raw into v while rejecting unknown fields, mirroring
// serde's `deny_unknown_fields`. It returns an error if any field is unexpected.
func strictUnmarshal(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	// Reject trailing tokens so partial matches do not pass.
	if dec.More() {
		return fmt.Errorf("appserverproto: unexpected trailing JSON")
	}
	return nil
}

// validateFixedDiscriminant decodes a JSON string and verifies it equals one of
// the allowed values, mirroring serde's validation of a unit-variant enum. This
// is essential for untagged-union disambiguation, where serde rejects a variant
// whose discriminant value does not match.
func validateFixedDiscriminant(data []byte, allowed ...string) (string, error) {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return "", err
	}
	for _, a := range allowed {
		if s == a {
			return s, nil
		}
	}
	return "", fmt.Errorf("appserverproto: invalid discriminant %q, want one of %v", s, allowed)
}

// McpElicitationObjectType is the fixed "object" discriminant for the schema.
//
// Rust: v2::McpElicitationObjectType, serde rename_all = "lowercase".
type McpElicitationObjectType string

// McpElicitationObjectTypeObject is the only value: "object".
const McpElicitationObjectTypeObject McpElicitationObjectType = "object"

// UnmarshalJSON validates the fixed "object" value.
func (t *McpElicitationObjectType) UnmarshalJSON(data []byte) error {
	v, err := validateFixedDiscriminant(data, string(McpElicitationObjectTypeObject))
	if err != nil {
		return err
	}
	*t = McpElicitationObjectType(v)
	return nil
}

// McpElicitationStringType is the fixed "string" discriminant.
//
// Rust: v2::McpElicitationStringType, serde rename_all = "lowercase".
type McpElicitationStringType string

// McpElicitationStringTypeString is the only value: "string".
const McpElicitationStringTypeString McpElicitationStringType = "string"

// UnmarshalJSON validates the fixed "string" value.
func (t *McpElicitationStringType) UnmarshalJSON(data []byte) error {
	v, err := validateFixedDiscriminant(data, string(McpElicitationStringTypeString))
	if err != nil {
		return err
	}
	*t = McpElicitationStringType(v)
	return nil
}

// McpElicitationNumberType is the number discriminant (number or integer).
//
// Rust: v2::McpElicitationNumberType, serde rename_all = "lowercase".
type McpElicitationNumberType string

const (
	// McpElicitationNumberTypeNumber is a floating-point number.
	McpElicitationNumberTypeNumber McpElicitationNumberType = "number"
	// McpElicitationNumberTypeInteger is an integer.
	McpElicitationNumberTypeInteger McpElicitationNumberType = "integer"
)

// UnmarshalJSON validates the "number" or "integer" value.
func (t *McpElicitationNumberType) UnmarshalJSON(data []byte) error {
	v, err := validateFixedDiscriminant(data,
		string(McpElicitationNumberTypeNumber), string(McpElicitationNumberTypeInteger))
	if err != nil {
		return err
	}
	*t = McpElicitationNumberType(v)
	return nil
}

// McpElicitationBooleanType is the fixed "boolean" discriminant.
//
// Rust: v2::McpElicitationBooleanType, serde rename_all = "lowercase".
type McpElicitationBooleanType string

// McpElicitationBooleanTypeBoolean is the only value: "boolean".
const McpElicitationBooleanTypeBoolean McpElicitationBooleanType = "boolean"

// UnmarshalJSON validates the fixed "boolean" value.
func (t *McpElicitationBooleanType) UnmarshalJSON(data []byte) error {
	v, err := validateFixedDiscriminant(data, string(McpElicitationBooleanTypeBoolean))
	if err != nil {
		return err
	}
	*t = McpElicitationBooleanType(v)
	return nil
}

// McpElicitationArrayType is the fixed "array" discriminant.
//
// Rust: v2::McpElicitationArrayType, serde rename_all = "lowercase".
type McpElicitationArrayType string

// McpElicitationArrayTypeArray is the only value: "array".
const McpElicitationArrayTypeArray McpElicitationArrayType = "array"

// UnmarshalJSON validates the fixed "array" value.
func (t *McpElicitationArrayType) UnmarshalJSON(data []byte) error {
	v, err := validateFixedDiscriminant(data, string(McpElicitationArrayTypeArray))
	if err != nil {
		return err
	}
	*t = McpElicitationArrayType(v)
	return nil
}

// McpElicitationStringFormat constrains a string value's format.
//
// Rust: v2::McpElicitationStringFormat, serde rename_all = "kebab-case".
type McpElicitationStringFormat string

const (
	// McpElicitationStringFormatEmail constrains to an email address.
	McpElicitationStringFormatEmail McpElicitationStringFormat = "email"
	// McpElicitationStringFormatURI constrains to a URI.
	McpElicitationStringFormatURI McpElicitationStringFormat = "uri"
	// McpElicitationStringFormatDate constrains to a date.
	McpElicitationStringFormatDate McpElicitationStringFormat = "date"
	// McpElicitationStringFormatDateTime constrains to a date-time.
	McpElicitationStringFormatDateTime McpElicitationStringFormat = "date-time"
)

// McpElicitationSchema is the typed form schema. `$schema` and `required` use
// skip_serializing_if; `type` and `properties` are always emitted.
//
// Rust: v2::McpElicitationSchema, camelCase, deny_unknown_fields.
type McpElicitationSchema struct {
	SchemaURI  *string                                  `json:"$schema,omitempty"`
	Type       McpElicitationObjectType                 `json:"type"`
	Properties map[string]McpElicitationPrimitiveSchema `json:"properties"`
	Required   *[]string                                `json:"required,omitempty"`
}

// MarshalJSON ensures `properties` always emits a JSON object (never null).
func (s McpElicitationSchema) MarshalJSON() ([]byte, error) {
	type alias McpElicitationSchema
	out := alias(s)
	if out.Properties == nil {
		out.Properties = map[string]McpElicitationPrimitiveSchema{}
	}
	return json.Marshal(out)
}

// McpElicitationStringSchema describes a string property.
//
// Rust: v2::McpElicitationStringSchema, camelCase, deny_unknown_fields.
type McpElicitationStringSchema struct {
	Type        McpElicitationStringType    `json:"type"`
	Title       *string                     `json:"title,omitempty"`
	Description *string                     `json:"description,omitempty"`
	MinLength   *uint32                     `json:"minLength,omitempty"`
	MaxLength   *uint32                     `json:"maxLength,omitempty"`
	Format      *McpElicitationStringFormat `json:"format,omitempty"`
	Default     *string                     `json:"default,omitempty"`
}

// McpElicitationNumberSchema describes a number property.
//
// Rust: v2::McpElicitationNumberSchema, camelCase, deny_unknown_fields.
type McpElicitationNumberSchema struct {
	Type        McpElicitationNumberType `json:"type"`
	Title       *string                  `json:"title,omitempty"`
	Description *string                  `json:"description,omitempty"`
	Minimum     *float64                 `json:"minimum,omitempty"`
	Maximum     *float64                 `json:"maximum,omitempty"`
	Default     *float64                 `json:"default,omitempty"`
}

// McpElicitationBooleanSchema describes a boolean property.
//
// Rust: v2::McpElicitationBooleanSchema, camelCase, deny_unknown_fields.
type McpElicitationBooleanSchema struct {
	Type        McpElicitationBooleanType `json:"type"`
	Title       *string                   `json:"title,omitempty"`
	Description *string                   `json:"description,omitempty"`
	Default     *bool                     `json:"default,omitempty"`
}

// McpElicitationConstOption is a titled enum option (const + title).
//
// Rust: v2::McpElicitationConstOption, deny_unknown_fields.
type McpElicitationConstOption struct {
	Const string `json:"const"`
	Title string `json:"title"`
}

// McpElicitationLegacyTitledEnumSchema is the legacy enum/enumNames form.
//
// Rust: v2::McpElicitationLegacyTitledEnumSchema, camelCase, deny_unknown_fields.
type McpElicitationLegacyTitledEnumSchema struct {
	Type        McpElicitationStringType `json:"type"`
	Title       *string                  `json:"title,omitempty"`
	Description *string                  `json:"description,omitempty"`
	Enum        []string                 `json:"enum"`
	EnumNames   *[]string                `json:"enumNames,omitempty"`
	Default     *string                  `json:"default,omitempty"`
}

// McpElicitationUntitledSingleSelectEnumSchema is an untitled single-select.
//
// Rust: v2::McpElicitationUntitledSingleSelectEnumSchema, camelCase,
// deny_unknown_fields.
type McpElicitationUntitledSingleSelectEnumSchema struct {
	Type        McpElicitationStringType `json:"type"`
	Title       *string                  `json:"title,omitempty"`
	Description *string                  `json:"description,omitempty"`
	Enum        []string                 `json:"enum"`
	Default     *string                  `json:"default,omitempty"`
}

// McpElicitationTitledSingleSelectEnumSchema is a titled single-select (oneOf).
//
// Rust: v2::McpElicitationTitledSingleSelectEnumSchema, camelCase,
// deny_unknown_fields.
type McpElicitationTitledSingleSelectEnumSchema struct {
	Type        McpElicitationStringType    `json:"type"`
	Title       *string                     `json:"title,omitempty"`
	Description *string                     `json:"description,omitempty"`
	OneOf       []McpElicitationConstOption `json:"oneOf"`
	Default     *string                     `json:"default,omitempty"`
}

// McpElicitationUntitledEnumItems is the items shape for an untitled multi-select.
//
// Rust: v2::McpElicitationUntitledEnumItems, deny_unknown_fields.
type McpElicitationUntitledEnumItems struct {
	Type McpElicitationStringType `json:"type"`
	Enum []string                 `json:"enum"`
}

// McpElicitationTitledEnumItems is the items shape for a titled multi-select.
// On deserialize it also accepts the legacy alias "oneOf" for the "anyOf" key.
//
// Rust: v2::McpElicitationTitledEnumItems, deny_unknown_fields, with
// alias = "oneOf" on the anyOf field.
type McpElicitationTitledEnumItems struct {
	AnyOf []McpElicitationConstOption `json:"anyOf"`
}

// MarshalJSON emits the canonical "anyOf" key.
func (i McpElicitationTitledEnumItems) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		AnyOf []McpElicitationConstOption `json:"anyOf"`
	}{AnyOf: i.AnyOf})
}

// UnmarshalJSON accepts either "anyOf" or the legacy "oneOf" alias.
func (i *McpElicitationTitledEnumItems) UnmarshalJSON(data []byte) error {
	var probe struct {
		AnyOf []McpElicitationConstOption `json:"anyOf"`
		OneOf []McpElicitationConstOption `json:"oneOf"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&probe); err != nil {
		return err
	}
	if probe.AnyOf != nil {
		i.AnyOf = probe.AnyOf
	} else {
		i.AnyOf = probe.OneOf
	}
	return nil
}

// McpElicitationUntitledMultiSelectEnumSchema is an untitled multi-select.
//
// Rust: v2::McpElicitationUntitledMultiSelectEnumSchema, camelCase,
// deny_unknown_fields.
type McpElicitationUntitledMultiSelectEnumSchema struct {
	Type        McpElicitationArrayType         `json:"type"`
	Title       *string                         `json:"title,omitempty"`
	Description *string                         `json:"description,omitempty"`
	MinItems    *uint64                         `json:"minItems,omitempty"`
	MaxItems    *uint64                         `json:"maxItems,omitempty"`
	Items       McpElicitationUntitledEnumItems `json:"items"`
	Default     *[]string                       `json:"default,omitempty"`
}

// McpElicitationTitledMultiSelectEnumSchema is a titled multi-select.
//
// Rust: v2::McpElicitationTitledMultiSelectEnumSchema, camelCase,
// deny_unknown_fields.
type McpElicitationTitledMultiSelectEnumSchema struct {
	Type        McpElicitationArrayType       `json:"type"`
	Title       *string                       `json:"title,omitempty"`
	Description *string                       `json:"description,omitempty"`
	MinItems    *uint64                       `json:"minItems,omitempty"`
	MaxItems    *uint64                       `json:"maxItems,omitempty"`
	Items       McpElicitationTitledEnumItems `json:"items"`
	Default     *[]string                     `json:"default,omitempty"`
}

// McpElicitationSingleSelectKind discriminates the untagged single-select union.
type McpElicitationSingleSelectKind int

const (
	// McpElicitationSingleSelectUntitled is the untitled (enum) variant.
	McpElicitationSingleSelectUntitled McpElicitationSingleSelectKind = iota
	// McpElicitationSingleSelectTitled is the titled (oneOf) variant.
	McpElicitationSingleSelectTitled
)

// McpElicitationSingleSelectEnumSchema is the untagged single-select union.
// serde tries Untitled then Titled; the variants differ structurally (enum vs
// oneOf), which deny_unknown_fields makes unambiguous.
//
// Rust: v2::McpElicitationSingleSelectEnumSchema, untagged.
type McpElicitationSingleSelectEnumSchema struct {
	Kind     McpElicitationSingleSelectKind
	Untitled *McpElicitationUntitledSingleSelectEnumSchema
	Titled   *McpElicitationTitledSingleSelectEnumSchema
}

// MarshalJSON emits the active single-select variant.
func (s McpElicitationSingleSelectEnumSchema) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case McpElicitationSingleSelectUntitled:
		if s.Untitled == nil {
			return nil, fmt.Errorf("appserverproto: untitled single-select schema is nil")
		}
		return json.Marshal(s.Untitled)
	case McpElicitationSingleSelectTitled:
		if s.Titled == nil {
			return nil, fmt.Errorf("appserverproto: titled single-select schema is nil")
		}
		return json.Marshal(s.Titled)
	default:
		return nil, fmt.Errorf("appserverproto: unknown single-select kind: %d", s.Kind)
	}
}

// UnmarshalJSON decodes the untagged single-select union in serde order.
func (s *McpElicitationSingleSelectEnumSchema) UnmarshalJSON(data []byte) error {
	var untitled McpElicitationUntitledSingleSelectEnumSchema
	if err := strictUnmarshal(data, &untitled); err == nil {
		*s = McpElicitationSingleSelectEnumSchema{
			Kind:     McpElicitationSingleSelectUntitled,
			Untitled: &untitled,
		}
		return nil
	}
	var titled McpElicitationTitledSingleSelectEnumSchema
	if err := strictUnmarshal(data, &titled); err == nil {
		*s = McpElicitationSingleSelectEnumSchema{
			Kind:   McpElicitationSingleSelectTitled,
			Titled: &titled,
		}
		return nil
	}
	return fmt.Errorf("appserverproto: McpElicitationSingleSelectEnumSchema matched no variant: %s", string(data))
}

// McpElicitationMultiSelectKind discriminates the untagged multi-select union.
type McpElicitationMultiSelectKind int

const (
	// McpElicitationMultiSelectUntitled is the untitled variant.
	McpElicitationMultiSelectUntitled McpElicitationMultiSelectKind = iota
	// McpElicitationMultiSelectTitled is the titled variant.
	McpElicitationMultiSelectTitled
)

// McpElicitationMultiSelectEnumSchema is the untagged multi-select union.
//
// Rust: v2::McpElicitationMultiSelectEnumSchema, untagged.
type McpElicitationMultiSelectEnumSchema struct {
	Kind     McpElicitationMultiSelectKind
	Untitled *McpElicitationUntitledMultiSelectEnumSchema
	Titled   *McpElicitationTitledMultiSelectEnumSchema
}

// MarshalJSON emits the active multi-select variant.
func (s McpElicitationMultiSelectEnumSchema) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case McpElicitationMultiSelectUntitled:
		if s.Untitled == nil {
			return nil, fmt.Errorf("appserverproto: untitled multi-select schema is nil")
		}
		return json.Marshal(s.Untitled)
	case McpElicitationMultiSelectTitled:
		if s.Titled == nil {
			return nil, fmt.Errorf("appserverproto: titled multi-select schema is nil")
		}
		return json.Marshal(s.Titled)
	default:
		return nil, fmt.Errorf("appserverproto: unknown multi-select kind: %d", s.Kind)
	}
}

// UnmarshalJSON decodes the untagged multi-select union in serde order.
func (s *McpElicitationMultiSelectEnumSchema) UnmarshalJSON(data []byte) error {
	var untitled McpElicitationUntitledMultiSelectEnumSchema
	if err := strictUnmarshal(data, &untitled); err == nil {
		*s = McpElicitationMultiSelectEnumSchema{
			Kind:     McpElicitationMultiSelectUntitled,
			Untitled: &untitled,
		}
		return nil
	}
	var titled McpElicitationTitledMultiSelectEnumSchema
	if err := strictUnmarshal(data, &titled); err == nil {
		*s = McpElicitationMultiSelectEnumSchema{
			Kind:   McpElicitationMultiSelectTitled,
			Titled: &titled,
		}
		return nil
	}
	return fmt.Errorf("appserverproto: McpElicitationMultiSelectEnumSchema matched no variant: %s", string(data))
}

// McpElicitationEnumKind discriminates the untagged enum-schema union.
type McpElicitationEnumKind int

const (
	// McpElicitationEnumSingleSelect is a single-select enum.
	McpElicitationEnumSingleSelect McpElicitationEnumKind = iota
	// McpElicitationEnumMultiSelect is a multi-select enum.
	McpElicitationEnumMultiSelect
	// McpElicitationEnumLegacy is the legacy titled enum.
	McpElicitationEnumLegacy
)

// McpElicitationEnumSchema is the untagged enum-schema union. serde tries
// SingleSelect, MultiSelect, then Legacy.
//
// Rust: v2::McpElicitationEnumSchema, untagged.
type McpElicitationEnumSchema struct {
	Kind         McpElicitationEnumKind
	SingleSelect *McpElicitationSingleSelectEnumSchema
	MultiSelect  *McpElicitationMultiSelectEnumSchema
	Legacy       *McpElicitationLegacyTitledEnumSchema
}

// MarshalJSON emits the active enum-schema variant.
func (s McpElicitationEnumSchema) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case McpElicitationEnumSingleSelect:
		if s.SingleSelect == nil {
			return nil, fmt.Errorf("appserverproto: single-select enum schema is nil")
		}
		return json.Marshal(s.SingleSelect)
	case McpElicitationEnumMultiSelect:
		if s.MultiSelect == nil {
			return nil, fmt.Errorf("appserverproto: multi-select enum schema is nil")
		}
		return json.Marshal(s.MultiSelect)
	case McpElicitationEnumLegacy:
		if s.Legacy == nil {
			return nil, fmt.Errorf("appserverproto: legacy enum schema is nil")
		}
		return json.Marshal(s.Legacy)
	default:
		return nil, fmt.Errorf("appserverproto: unknown enum-schema kind: %d", s.Kind)
	}
}

// UnmarshalJSON decodes the untagged enum-schema union in serde order.
func (s *McpElicitationEnumSchema) UnmarshalJSON(data []byte) error {
	var single McpElicitationSingleSelectEnumSchema
	if err := single.UnmarshalJSON(data); err == nil {
		*s = McpElicitationEnumSchema{Kind: McpElicitationEnumSingleSelect, SingleSelect: &single}
		return nil
	}
	var multi McpElicitationMultiSelectEnumSchema
	if err := multi.UnmarshalJSON(data); err == nil {
		*s = McpElicitationEnumSchema{Kind: McpElicitationEnumMultiSelect, MultiSelect: &multi}
		return nil
	}
	var legacy McpElicitationLegacyTitledEnumSchema
	if err := strictUnmarshal(data, &legacy); err == nil {
		*s = McpElicitationEnumSchema{Kind: McpElicitationEnumLegacy, Legacy: &legacy}
		return nil
	}
	return fmt.Errorf("appserverproto: McpElicitationEnumSchema matched no variant: %s", string(data))
}

// McpElicitationPrimitiveKind discriminates the untagged primitive-schema union.
type McpElicitationPrimitiveKind int

const (
	// McpElicitationPrimitiveEnum is an enum schema.
	McpElicitationPrimitiveEnum McpElicitationPrimitiveKind = iota
	// McpElicitationPrimitiveString is a string schema.
	McpElicitationPrimitiveString
	// McpElicitationPrimitiveNumber is a number schema.
	McpElicitationPrimitiveNumber
	// McpElicitationPrimitiveBoolean is a boolean schema.
	McpElicitationPrimitiveBoolean
)

// McpElicitationPrimitiveSchema is the untagged primitive-property union. serde
// tries Enum, String, Number, then Boolean.
//
// Rust: v2::McpElicitationPrimitiveSchema, untagged.
type McpElicitationPrimitiveSchema struct {
	Kind    McpElicitationPrimitiveKind
	Enum    *McpElicitationEnumSchema
	String  *McpElicitationStringSchema
	Number  *McpElicitationNumberSchema
	Boolean *McpElicitationBooleanSchema
}

// MarshalJSON emits the active primitive-property variant.
func (s McpElicitationPrimitiveSchema) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case McpElicitationPrimitiveEnum:
		if s.Enum == nil {
			return nil, fmt.Errorf("appserverproto: enum primitive schema is nil")
		}
		return json.Marshal(s.Enum)
	case McpElicitationPrimitiveString:
		if s.String == nil {
			return nil, fmt.Errorf("appserverproto: string primitive schema is nil")
		}
		return json.Marshal(s.String)
	case McpElicitationPrimitiveNumber:
		if s.Number == nil {
			return nil, fmt.Errorf("appserverproto: number primitive schema is nil")
		}
		return json.Marshal(s.Number)
	case McpElicitationPrimitiveBoolean:
		if s.Boolean == nil {
			return nil, fmt.Errorf("appserverproto: boolean primitive schema is nil")
		}
		return json.Marshal(s.Boolean)
	default:
		return nil, fmt.Errorf("appserverproto: unknown primitive-schema kind: %d", s.Kind)
	}
}

// UnmarshalJSON decodes the untagged primitive-property union in serde order.
func (s *McpElicitationPrimitiveSchema) UnmarshalJSON(data []byte) error {
	var en McpElicitationEnumSchema
	if err := en.UnmarshalJSON(data); err == nil {
		*s = McpElicitationPrimitiveSchema{Kind: McpElicitationPrimitiveEnum, Enum: &en}
		return nil
	}
	var str McpElicitationStringSchema
	if err := strictUnmarshal(data, &str); err == nil {
		*s = McpElicitationPrimitiveSchema{Kind: McpElicitationPrimitiveString, String: &str}
		return nil
	}
	var num McpElicitationNumberSchema
	if err := strictUnmarshal(data, &num); err == nil {
		*s = McpElicitationPrimitiveSchema{Kind: McpElicitationPrimitiveNumber, Number: &num}
		return nil
	}
	var b McpElicitationBooleanSchema
	if err := strictUnmarshal(data, &b); err == nil {
		*s = McpElicitationPrimitiveSchema{Kind: McpElicitationPrimitiveBoolean, Boolean: &b}
		return nil
	}
	return fmt.Errorf("appserverproto: McpElicitationPrimitiveSchema matched no variant: %s", string(data))
}
