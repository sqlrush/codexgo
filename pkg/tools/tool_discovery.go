package tools

import "github.com/sqlrush/codexgo/internal/appserverproto"

const (
	// tuiClientName is the app-server client name used by the TUI.
	tuiClientName = "codex-tui"

	// ToolSearchToolName is the name of the tool_search tool.
	ToolSearchToolName = "tool_search"
	// ToolSearchDefaultLimit is the default number of results tool_search returns.
	ToolSearchDefaultLimit = 8
	// ListAvailablePluginsToInstallToolName is the discovery list tool name.
	ListAvailablePluginsToInstallToolName = "list_available_plugins_to_install"
	// RequestPluginInstallToolName is the plugin install request tool name.
	RequestPluginInstallToolName = "request_plugin_install"
)

// ToolSearchSourceInfo describes the source backing a tool_search result.
// Mirrors Rust `ToolSearchSourceInfo`.
type ToolSearchSourceInfo struct {
	Name        string
	Description *string
}

// DiscoverableToolType is the kind of a discoverable tool. Mirrors Rust
// `DiscoverableToolType` (`#[serde(rename_all = "snake_case")]`).
type DiscoverableToolType string

// DiscoverableToolType variants.
const (
	DiscoverableToolTypeConnector DiscoverableToolType = "connector"
	DiscoverableToolTypePlugin    DiscoverableToolType = "plugin"
)

// DiscoverableToolAction is the action offered for a discoverable tool. Mirrors
// Rust `DiscoverableToolAction` (`#[serde(rename_all = "snake_case")]`).
type DiscoverableToolAction string

// DiscoverableToolAction variants.
const (
	DiscoverableToolActionInstall DiscoverableToolAction = "install"
	DiscoverableToolActionEnable  DiscoverableToolAction = "enable"
)

// DiscoverablePluginInfo describes a plugin that can be installed. Mirrors Rust
// `DiscoverablePluginInfo`.
type DiscoverablePluginInfo struct {
	ID              string
	Name            string
	Description     *string
	HasSkills       bool
	MCPServerNames  []string
	AppConnectorIDs []string
}

// DiscoverableToolKind discriminates a DiscoverableTool. Mirrors the Rust enum
// variants.
type DiscoverableToolKind int

// DiscoverableToolKind variants.
const (
	// DiscoverableToolKindConnector wraps an app connector.
	DiscoverableToolKindConnector DiscoverableToolKind = iota
	// DiscoverableToolKindPlugin wraps a plugin.
	DiscoverableToolKindPlugin
)

// DiscoverableTool is a tool the model can ask to install or enable. Mirrors
// Rust `DiscoverableTool`.
type DiscoverableTool struct {
	Kind DiscoverableToolKind
	// Connector is set for the Connector variant.
	Connector *appserverproto.AppInfo
	// Plugin is set for the Plugin variant.
	Plugin *DiscoverablePluginInfo
}

// ConnectorDiscoverableTool constructs a Connector DiscoverableTool.
func ConnectorDiscoverableTool(info appserverproto.AppInfo) DiscoverableTool {
	return DiscoverableTool{Kind: DiscoverableToolKindConnector, Connector: &info}
}

// PluginDiscoverableTool constructs a Plugin DiscoverableTool.
func PluginDiscoverableTool(info DiscoverablePluginInfo) DiscoverableTool {
	return DiscoverableTool{Kind: DiscoverableToolKindPlugin, Plugin: &info}
}

// ToolType reports the discoverable tool type. Mirrors Rust
// `DiscoverableTool::tool_type`.
func (t DiscoverableTool) ToolType() DiscoverableToolType {
	if t.Kind == DiscoverableToolKindConnector {
		return DiscoverableToolTypeConnector
	}
	return DiscoverableToolTypePlugin
}

// ID returns the discoverable tool id. Mirrors Rust `DiscoverableTool::id`.
func (t DiscoverableTool) ID() string {
	if t.Kind == DiscoverableToolKindConnector {
		return t.Connector.ID
	}
	return t.Plugin.ID
}

// Name returns the discoverable tool name. Mirrors Rust `DiscoverableTool::name`.
func (t DiscoverableTool) Name() string {
	if t.Kind == DiscoverableToolKindConnector {
		return t.Connector.Name
	}
	return t.Plugin.Name
}

// InstallURL returns the connector install URL when present. Plugins have none.
// Mirrors Rust `DiscoverableTool::install_url`.
func (t DiscoverableTool) InstallURL() *string {
	if t.Kind == DiscoverableToolKindConnector {
		return t.Connector.InstallURL
	}
	return nil
}

// FilterRequestPluginInstallDiscoverableToolsForClient drops plugin entries for
// the TUI client, leaving connectors. Other clients receive the list unchanged.
// Mirrors Rust `filter_request_plugin_install_discoverable_tools_for_client`.
func FilterRequestPluginInstallDiscoverableToolsForClient(
	discoverableTools []DiscoverableTool,
	appServerClientName *string,
) []DiscoverableTool {
	if appServerClientName == nil || *appServerClientName != tuiClientName {
		return discoverableTools
	}
	filtered := make([]DiscoverableTool, 0, len(discoverableTools))
	for _, tool := range discoverableTools {
		if tool.Kind == DiscoverableToolKindPlugin {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

// RequestPluginInstallEntry is the serialized descriptor of a discoverable tool.
// Mirrors Rust `RequestPluginInstallEntry` (serde default = snake_case fields).
type RequestPluginInstallEntry struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Description     *string              `json:"description"`
	ToolType        DiscoverableToolType `json:"tool_type"`
	HasSkills       bool                 `json:"has_skills"`
	MCPServerNames  []string             `json:"mcp_server_names"`
	AppConnectorIDs []string             `json:"app_connector_ids"`
}

// ListAvailablePluginsToInstallResult is the result of the discovery list tool.
// Mirrors Rust `ListAvailablePluginsToInstallResult`.
type ListAvailablePluginsToInstallResult struct {
	Tools []RequestPluginInstallEntry `json:"tools"`
}

// CollectRequestPluginInstallEntries projects discoverable tools into serialized
// entries. Mirrors Rust `collect_request_plugin_install_entries`: connectors
// carry no skills/server/connector ids, while plugins carry their own.
func CollectRequestPluginInstallEntries(discoverableTools []DiscoverableTool) []RequestPluginInstallEntry {
	entries := make([]RequestPluginInstallEntry, 0, len(discoverableTools))
	for _, tool := range discoverableTools {
		switch tool.Kind {
		case DiscoverableToolKindConnector:
			entries = append(entries, RequestPluginInstallEntry{
				ID:              tool.Connector.ID,
				Name:            tool.Connector.Name,
				Description:     tool.Connector.Description,
				ToolType:        DiscoverableToolTypeConnector,
				HasSkills:       false,
				MCPServerNames:  []string{},
				AppConnectorIDs: []string{},
			})
		case DiscoverableToolKindPlugin:
			entries = append(entries, RequestPluginInstallEntry{
				ID:              tool.Plugin.ID,
				Name:            tool.Plugin.Name,
				Description:     tool.Plugin.Description,
				ToolType:        DiscoverableToolTypePlugin,
				HasSkills:       tool.Plugin.HasSkills,
				MCPServerNames:  tool.Plugin.MCPServerNames,
				AppConnectorIDs: tool.Plugin.AppConnectorIDs,
			})
		}
	}
	return entries
}
