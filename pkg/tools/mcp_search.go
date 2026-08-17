package tools

// Deferred MCP tool search/spec construction, porting the Rust core
// tools/handlers/mcp.rs: create_tool_spec (the namespace ToolSpec a deferred MCP
// tool advertises through tool_search), build_mcp_search_text (the BM25 document
// indexed for the tool), and McpHandler::search_info (the per-server
// ToolSearchSourceInfo plus the from_spec lowering).
//
// A deferred MCP tool is described by an [McpToolInfo], the Go mirror of the
// Rust codex_mcp::ToolInfo (reduced to the fields the spec + search text need).
// The McpToolInfo carries enough metadata to (1) lower the raw MCP tool into a
// namespace spec, (2) build the search text, and (3) report the source info.

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// McpToolInfo is the model-visible + routing metadata for a single MCP tool,
// mirroring the Rust codex_mcp::ToolInfo fields used by the deferred-tool spec
// and search machinery. The raw MCP tool definition is the lowered shape sent
// back to the server.
type McpToolInfo struct {
	// ServerName is the raw MCP server name used for routing the tool call.
	ServerName string
	// CallableName is the model-visible tool name (canonical_tool_name.name).
	CallableName string
	// CallableNamespace is the model-visible namespace (canonical_tool_name
	// namespace, e.g. "mcp__calendar__").
	CallableNamespace string
	// NamespaceDescription is the model-visible namespace description, if any.
	NamespaceDescription *string
	// ConnectorName is the friendly connector name, if any.
	ConnectorName *string
	// PluginDisplayNames are the owning plugins' display names, if any.
	PluginDisplayNames []string
	// Tool is the raw MCP tool definition (tool.name routes back to the server).
	Tool protocol.Tool
}

// CanonicalToolName returns the namespaced tool name. Mirrors Rust
// `ToolInfo::canonical_tool_name`.
func (i McpToolInfo) CanonicalToolName() protocol.ToolName {
	return protocol.NamespacedToolName(i.CallableNamespace, i.CallableName)
}

// McpToolSpec builds the namespace ToolSpec a (deferred) MCP tool advertises,
// mirroring the Rust `create_tool_spec`: the raw MCP tool is lowered to a
// function tool under the canonical name, and the namespace description prefers
// the explicit namespace_description, then a "Tools for working with {connector}."
// fallback, then empty.
func McpToolSpec(info McpToolInfo) (ToolSpec, error) {
	toolName := info.CanonicalToolName()
	tool, err := McpToolToResponsesApiTool(toolName, info.Tool)
	if err != nil {
		return ToolSpec{}, err
	}
	return NamespaceToolSpec(ResponsesApiNamespace{
		Name:        info.CallableNamespace,
		Description: mcpNamespaceDescription(info),
		Tools:       []ResponsesApiNamespaceTool{FunctionNamespaceTool(tool)},
	}), nil
}

// mcpNamespaceDescription resolves the namespace description, mirroring the
// description-resolution chain in Rust `create_tool_spec`.
func mcpNamespaceDescription(info McpToolInfo) string {
	if desc := trimmedNonEmpty(info.NamespaceDescription); desc != "" {
		return desc
	}
	if connector := trimmedNonEmpty(info.ConnectorName); connector != "" {
		return "Tools for working with " + connector + "."
	}
	return ""
}

// McpToolSearchInfo builds the ToolSearchInfo for a deferred MCP tool, mirroring
// the Rust `McpHandler::search_info`: the source name prefers the connector name
// then the server name, the source description is the trimmed namespace
// description, and the entry is the spec lowered through from_spec.
func McpToolSearchInfo(info McpToolInfo) (ToolSearchInfo, bool) {
	spec, err := McpToolSpec(info)
	if err != nil {
		return ToolSearchInfo{}, false
	}
	source := mcpSearchSource(info)
	return ToolSearchInfoFromSpec(BuildMcpSearchText(info), spec, source)
}

// mcpSearchSource builds the per-server ToolSearchSourceInfo for a deferred MCP
// tool, mirroring the source_info construction in `McpHandler::search_info`: the
// name prefers a non-empty trimmed connector name, falling back to the trimmed
// server name; an empty resulting name yields no source.
func mcpSearchSource(info McpToolInfo) *ToolSearchSourceInfo {
	name := trimmedNonEmpty(info.ConnectorName)
	if name == "" {
		name = strings.TrimSpace(info.ServerName)
	}
	if name == "" {
		return nil
	}
	var description *string
	if desc := trimmedNonEmpty(info.NamespaceDescription); desc != "" {
		d := desc
		description = &d
	}
	return &ToolSearchSourceInfo{Name: name, Description: description}
}

// BuildMcpSearchText builds the BM25 document for a deferred MCP tool, mirroring
// the Rust `build_mcp_search_text`: a space-joined sequence of the flat tool
// name, callable name, raw tool name, server name, then the optional trimmed
// title / description / connector name / namespace description, the trimmed
// non-empty plugin display names, and finally the sorted input-schema property
// names.
func BuildMcpSearchText(info McpToolInfo) string {
	parts := []string{
		info.CanonicalToolName().String(),
		info.CallableName,
		info.Tool.Name,
		info.ServerName,
	}
	if title := trimmedNonEmpty(info.Tool.Title); title != "" {
		parts = append(parts, title)
	}
	if description := trimmedNonEmpty(info.Tool.Description); description != "" {
		parts = append(parts, description)
	}
	if connector := trimmedNonEmpty(info.ConnectorName); connector != "" {
		parts = append(parts, connector)
	}
	if namespaceDescription := trimmedNonEmpty(info.NamespaceDescription); namespaceDescription != "" {
		parts = append(parts, namespaceDescription)
	}
	for _, displayName := range info.PluginDisplayNames {
		if trimmed := strings.TrimSpace(displayName); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	parts = append(parts, mcpSchemaPropertyNames(info.Tool.InputSchema)...)
	return strings.Join(parts, " ")
}

// mcpSchemaPropertyNames returns the sorted property names of an MCP tool input
// schema, mirroring the `schema_properties` collection in
// `build_mcp_search_text` (the keys of the schema's "properties" object, sorted).
func mcpSchemaPropertyNames(inputSchema json.RawMessage) []string {
	if len(inputSchema) == 0 {
		return nil
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(inputSchema, &schema); err != nil {
		return nil
	}
	if len(schema.Properties) == 0 {
		return nil
	}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// trimmedNonEmpty returns the trimmed value of an optional string, or "" when
// the pointer is nil or the trimmed value is empty. Mirrors the Rust
// `.as_deref().map(str::trim).filter(|s| !s.is_empty())` chain.
func trimmedNonEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
