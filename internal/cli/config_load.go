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
	// preferred default model for the assembly over CODEX_MODEL and the mock slug.
	DefaultModel *string
	// OpenAIBaseURL overrides the built-in OpenAI provider's base URL when set and
	// non-empty, matching the Rust `openai_base_url` handling.
	OpenAIBaseURL *string
	// SandboxMode is the resolved `sandbox_mode`, or nil when unset (codex defaults
	// to read-only). Used to render the environment-context message.
	SandboxMode *protocol.SandboxMode
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

	return loadedConfig{
		CodexHome:       result.CodexHome,
		StoreMode:       resolveStoreMode(result.Config.CliAuthCredentialsStore),
		ChatgptBaseURL:  result.Config.ChatgptBaseURL,
		Tui:             result.Config.Tui,
		ModelProviderID: result.Config.ModelProvider,
		ModelProviders:  result.Config.ModelProviders,
		DefaultModel:    result.Config.Model,
		OpenAIBaseURL:   result.Config.OpenAIBaseURL,
		SandboxMode:     result.Config.SandboxMode,
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
