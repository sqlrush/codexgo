package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// Default timeouts for MCP startup and tool calls, matching the reference
// DEFAULT_STARTUP_TIMEOUT and DEFAULT_TOOL_TIMEOUT constants.
const (
	DefaultStartupTimeout = 30 * time.Second
	DefaultToolTimeout    = 120 * time.Second
)

// ClientName and ClientVersion identify Codex during the MCP handshake. They
// match the reference Implementation::new("codex-mcp-client", CARGO_PKG_VERSION)
// with title "Codex".
const (
	ClientName    = "codex-mcp-client"
	ClientVersion = "0.136.0"
	ClientTitle   = "Codex"
)

// ManagedClient is a fully initialized connection to one MCP server: the live
// JSON-RPC client, the server's advertised info, its discovered (filtered) tools,
// and the per-server tool-call timeout. ManagedClient values are immutable after
// construction.
type ManagedClient struct {
	// ServerName is the raw MCP server name from config.
	ServerName string
	// Client is the live JSON-RPC client for this server.
	Client *Client
	// ServerInfo is the implementation metadata advertised at initialize.
	ServerInfo Implementation
	// Instructions is the optional server instructions from initialize.
	Instructions *string
	// Tools is the filtered set of tools discovered at startup.
	Tools []protocol.Tool
	// ToolTimeout bounds individual tool calls to this server.
	ToolTimeout time.Duration
	// filter restricts which tools may be called and exposed.
	filter ToolFilter
}

// ToolFilter restricts which tools a server exposes. A tool is allowed when the
// enabled allowlist is absent or contains it, and it is not in the disabled set.
// It is a faithful port of the reference ToolFilter.
type ToolFilter struct {
	// Enabled is the optional allowlist; nil means "all tools allowed".
	Enabled map[string]struct{}
	// Disabled is the set of explicitly disabled tools.
	Disabled map[string]struct{}
}

// ToolFilterFromConfig builds a [ToolFilter] from a server config's enabled and
// disabled tool lists.
func ToolFilterFromConfig(cfg config.McpServerConfig) ToolFilter {
	var enabled map[string]struct{}
	if cfg.EnabledTools != nil {
		enabled = make(map[string]struct{}, len(*cfg.EnabledTools))
		for _, name := range *cfg.EnabledTools {
			enabled[name] = struct{}{}
		}
	}
	disabled := map[string]struct{}{}
	if cfg.DisabledTools != nil {
		for _, name := range *cfg.DisabledTools {
			disabled[name] = struct{}{}
		}
	}
	return ToolFilter{Enabled: enabled, Disabled: disabled}
}

// Allows reports whether a raw tool name passes the filter.
func (f ToolFilter) Allows(toolName string) bool {
	if f.Enabled != nil {
		if _, ok := f.Enabled[toolName]; !ok {
			return false
		}
	}
	_, disabled := f.Disabled[toolName]
	return !disabled
}

// filterTools returns the subset of tools the filter allows, preserving order.
func filterTools(in []protocol.Tool, filter ToolFilter) []protocol.Tool {
	out := make([]protocol.Tool, 0, len(in))
	for _, tool := range in {
		if filter.Allows(tool.Name) {
			out = append(out, tool)
		}
	}
	return out
}

// startManagedClient performs the full per-server startup: it runs the
// initialize handshake (bounded by startupTimeout), discovers tools, applies the
// tool filter, and returns the ready [ManagedClient]. The client is closed on
// failure so no resources leak.
func startManagedClient(
	ctx context.Context,
	serverName string,
	client *Client,
	filter ToolFilter,
	startupTimeout, toolTimeout time.Duration,
) (*ManagedClient, error) {
	params := InitializeParams{
		ProtocolVersion: MCPProtocolVersion,
		Capabilities: ClientCapabilities{
			Elicitation: &struct{}{},
		},
		ClientInfo: Implementation{
			Name:    ClientName,
			Version: ClientVersion,
			Title:   strPtr(ClientTitle),
		},
	}

	initResult, err := client.Initialize(ctx, params, startupTimeout)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("mcp: initialize server %q: %w", serverName, err)
	}

	rawTools, err := client.ListAllTools(ctx, startupTimeout)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("mcp: list tools for server %q: %w", serverName, err)
	}

	return &ManagedClient{
		ServerName:   serverName,
		Client:       client,
		ServerInfo:   initResult.ServerInfo,
		Instructions: initResult.Instructions,
		Tools:        filterTools(rawTools, filter),
		ToolTimeout:  toolTimeout,
		filter:       filter,
	}, nil
}

// CallTool invokes a raw tool on this server, rejecting tools the filter
// disallows. arguments and meta must each be a JSON object or nil.
func (m *ManagedClient) CallTool(ctx context.Context, toolName string, arguments, meta []byte) (protocol.CallToolResult, error) {
	if !m.filter.Allows(toolName) {
		return protocol.CallToolResult{}, fmt.Errorf("mcp: tool %q is disabled for MCP server %q", toolName, m.ServerName)
	}
	return m.Client.CallTool(ctx, toolName, arguments, meta, m.ToolTimeout)
}

// NamespacedTools returns this server's tools wrapped for model exposure and
// routing.
func (m *ManagedClient) NamespacedTools() []NamespacedTool {
	out := make([]NamespacedTool, 0, len(m.Tools))
	for _, tool := range m.Tools {
		out = append(out, NamespacedTool{ServerName: m.ServerName, Tool: tool})
	}
	return out
}

// Close shuts down the underlying client and its transport.
func (m *ManagedClient) Close() error {
	return m.Client.Close()
}

// strPtr returns a pointer to s. It keeps Implementation construction concise.
func strPtr(s string) *string {
	return &s
}
