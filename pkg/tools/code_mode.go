package tools

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/codemode"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// AugmentToolSpecForCodeMode augments tool descriptions with code-mode-specific
// exec samples. Mirrors Rust `augment_tool_spec_for_code_mode`. The input spec
// is not mutated; a new spec is returned.
func AugmentToolSpecForCodeMode(spec ToolSpec) ToolSpec {
	switch spec.Kind {
	case ToolSpecKindFunction:
		description, ok := augmentedDescriptionForSpec(spec)
		if !ok {
			return spec
		}
		tool := *spec.Function
		tool.Description = description
		return FunctionToolSpec(tool)
	case ToolSpecKindFreeform:
		description, ok := augmentedDescriptionForSpec(spec)
		if !ok {
			return spec
		}
		tool := *spec.Freeform
		tool.Description = description
		return FreeformToolSpec(tool)
	case ToolSpecKindNamespace:
		namespace := *spec.Namespace
		tools := make([]ResponsesApiNamespaceTool, len(namespace.Tools))
		copy(tools, namespace.Tools)
		for i := range tools {
			tool := tools[i].Function
			toolName := protocol.NamespacedToolName(namespace.Name, tool.Name)
			def := codemode.ToolDefinition{
				Name:         CodeModeNameForToolName(toolName),
				ToolName:     toolName,
				Description:  tool.Description,
				Kind:         codemode.CodeModeToolKindFunction,
				InputSchema:  schemaToValue(tool.Parameters),
				OutputSchema: rawToValue(tool.OutputSchema),
			}
			tool.Description = codemode.AugmentToolDefinition(def).Description
			tools[i] = FunctionNamespaceTool(tool)
		}
		namespace.Tools = tools
		return NamespaceToolSpec(namespace)
	default:
		return spec
	}
}

// ToolSpecToCodeModeToolDefinition converts a supported nested tool spec into the
// code-mode runtime shape, returning false when the spec is not a code-mode
// nested tool. Mirrors Rust `tool_spec_to_code_mode_tool_definition`.
func ToolSpecToCodeModeToolDefinition(spec ToolSpec) (codemode.ToolDefinition, bool) {
	def, ok := codeModeToolDefinitionForSpec(spec)
	if !ok {
		return codemode.ToolDefinition{}, false
	}
	if !codemode.IsCodeModeNestedTool(def.Name) {
		return codemode.ToolDefinition{}, false
	}
	return codemode.AugmentToolDefinition(def), true
}

// CollectCodeModeToolDefinitions collects augmented code-mode tool definitions
// from the given specs, sorted and de-duplicated by name. Mirrors Rust
// `collect_code_mode_tool_definitions`.
func CollectCodeModeToolDefinitions(specs []ToolSpec) []codemode.ToolDefinition {
	defs := collectCodeModeDefinitions(specs, true)
	return defs
}

// CollectCodeModeExecPromptToolDefinitions collects code-mode tool definitions
// without augmentation, sorted and de-duplicated by name. Mirrors Rust
// `collect_code_mode_exec_prompt_tool_definitions`.
func CollectCodeModeExecPromptToolDefinitions(specs []ToolSpec) []codemode.ToolDefinition {
	defs := collectCodeModeDefinitions(specs, false)
	return defs
}

func collectCodeModeDefinitions(specs []ToolSpec, augment bool) []codemode.ToolDefinition {
	var defs []codemode.ToolDefinition
	for _, spec := range specs {
		for _, def := range codeModeToolDefinitionsForSpec(spec) {
			if !codemode.IsCodeModeNestedTool(def.Name) {
				continue
			}
			if augment {
				def = codemode.AugmentToolDefinition(def)
			}
			defs = append(defs, def)
		}
	}
	sort.SliceStable(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return dedupByName(defs)
}

// dedupByName removes consecutive entries with the same name, mirroring Rust's
// `dedup_by` applied after sorting.
func dedupByName(defs []codemode.ToolDefinition) []codemode.ToolDefinition {
	if len(defs) == 0 {
		return defs
	}
	out := defs[:1]
	for _, def := range defs[1:] {
		if def.Name != out[len(out)-1].Name {
			out = append(out, def)
		}
	}
	return out
}

func augmentedDescriptionForSpec(spec ToolSpec) (string, bool) {
	def, ok := codeModeToolDefinitionForSpec(spec)
	if !ok {
		return "", false
	}
	return codemode.AugmentToolDefinition(def).Description, true
}

func codeModeToolDefinitionForSpec(spec ToolSpec) (codemode.ToolDefinition, bool) {
	defs := codeModeToolDefinitionsForSpec(spec)
	if len(defs) == 0 {
		return codemode.ToolDefinition{}, false
	}
	return defs[0], true
}

func codeModeToolDefinitionsForSpec(spec ToolSpec) []codemode.ToolDefinition {
	switch spec.Kind {
	case ToolSpecKindFunction:
		tool := spec.Function
		return []codemode.ToolDefinition{{
			Name:         tool.Name,
			ToolName:     protocol.PlainToolName(tool.Name),
			Description:  tool.Description,
			Kind:         codemode.CodeModeToolKindFunction,
			InputSchema:  schemaToValue(tool.Parameters),
			OutputSchema: rawToValue(tool.OutputSchema),
		}}
	case ToolSpecKindFreeform:
		tool := spec.Freeform
		return []codemode.ToolDefinition{{
			Name:         tool.Name,
			ToolName:     protocol.PlainToolName(tool.Name),
			Description:  tool.Description,
			Kind:         codemode.CodeModeToolKindFreeform,
			InputSchema:  nil,
			OutputSchema: nil,
		}}
	case ToolSpecKindNamespace:
		namespace := spec.Namespace
		defs := make([]codemode.ToolDefinition, 0, len(namespace.Tools))
		for _, nsTool := range namespace.Tools {
			tool := nsTool.Function
			toolName := protocol.NamespacedToolName(namespace.Name, tool.Name)
			defs = append(defs, codemode.ToolDefinition{
				Name:         CodeModeNameForToolName(toolName),
				ToolName:     toolName,
				Description:  tool.Description,
				Kind:         codemode.CodeModeToolKindFunction,
				InputSchema:  schemaToValue(tool.Parameters),
				OutputSchema: rawToValue(tool.OutputSchema),
			})
		}
		return defs
	default:
		// ImageGeneration / ToolSearch / WebSearch have no code-mode definitions.
		return nil
	}
}

// CodeModeNameForToolName derives the code-mode-visible name for a tool name.
// Mirrors Rust `code_mode_name_for_tool_name`.
func CodeModeNameForToolName(toolName protocol.ToolName) string {
	if toolName.Namespace == nil {
		return toolName.Name
	}
	namespace := *toolName.Namespace
	if strings.HasSuffix(namespace, "_") || strings.HasPrefix(toolName.Name, "_") {
		return namespace + toolName.Name
	}
	return namespace + "__" + toolName.Name
}

// schemaToValue marshals a JsonSchema to a decoded JSON value, mirroring Rust's
// `serde_json::to_value(&tool.parameters).ok()`. It returns nil on failure.
func schemaToValue(schema JsonSchema) any {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

// rawToValue decodes a json.RawMessage into a JSON value, returning nil when the
// message is absent or invalid (mirroring Rust's `Option<Value>`).
func rawToValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
