package tools

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// FreeformTool is a custom (freeform / grammar) tool in the Responses API.
// Mirrors Rust `FreeformTool`. Field names are emitted verbatim.
type FreeformTool struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Format      FreeformToolFormat `json:"format"`
}

// FreeformToolFormat describes a freeform tool's grammar format. Mirrors Rust
// `FreeformToolFormat`.
type FreeformToolFormat struct {
	Type       string `json:"type"`
	Syntax     string `json:"syntax"`
	Definition string `json:"definition"`
}

// ResponsesApiTool is a function tool serialized into the OpenAI Responses API
// tool format. Mirrors Rust `ResponsesApiTool`.
//
// Serialization: `name`, `description`, `strict`, `parameters` are always
// emitted. `defer_loading` uses `skip_serializing_if = "Option::is_none"`.
// `output_schema` is `#[serde(skip)]` in Rust and therefore never serialized.
type ResponsesApiTool struct {
	Name        string
	Description string
	// Strict is always emitted. The next stage constructs specs with strict:false.
	Strict bool
	// DeferLoading is omitted when nil.
	DeferLoading *bool
	Parameters   JsonSchema
	// OutputSchema is never serialized (Rust `#[serde(skip)]`); it is retained for
	// downstream consumers such as code mode. Nil when absent.
	OutputSchema json.RawMessage
}

// MarshalJSON emits the wire shape, preserving field order and the skip rules.
func (t ResponsesApiTool) MarshalJSON() ([]byte, error) {
	m := orderedMap{}
	m.set("name", t.Name)
	m.set("description", t.Description)
	m.set("strict", t.Strict)
	if t.DeferLoading != nil {
		m.set("defer_loading", *t.DeferLoading)
	}
	m.set("parameters", t.Parameters)
	return m.MarshalJSON()
}

// LoadableToolSpecKind discriminates a LoadableToolSpec. Mirrors the Rust enum
// variants; serialized via the `type` tag.
type LoadableToolSpecKind int

// LoadableToolSpecKind variants.
const (
	// LoadableToolSpecKindFunction wraps a single function tool.
	LoadableToolSpecKindFunction LoadableToolSpecKind = iota
	// LoadableToolSpecKindNamespace wraps a tool namespace.
	LoadableToolSpecKindNamespace
)

// LoadableToolSpec is a tool spec eligible for deferred loading: either a single
// function tool or a namespace. Mirrors Rust `LoadableToolSpec`
// (`#[serde(tag = "type")]`).
type LoadableToolSpec struct {
	Kind LoadableToolSpecKind

	// Function is set for the Function variant.
	Function *ResponsesApiTool
	// Namespace is set for the Namespace variant.
	Namespace *ResponsesApiNamespace
}

// FunctionLoadableToolSpec constructs a Function LoadableToolSpec.
func FunctionLoadableToolSpec(tool ResponsesApiTool) LoadableToolSpec {
	return LoadableToolSpec{Kind: LoadableToolSpecKindFunction, Function: &tool}
}

// NamespaceLoadableToolSpec constructs a Namespace LoadableToolSpec.
func NamespaceLoadableToolSpec(namespace ResponsesApiNamespace) LoadableToolSpec {
	return LoadableToolSpec{Kind: LoadableToolSpecKindNamespace, Namespace: &namespace}
}

// MarshalJSON emits the internally-tagged form with a leading "type" field.
func (s LoadableToolSpec) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case LoadableToolSpecKindFunction:
		if s.Function == nil {
			return nil, fmt.Errorf("LoadableToolSpec function variant has nil tool")
		}
		return marshalTaggedTool("function", *s.Function)
	case LoadableToolSpecKindNamespace:
		if s.Namespace == nil {
			return nil, fmt.Errorf("LoadableToolSpec namespace variant has nil namespace")
		}
		return marshalTaggedNamespace("namespace", *s.Namespace)
	default:
		return nil, fmt.Errorf("unknown LoadableToolSpec kind: %d", s.Kind)
	}
}

// ResponsesApiNamespace groups related function tools under a shared name.
// Mirrors Rust `ResponsesApiNamespace`.
type ResponsesApiNamespace struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Tools       []ResponsesApiNamespaceTool `json:"tools"`
}

// DefaultNamespaceDescription returns the default description for a namespace.
// Mirrors Rust `default_namespace_description`.
func DefaultNamespaceDescription(namespaceName string) string {
	return fmt.Sprintf("Tools in the %s namespace.", namespaceName)
}

// ResponsesApiNamespaceToolKind discriminates a namespace child tool. Mirrors the
// Rust enum (`#[serde(tag = "type")]`) which currently has a single variant.
type ResponsesApiNamespaceToolKind int

// ResponsesApiNamespaceToolKind variants.
const (
	// ResponsesApiNamespaceToolKindFunction wraps a function tool.
	ResponsesApiNamespaceToolKindFunction ResponsesApiNamespaceToolKind = iota
)

// ResponsesApiNamespaceTool is a tool nested inside a namespace. Mirrors Rust
// `ResponsesApiNamespaceTool`.
type ResponsesApiNamespaceTool struct {
	Kind ResponsesApiNamespaceToolKind
	// Function is set for the Function variant.
	Function ResponsesApiTool
}

// FunctionNamespaceTool constructs a Function namespace tool.
func FunctionNamespaceTool(tool ResponsesApiTool) ResponsesApiNamespaceTool {
	return ResponsesApiNamespaceTool{Kind: ResponsesApiNamespaceToolKindFunction, Function: tool}
}

// MarshalJSON emits the internally-tagged function tool.
func (t ResponsesApiNamespaceTool) MarshalJSON() ([]byte, error) {
	switch t.Kind {
	case ResponsesApiNamespaceToolKindFunction:
		return marshalTaggedTool("function", t.Function)
	default:
		return nil, fmt.Errorf("unknown ResponsesApiNamespaceTool kind: %d", t.Kind)
	}
}

// DynamicToolToResponsesApiTool lowers a dynamic tool spec into a function tool.
// Mirrors Rust `dynamic_tool_to_responses_api_tool`.
func DynamicToolToResponsesApiTool(tool protocol.DynamicToolSpec) (ResponsesApiTool, error) {
	def, err := ParseDynamicTool(tool)
	if err != nil {
		return ResponsesApiTool{}, fmt.Errorf("parse dynamic tool: %w", err)
	}
	return ToolDefinitionToResponsesApiTool(def), nil
}

// CoalesceLoadableToolSpecs merges namespaces that share a name, appending their
// child tools. Function specs pass through unchanged and preserve order. Mirrors
// Rust `coalesce_loadable_tool_specs`.
func CoalesceLoadableToolSpecs(specs []LoadableToolSpec) []LoadableToolSpec {
	coalesced := make([]LoadableToolSpec, 0, len(specs))
	for _, spec := range specs {
		switch spec.Kind {
		case LoadableToolSpecKindFunction:
			coalesced = append(coalesced, spec)
		case LoadableToolSpecKindNamespace:
			merged := false
			for i := range coalesced {
				existing := coalesced[i]
				if existing.Kind == LoadableToolSpecKindNamespace &&
					existing.Namespace.Name == spec.Namespace.Name {
					existing.Namespace.Tools = append(existing.Namespace.Tools, spec.Namespace.Tools...)
					coalesced[i] = existing
					merged = true
					break
				}
			}
			if !merged {
				coalesced = append(coalesced, spec)
			}
		}
	}
	return coalesced
}

// McpToolToResponsesApiTool lowers an MCP tool into a function tool, renaming it
// to the given tool name. Mirrors Rust `mcp_tool_to_responses_api_tool`.
func McpToolToResponsesApiTool(toolName protocol.ToolName, tool protocol.Tool) (ResponsesApiTool, error) {
	def, err := ParseMcpTool(tool)
	if err != nil {
		return ResponsesApiTool{}, fmt.Errorf("parse mcp tool: %w", err)
	}
	return ToolDefinitionToResponsesApiTool(def.Renamed(toolName.Name)), nil
}

// McpToolToDeferredResponsesApiTool lowers an MCP tool into a deferred function
// tool. Mirrors Rust `mcp_tool_to_deferred_responses_api_tool`.
func McpToolToDeferredResponsesApiTool(toolName protocol.ToolName, tool protocol.Tool) (ResponsesApiTool, error) {
	def, err := ParseMcpTool(tool)
	if err != nil {
		return ResponsesApiTool{}, fmt.Errorf("parse mcp tool: %w", err)
	}
	return ToolDefinitionToResponsesApiTool(def.Renamed(toolName.Name).IntoDeferred()), nil
}

// ToolDefinitionToResponsesApiTool lowers a ToolDefinition into a function tool
// with strict:false. Mirrors Rust `tool_definition_to_responses_api_tool`: a
// false `defer_loading` is dropped (None), and a true value becomes Some(true).
func ToolDefinitionToResponsesApiTool(def ToolDefinition) ResponsesApiTool {
	var deferLoading *bool
	if def.DeferLoading {
		v := true
		deferLoading = &v
	}
	return ResponsesApiTool{
		Name:         def.Name,
		Description:  def.Description,
		Strict:       false,
		DeferLoading: deferLoading,
		Parameters:   def.InputSchema,
		OutputSchema: def.OutputSchema,
	}
}

// marshalTaggedTool emits a function tool prefixed with a "type" tag.
func marshalTaggedTool(tag string, tool ResponsesApiTool) ([]byte, error) {
	m := orderedMap{}
	m.set("type", tag)
	m.set("name", tool.Name)
	m.set("description", tool.Description)
	m.set("strict", tool.Strict)
	if tool.DeferLoading != nil {
		m.set("defer_loading", *tool.DeferLoading)
	}
	m.set("parameters", tool.Parameters)
	return m.MarshalJSON()
}

// marshalTaggedNamespace emits a namespace prefixed with a "type" tag.
func marshalTaggedNamespace(tag string, namespace ResponsesApiNamespace) ([]byte, error) {
	m := orderedMap{}
	m.set("type", tag)
	m.set("name", namespace.Name)
	m.set("description", namespace.Description)
	m.set("tools", namespace.Tools)
	return m.MarshalJSON()
}
