package codemode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// maxJSSafeInteger is 2^53 - 1, the largest integer JavaScript can represent
// exactly. It bounds the exec pragma numeric fields.
const maxJSSafeInteger uint64 = (uint64(1) << 53) - 1

// CodeModePragmaPrefix is the first-line pragma marker for exec source.
const CodeModePragmaPrefix = "// @exec:"

// Public tool names, mirroring codex.
const (
	// PublicToolName is the name of the exec tool.
	PublicToolName = "exec"
	// WaitToolName is the name of the wait tool.
	WaitToolName = "wait"
)

const deferredNestedToolsGuidance = `Some nested MCP/app tools may be omitted from this description. They are still available on the global ` + "`tools`" + ` object and listed in ` + "`ALL_TOOLS`" + `.
To find one, filter ` + "`ALL_TOOLS`" + ` by ` + "`name`" + ` and ` + "`description`" + `.`

const execDescriptionTemplate = `Run JavaScript code to orchestrate/compose tool calls
- Evaluates the provided JavaScript code in a fresh V8 isolate as an async module.
- All nested tools are available on the global ` + "`tools`" + ` object, for example ` + "`await tools.exec_command(...)`" + `. Tool names are exposed as normalized JavaScript identifiers, for example ` + "`await tools.mcp__ologs__get_profile(...)`" + `.
- Nested tool methods take either a string or an object as their input argument.
- Nested tools return either an object or a string, based on the description.
- Runs raw JavaScript -- no Node, no file system, no network access, no console.
- Accepts raw JavaScript source text, not JSON, quoted strings, or markdown code fences.
- You may optionally start the tool input with a first-line pragma like ` + "`// @exec: {\"yield_time_ms\": 10000, \"max_output_tokens\": 1000}`" + `.
- ` + "`yield_time_ms`" + ` asks ` + "`exec`" + ` to yield early if the script is still running. Defaults to 10000 ms.
- ` + "`max_output_tokens`" + ` sets the token budget for direct ` + "`exec`" + ` results. Defaults to 10000 tokens.
- When the JS code is fully evaluated, the isolate's lifetime ends and unawaited promises are silently discarded.

- Global helpers:
- ` + "`exit()`" + `: Immediately ends the current script successfully (like an early return from the top level).
- ` + "`text(value: string | number | boolean | undefined | null)`" + `: Appends a text item. Non-string values are stringified with ` + "`JSON.stringify(...)`" + ` when possible.
- ` + "`image(imageUrlOrItem: string | { image_url: string; detail?: \"auto\" | \"low\" | \"high\" | \"original\" | null } | ImageContent, detail?: \"auto\" | \"low\" | \"high\" | \"original\" | null)`" + `: Appends an image item. ` + "`image_url`" + ` can be an HTTPS URL or a base64-encoded ` + "`data:`" + ` URL. To forward an MCP tool image, pass an individual ` + "`ImageContent`" + ` block from ` + "`result.content`" + `, for example ` + "`image(result.content[0])`" + `. MCP image blocks may request detail with ` + "`_meta: { \"codex/imageDetail\": \"original\" }`" + `. When provided, the second ` + "`detail`" + ` argument overrides any detail embedded in the first argument.
- ` + "`store(key: string, value: any)`" + `: stores a serializable value under a string key for later ` + "`exec`" + ` calls in the same session.
- ` + "`load(key: string)`" + `: returns the stored value for a string key, or ` + "`undefined`" + ` if it is missing.
- ` + "`notify(value: string | number | boolean | undefined | null)`" + `: immediately injects an extra ` + "`custom_tool_call_output`" + ` for the current ` + "`exec`" + ` call. Values are stringified like ` + "`text(...)`" + `.
- ` + "`setTimeout(callback: () => void, delayMs?: number)`" + `: schedules a callback to run later and returns a timeout id. Pending timeouts do not keep ` + "`exec`" + ` alive by themselves; await an explicit promise if you need to wait for one.
- ` + "`clearTimeout(timeoutId?: number)`" + `: cancels a timeout created by ` + "`setTimeout`" + `.
- ` + "`ALL_TOOLS`" + `: metadata for the enabled nested tools as ` + "`{ name, description }`" + ` entries.
- ` + "`yield_control()`" + `: yields the accumulated output to the model immediately while the script keeps running.`

const waitDescriptionTemplate = `- Use ` + "`wait`" + ` only after ` + "`exec`" + ` returns ` + "`Script running with cell ID ...`" + `.
- ` + "`cell_id`" + ` identifies the running ` + "`exec`" + ` cell to resume.
- ` + "`yield_time_ms`" + ` controls how long to wait for more output before yielding again. Defaults to 10000 ms.
- ` + "`max_tokens`" + ` limits how much new output this wait call returns. Defaults to 10000 tokens.
- ` + "`terminate: true`" + ` stops the running cell; false or omitted waits for output.
- ` + "`wait`" + ` returns only the new output since the last yield, or the final completion or termination result for that cell.
- If the cell is still running, ` + "`wait`" + ` may yield again with the same ` + "`cell_id`" + `.
- If the cell has already finished, ` + "`wait`" + ` returns the completed result and closes the cell.`

// mcpTypeScriptPreamble is the shared MCP CallToolResult typing block, based on
// https://modelcontextprotocol.io/specification/draft/schema#calltoolresult.
const mcpTypeScriptPreamble = `type Role = "user" | "assistant";
type MetaObject = Record<string, unknown>;
type Annotations = {
  audience?: Role[];
  priority?: number;
  lastModified?: string;
};
type Icon = {
  src: string;
  mimeType?: string;
  sizes?: string[];
  theme?: "light" | "dark";
};
type TextResourceContents = {
  uri: string;
  mimeType?: string;
  _meta?: MetaObject;
  text: string;
};
type BlobResourceContents = {
  uri: string;
  mimeType?: string;
  _meta?: MetaObject;
  blob: string;
};
type TextContent = {
  type: "text";
  text: string;
  annotations?: Annotations;
  _meta?: MetaObject;
};
type ImageContent = {
  type: "image";
  data: string;
  mimeType: string;
  annotations?: Annotations;
  _meta?: MetaObject;
};
type AudioContent = {
  type: "audio";
  data: string;
  mimeType: string;
  annotations?: Annotations;
  _meta?: MetaObject;
};
type ResourceLink = {
  icons?: Icon[];
  name: string;
  title?: string;
  uri: string;
  description?: string;
  mimeType?: string;
  annotations?: Annotations;
  size?: number;
  _meta?: MetaObject;
  type: "resource_link";
};
type EmbeddedResource = {
  type: "resource";
  resource: TextResourceContents | BlobResourceContents;
  annotations?: Annotations;
  _meta?: MetaObject;
};
type ContentBlock =
  | TextContent
  | ImageContent
  | AudioContent
  | ResourceLink
  | EmbeddedResource;
type CallToolResult<TStructured = { [key: string]: unknown }> = {
  _meta?: MetaObject;
  content: ContentBlock[];
  isError?: boolean;
  structuredContent?: TStructured;
  [key: string]: unknown;
};`

// CodeModeToolKind mirrors codex's `CodeModeToolKind` enum. It controls how a
// nested tool's argument is typed in the generated TypeScript declaration.
type CodeModeToolKind int

// CodeModeToolKind variants.
const (
	// CodeModeToolKindFunction takes a JSON-object argument named "args".
	CodeModeToolKindFunction CodeModeToolKind = iota
	// CodeModeToolKindFreeform takes a string argument named "input".
	CodeModeToolKindFreeform
)

// String renders the snake_case serde name.
func (k CodeModeToolKind) String() string {
	if k == CodeModeToolKindFreeform {
		return "freeform"
	}
	return "function"
}

// MarshalJSON emits the snake_case variant name.
func (k CodeModeToolKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

// UnmarshalJSON parses the snake_case variant name.
func (k *CodeModeToolKind) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode tool kind: %w", err)
	}
	switch raw {
	case "function":
		*k = CodeModeToolKindFunction
	case "freeform":
		*k = CodeModeToolKindFreeform
	default:
		return fmt.Errorf("invalid tool kind %q", raw)
	}
	return nil
}

// ToolDefinition mirrors codex's `ToolDefinition`. Schemas are decoded JSON
// values (objects, bools, etc.) and may be nil when absent.
type ToolDefinition struct {
	Name         string
	ToolName     protocol.ToolName
	Description  string
	Kind         CodeModeToolKind
	InputSchema  any
	OutputSchema any
}

// ToolNamespaceDescription mirrors codex's `ToolNamespaceDescription`.
type ToolNamespaceDescription struct {
	Name        string
	Description string
}

// EnabledToolMetadata mirrors codex's `EnabledToolMetadata`. It carries the
// runtime-facing details of a nested tool.
type EnabledToolMetadata struct {
	ToolName    protocol.ToolName `json:"tool_name"`
	GlobalName  string            `json:"global_name"`
	Description string            `json:"description"`
	Kind        CodeModeToolKind  `json:"kind"`
}

// ParsedExecSource mirrors codex's `ParsedExecSource`: the JS body with the
// optional pragma stripped, plus parsed pragma overrides.
type ParsedExecSource struct {
	Code            string
	YieldTimeMS     *uint64
	MaxOutputTokens *uint64
}

// ParseExecSource parses the exec tool input, extracting the optional first-line
// pragma. It mirrors codex's `parse_exec_source` error strings exactly.
func ParseExecSource(input string) (ParsedExecSource, error) {
	if strings.TrimSpace(input) == "" {
		return ParsedExecSource{}, fmt.Errorf(
			"exec expects raw JavaScript source text (non-empty). Provide JS only, optionally with first-line `// @exec: {\"yield_time_ms\": 10000, \"max_output_tokens\": 1000}`.",
		)
	}

	parsed := ParsedExecSource{Code: input}

	firstLine, rest, _ := strings.Cut(input, "\n")
	trimmed := strings.TrimLeft(firstLine, " \t\r\n\x0c\x0b")
	directiveRaw, ok := strings.CutPrefix(trimmed, CodeModePragmaPrefix)
	if !ok {
		return parsed, nil
	}

	if strings.TrimSpace(rest) == "" {
		return ParsedExecSource{}, fmt.Errorf(
			"exec pragma must be followed by JavaScript source on subsequent lines",
		)
	}

	directive := strings.TrimSpace(directiveRaw)
	if directive == "" {
		return ParsedExecSource{}, fmt.Errorf(
			"exec pragma must be a JSON object with supported fields `yield_time_ms` and `max_output_tokens`",
		)
	}

	var generic any
	if err := json.Unmarshal([]byte(directive), &generic); err != nil {
		return ParsedExecSource{}, fmt.Errorf(
			"exec pragma must be valid JSON with supported fields `yield_time_ms` and `max_output_tokens`: %w", err,
		)
	}
	object, ok := generic.(map[string]any)
	if !ok {
		return ParsedExecSource{}, fmt.Errorf(
			"exec pragma must be a JSON object with supported fields `yield_time_ms` and `max_output_tokens`",
		)
	}
	for key := range object {
		switch key {
		case "yield_time_ms", "max_output_tokens":
		default:
			return ParsedExecSource{}, fmt.Errorf(
				"exec pragma only supports `yield_time_ms` and `max_output_tokens`; got `%s`", key,
			)
		}
	}

	yieldTimeMS, err := decodePragmaUint(object, "yield_time_ms")
	if err != nil {
		return ParsedExecSource{}, err
	}
	maxOutputTokens, err := decodePragmaUint(object, "max_output_tokens")
	if err != nil {
		return ParsedExecSource{}, err
	}
	if yieldTimeMS != nil && *yieldTimeMS > maxJSSafeInteger {
		return ParsedExecSource{}, fmt.Errorf(
			"exec pragma field `yield_time_ms` must be a non-negative safe integer",
		)
	}
	if maxOutputTokens != nil && *maxOutputTokens > maxJSSafeInteger {
		return ParsedExecSource{}, fmt.Errorf(
			"exec pragma field `max_output_tokens` must be a non-negative safe integer",
		)
	}

	parsed.Code = rest
	parsed.YieldTimeMS = yieldTimeMS
	parsed.MaxOutputTokens = maxOutputTokens
	return parsed, nil
}

// decodePragmaUint validates that a present pragma field is a non-negative
// integer expressible as a JS-safe value. The error strings match codex's
// serde error wrapping for the pragma numeric fields.
func decodePragmaUint(object map[string]any, key string) (*uint64, error) {
	raw, present := object[key]
	if !present {
		return nil, nil
	}
	number, ok := raw.(float64)
	if !ok || number < 0 || number != float64(uint64(number)) {
		return nil, fmt.Errorf(
			"exec pragma fields `yield_time_ms` and `max_output_tokens` must be non-negative safe integers: invalid value for `%s`", key,
		)
	}
	value := uint64(number)
	return &value, nil
}

// IsCodeModeNestedTool reports whether a tool name is a nested tool (i.e. not
// the exec or wait tool itself).
func IsCodeModeNestedTool(toolName string) bool {
	return toolName != PublicToolName && toolName != WaitToolName
}

// BuildWaitToolDescription returns the static wait tool description.
func BuildWaitToolDescription() string {
	return waitDescriptionTemplate
}

// NormalizeCodeModeIdentifier rewrites a tool key into a valid JavaScript
// identifier, replacing invalid characters with underscores. Mirrors codex's
// `normalize_code_mode_identifier`.
func NormalizeCodeModeIdentifier(toolKey string) string {
	var builder strings.Builder
	for index, ch := range toolKey {
		var valid bool
		if index == 0 {
			valid = ch == '_' || ch == '$' || isASCIILetter(ch)
		} else {
			valid = ch == '_' || ch == '$' || isASCIILetter(ch) || isASCIIDigit(ch)
		}
		if valid {
			builder.WriteRune(ch)
		} else {
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "_"
	}
	return builder.String()
}

func isASCIILetter(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isASCIIDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

// AugmentToolDefinition returns a copy of the definition whose description has a
// typed TypeScript declaration appended (for nested tools only). Mirrors codex's
// `augment_tool_definition` and preserves immutability.
func AugmentToolDefinition(definition ToolDefinition) ToolDefinition {
	if definition.Name == PublicToolName {
		return definition
	}
	augmented := definition
	augmented.Description = renderCodeModeSampleForDefinition(definition)
	return augmented
}

// EnabledToolMetadataOf projects a ToolDefinition into its runtime metadata.
func EnabledToolMetadataOf(definition ToolDefinition) EnabledToolMetadata {
	return EnabledToolMetadata{
		ToolName:    definition.ToolName,
		GlobalName:  NormalizeCodeModeIdentifier(definition.Name),
		Description: definition.Description,
		Kind:        definition.Kind,
	}
}

// BuildExecToolDescription assembles the exec tool description, optionally
// including per-tool TypeScript declarations when code_mode_only is set. Mirrors
// codex's `build_exec_tool_description`.
func BuildExecToolDescription(
	enabledTools []ToolDefinition,
	namespaceDescriptions map[string]ToolNamespaceDescription,
	codeModeOnly bool,
	deferredToolsAvailable bool,
) string {
	sections := []string{execDescriptionTemplate}
	if deferredToolsAvailable {
		sections = append(sections, deferredNestedToolsGuidance)
	}
	if !codeModeOnly {
		return strings.Join(sections, "\n\n")
	}

	if len(enabledTools) > 0 {
		haveCurrentNamespace := false
		currentNamespace := ""
		nestedToolSections := make([]string, 0, len(enabledTools))
		hasMCPTools := false
		for _, tool := range enabledTools {
			if mcpStructuredContentSchema(tool.OutputSchema) != nil {
				hasMCPTools = true
				break
			}
		}

		for _, tool := range enabledTools {
			name := tool.Name
			nestedDescription := strings.TrimSpace(renderCodeModeSampleForDefinition(tool))

			var namespaceDescription *ToolNamespaceDescription
			if tool.ToolName.Namespace != nil {
				if found, ok := namespaceDescriptions[*tool.ToolName.Namespace]; ok {
					namespaceDescription = &found
				}
			}
			nextHaveNamespace := namespaceDescription != nil
			nextNamespace := ""
			if namespaceDescription != nil {
				nextNamespace = namespaceDescription.Name
			}
			if nextHaveNamespace != haveCurrentNamespace || nextNamespace != currentNamespace {
				if namespaceDescription != nil {
					namespaceText := strings.TrimSpace(namespaceDescription.Description)
					if namespaceText != "" {
						nestedToolSections = append(nestedToolSections, fmt.Sprintf(
							"## %s\n%s", namespaceDescription.Name, namespaceText,
						))
					}
				}
				haveCurrentNamespace = nextHaveNamespace
				currentNamespace = nextNamespace
			}

			globalName := NormalizeCodeModeIdentifier(name)
			heading := renderToolHeading(globalName, name)
			if nestedDescription == "" {
				nestedToolSections = append(nestedToolSections, heading)
			} else {
				nestedToolSections = append(nestedToolSections, fmt.Sprintf("%s\n%s", heading, nestedDescription))
			}
		}

		if hasMCPTools {
			sections = append(sections, fmt.Sprintf("Shared MCP Types:\n```ts\n%s\n```", mcpTypeScriptPreamble))
		}
		sections = append(sections, strings.Join(nestedToolSections, "\n\n"))
	}

	return strings.Join(sections, "\n\n")
}

// RenderCodeModeSample appends the typed `declare const tools` TypeScript sample
// to a tool description. Mirrors codex's `render_code_mode_sample`.
func RenderCodeModeSample(description, toolName, inputName, inputType, outputType string) string {
	declaration := fmt.Sprintf(
		"declare const tools: { %s };",
		renderCodeModeToolDeclaration(toolName, inputName, inputType, outputType),
	)
	return fmt.Sprintf("%s\n\nexec tool declaration:\n```ts\n%s\n```", description, declaration)
}

func renderCodeModeSampleForDefinition(definition ToolDefinition) string {
	inputName := "args"
	if definition.Kind == CodeModeToolKindFreeform {
		inputName = "input"
	}

	var inputType string
	switch definition.Kind {
	case CodeModeToolKindFunction:
		if definition.InputSchema != nil {
			inputType = RenderJSONSchemaToTypeScript(definition.InputSchema)
		} else {
			inputType = "unknown"
		}
	case CodeModeToolKindFreeform:
		inputType = "string"
	}

	var outputType string
	if structured := mcpStructuredContentSchema(definition.OutputSchema); structured != nil {
		structuredType := RenderJSONSchemaToTypeScript(*structured)
		if structuredType == "unknown" {
			outputType = "CallToolResult"
		} else {
			outputType = fmt.Sprintf("CallToolResult<%s>", structuredType)
		}
	} else if definition.OutputSchema != nil {
		outputType = RenderJSONSchemaToTypeScript(definition.OutputSchema)
	} else {
		outputType = "unknown"
	}

	return RenderCodeModeSample(definition.Description, definition.Name, inputName, inputType, outputType)
}

func renderCodeModeToolDeclaration(toolName, inputName, inputType, outputType string) string {
	normalized := NormalizeCodeModeIdentifier(toolName)
	return fmt.Sprintf("%s(%s: %s): Promise<%s>;", normalized, inputName, inputType, outputType)
}

func renderToolHeading(globalName, rawName string) string {
	if globalName == rawName {
		return fmt.Sprintf("### `%s`", globalName)
	}
	return fmt.Sprintf("### `%s` (`%s`)", globalName, rawName)
}

// mcpStructuredContentSchema detects whether the output schema is an MCP
// CallToolResult shape and, if so, returns the structuredContent sub-schema
// (defaulting to the permissive `true` schema). Mirrors codex's
// `mcp_structured_content_schema`.
func mcpStructuredContentSchema(outputSchema any) *any {
	root, ok := outputSchema.(map[string]any)
	if !ok {
		return nil
	}
	properties, ok := root["properties"].(map[string]any)
	if !ok {
		return nil
	}
	contentSchema, ok := properties["content"].(map[string]any)
	if !ok {
		return nil
	}
	if asString(contentSchema["type"]) != "array" {
		return nil
	}
	items, ok := contentSchema["items"].(map[string]any)
	if !ok || asString(items["type"]) != "object" {
		return nil
	}
	if !objectFieldHasType(properties, "isError", "boolean") {
		return nil
	}
	if !objectFieldHasType(properties, "_meta", "object") {
		return nil
	}

	if structured, present := properties["structuredContent"]; present {
		return &structured
	}
	permissive := any(true)
	return &permissive
}

func objectFieldHasType(properties map[string]any, field, wantType string) bool {
	schema, ok := properties[field].(map[string]any)
	if !ok {
		return false
	}
	return asString(schema["type"]) == wantType
}

// sortedKeys returns the keys of a string-keyed map in ascending order, matching
// the deterministic iteration codex relies on (BTreeMap / sort_unstable_by_key).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func asString(value any) string {
	str, _ := value.(string)
	return str
}
