package mcp

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// mcpToolNamePrefix is the historical namespace prefix applied to every MCP tool
// exposed to the model. It matches the reference LEGACY_MCP_TOOL_NAME_PREFIX.
const mcpToolNamePrefix = "mcp__"

// mcpToolNameDelimiter joins a namespace and a tool name in the flattened
// model-visible identifier. It matches the reference MCP_TOOL_NAME_DELIMITER.
const mcpToolNameDelimiter = "__"

// NamespaceForServer returns the model-visible namespace for an MCP server's
// tools, formatted as "mcp__<server>". This is the callable_namespace half of a
// fully-qualified MCP tool name.
func NamespaceForServer(serverName string) string {
	return mcpToolNamePrefix + serverName
}

// FullyQualifiedToolName builds the flattened model-visible name for a tool on a
// server, formatted as "mcp__<server>__<tool>". It mirrors the reference
// convention used across codex-core for MCP tool exposure.
func FullyQualifiedToolName(serverName, toolName string) string {
	return NamespaceForServer(serverName) + mcpToolNameDelimiter + toolName
}

// QualifiedToolName returns the structured [protocol.ToolName] for an MCP tool,
// with namespace "mcp__<server>" and the raw tool name. The namespace carries a
// trailing delimiter so ToolName.String reconstructs "mcp__<server>__<tool>".
func QualifiedToolName(serverName, toolName string) protocol.ToolName {
	return protocol.NamespacedToolName(NamespaceForServer(serverName)+mcpToolNameDelimiter, toolName)
}

// ParseFullyQualifiedToolName splits a flattened "mcp__<server>__<tool>" name
// back into its server and tool components. It reports an error when the input
// does not carry the MCP prefix or omits the tool segment.
func ParseFullyQualifiedToolName(qualified string) (server, tool string, err error) {
	rest, ok := strings.CutPrefix(qualified, mcpToolNamePrefix)
	if !ok {
		return "", "", fmt.Errorf("mcp: %q is not an MCP tool name (missing %q prefix)", qualified, mcpToolNamePrefix)
	}
	server, tool, ok = strings.Cut(rest, mcpToolNameDelimiter)
	if !ok || server == "" || tool == "" {
		return "", "", fmt.Errorf("mcp: %q is not a fully-qualified MCP tool name", qualified)
	}
	return server, tool, nil
}

// NamespacedTool pairs a server name with one of its tools, used when exposing
// MCP tools to the model and when routing tool calls back to the owning server.
type NamespacedTool struct {
	// ServerName is the raw MCP server name used for routing.
	ServerName string
	// Tool is the raw MCP tool definition; Tool.Name is sent to the server.
	Tool protocol.Tool
}

// QualifiedName returns the model-visible "mcp__<server>__<tool>" name.
func (n NamespacedTool) QualifiedName() string {
	return FullyQualifiedToolName(n.ServerName, n.Tool.Name)
}

// ToolInfo lowers a namespaced tool into the [tools.McpToolInfo] the runtime
// executors consume. It carries the server identity (lost by a bare ToolSpec)
// so the eager executor can route the call back to the owning server via the
// canonical "mcp__<server>__<tool>" name while still advertising the raw tool
// name to the model. CallableNamespace carries the trailing delimiter so
// CanonicalToolName().String() reconstructs the fully-qualified name.
func (n NamespacedTool) ToolInfo() tools.McpToolInfo {
	return tools.McpToolInfo{
		ServerName:        n.ServerName,
		CallableName:      n.Tool.Name,
		CallableNamespace: NamespaceForServer(n.ServerName) + mcpToolNameDelimiter,
		Tool:              n.Tool,
	}
}

// ToolSpec lowers a namespaced MCP tool into a Responses API function tool spec
// named with its fully-qualified identifier. deferred controls whether the tool
// is exposed for deferred loading.
func (n NamespacedTool) ToolSpec(deferred bool) (tools.ToolSpec, error) {
	name := QualifiedToolName(n.ServerName, n.Tool.Name)
	var (
		apiTool tools.ResponsesApiTool
		err     error
	)
	if deferred {
		apiTool, err = tools.McpToolToDeferredResponsesApiTool(name, n.Tool)
	} else {
		apiTool, err = tools.McpToolToResponsesApiTool(name, n.Tool)
	}
	if err != nil {
		return tools.ToolSpec{}, fmt.Errorf("mcp: lower tool %q: %w", n.QualifiedName(), err)
	}
	return tools.FunctionToolSpec(apiTool), nil
}

// ToolSpecs lowers a set of namespaced tools into Responses API tool specs.
// Tools whose fully-qualified names collide are skipped after the first
// occurrence, matching the reference dedup-by-name behavior at the exposure
// boundary.
func ToolSpecs(namespaced []NamespacedTool, deferred bool) ([]tools.ToolSpec, error) {
	seen := make(map[string]struct{}, len(namespaced))
	specs := make([]tools.ToolSpec, 0, len(namespaced))
	for _, nt := range namespaced {
		name := nt.QualifiedName()
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		spec, err := nt.ToolSpec(deferred)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}
