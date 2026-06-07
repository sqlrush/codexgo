package cli

import (
	"fmt"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// loadedConfig is the subset of resolved configuration the CLI subcommands need:
// the codex home directory, the auth credentials store mode, and the ChatGPT
// base URL. It is derived from a full config.Load so -c overrides and the
// selected profile are honored.
type loadedConfig struct {
	CodexHome      string
	StoreMode      config.AuthCredentialsStoreMode
	ChatgptBaseURL *string
	// Tui is the resolved [tui] config block, or nil when unset. The interactive
	// TUI launcher uses it to load the configured theme.
	Tui *config.Tui
	// ModelProviderID is the resolved `model_provider` selection (the active
	// provider id), or nil when unset. The assembly resolves this against the
	// merged provider catalog to pick the api.Provider. It mirrors the Rust
	// model_provider_id resolution in core/src/config/mod.rs.
	ModelProviderID *string
	// ModelProviders is the configured `[model_providers]` map, keyed by id. It is
	// merged onto the built-in catalog so custom providers extend the defaults.
	ModelProviders map[string]modelproviderinfo.ModelProviderInfo
	// DefaultModel is the resolved `model` slug, or nil when unset. It is the
	// preferred default model for the assembly over CODEXGO_MODEL and the mock slug.
	DefaultModel *string
	// OpenAIBaseURL overrides the built-in OpenAI provider's base URL when set and
	// non-empty, matching the Rust `openai_base_url` handling.
	OpenAIBaseURL *string
	// SandboxMode is the resolved `sandbox_mode`, or nil when unset (codex defaults
	// to read-only). Used to render the environment-context message.
	SandboxMode *protocol.SandboxMode
	// Merged is the merged TOML config tree across all layers. The skills host
	// reads its `[projects]` trust table to gate project-layer skill loading on
	// git-trust (mirroring the Rust loader's ProjectTrustContext).
	Merged config.TomlValue
	// ProjectRootMarkers is the resolved project_root_markers list (nil ==
	// default [".git"]). Used to locate the project root for trust resolution.
	ProjectRootMarkers []string
	// Marketplaces is the resolved `[marketplaces]` table, keyed by name. The
	// plugin marketplace-upgrade command reads it to refresh git marketplaces.
	Marketplaces map[string]config.MarketplaceConfig
	// McpServers is the resolved `[mcp_servers]` table, keyed by server name. The
	// assembly launches these via the MCP manager so their tools reach the model.
	McpServers map[string]config.McpServerConfig
	// Plugins is the resolved `[plugins]` table, keyed by "<plugin>@<marketplace>".
	// The assembly discovers each enabled plugin's MCP servers (from its .mcp.json)
	// and merges them with McpServers.
	Plugins map[string]config.PluginConfig
}

// loadConfig loads the merged configuration honoring the root -c overrides,
// -p/--profile, and --strict-config flags, then projects out the fields the CLI
// subcommands consume. It mirrors load_config_or_exit in login.rs combined with
// the profile/strict handling in main.rs.
func loadConfig(root RootOptions) (loadedConfig, error) {
	overrides, err := root.Overrides.Parse()
	if err != nil {
		return loadedConfig{}, fmt.Errorf("parsing -c overrides: %w", err)
	}

	result, err := config.Load(config.LoadOptions{
		Profile:      root.Profile,
		CliOverrides: overrides,
		StrictConfig: root.StrictConfig,
	})
	if err != nil {
		return loadedConfig{}, fmt.Errorf("loading configuration: %w", err)
	}

	markers, err := config.ProjectRootMarkersFromConfig(result.Merged)
	if err != nil {
		return loadedConfig{}, fmt.Errorf("resolving project_root_markers: %w", err)
	}

	return loadedConfig{
		CodexHome:          result.CodexHome,
		StoreMode:          resolveStoreMode(result.Config.CliAuthCredentialsStore),
		ChatgptBaseURL:     result.Config.ChatgptBaseURL,
		Tui:                result.Config.Tui,
		ModelProviderID:    result.Config.ModelProvider,
		ModelProviders:     result.Config.ModelProviders,
		DefaultModel:       result.Config.Model,
		OpenAIBaseURL:      result.Config.OpenAIBaseURL,
		SandboxMode:        result.Config.SandboxMode,
		Merged:             result.Merged,
		ProjectRootMarkers: markers,
		Marketplaces:       result.Config.Marketplaces,
		McpServers:         result.Config.McpServers,
		Plugins:            result.Config.Plugins,
	}, nil
}

// resolveStoreMode resolves the effective auth credentials store mode, defaulting
// to File when unset (matching the Rust serde default for
// cli_auth_credentials_store).
func resolveStoreMode(configured *config.AuthCredentialsStoreMode) config.AuthCredentialsStoreMode {
	if configured == nil || *configured == "" {
		return config.AuthCredentialsStoreFile
	}
	return *configured
}
