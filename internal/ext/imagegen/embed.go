package imagegen

import (
	_ "embed"
	"strings"
)

// imagegenDescription is the model-facing imagegen tool description. Rust:
// IMAGEGEN_DESCRIPTION (include_str!("../imagegen_description.md")).
//
//go:embed imagegen_description.md
var imagegenDescription string

// imagegenInputSchema is the tool input schema extracted from the schemars
// JsonSchema for ImagegenArgs. The Rust crate generates the root schema with
// SchemaSettings::draft2019_09 and inline_subschemas=true, then copies only the
// "properties", "required", "type", and "additionalProperties" keys into the
// tool input schema. For ImagegenArgs (prompt: String, action: enum
// {generate, edit}, deny_unknown_fields) that yields exactly this object, which
// parse_tool_input_schema then lowers into the JsonSchema subset.
const imagegenInputSchema = `{
  "properties": {
    "prompt": {
      "type": "string"
    },
    "action": {
      "type": "string",
      "enum": ["generate", "edit"]
    }
  },
  "required": ["prompt", "action"],
  "type": "object",
  "additionalProperties": false
}`

// stringReader returns an io.Reader over a string without copying, for use with
// json.Decoder.
func stringReader(s string) *strings.Reader {
	return strings.NewReader(s)
}
