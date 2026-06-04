package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
)

// defaultMockReply is the canned assistant reply used when CODEX_EXEC_MOCK_REPLY
// is unset, matching the existing codex-exec binary so the engine produces a
// complete turn out of the box without credentials.
const defaultMockReply = "Hello from codex (mock model). Set a real model client to run live."

// buildAssemblyWithDefaults constructs the Codex engine, selecting a real
// provider-backed model client when a usable provider + credentials resolve and a
// scripted mock otherwise, and returns the resolved per-session defaults
// (provider id + default model) alongside it.
//
// It loads the merged configuration, resolves the active `model_provider`
// selection against the merged built-in + configured provider catalog, builds the
// matching credential resolver (the login-backed OpenAI auth path for
// requires_openai_auth providers; a provider env_key bearer otherwise), and wires
// appserver.NewModelClientFactory with the scripted mock as the offline/dev
// fallback. This mirrors the Rust ModelClient::new + create_model_provider path
// plus the exec provider-selection fallback. Any configuration / provider error
// degrades to the pure-mock assembly so the exec / mcp-server / app-server paths
// still run out of the box.
//
// Threading the resolved provider id and model through the returned defaults is
// what lets the binary honor a config.toml `model` / `model_provider` selection
// end to end, rather than always defaulting to the mock slug and the "openai"
// provider id.
func buildAssemblyWithDefaults() (*appserver.Assembly, appserver.Defaults, error) {
	fallback := mockClientFactory()

	cfg, ok := loadProviderConfig()
	if !ok {
		// Configuration could not be loaded; run with the mock so the engine still
		// works offline (e.g. in tests or a fresh checkout without a config file).
		return assembleResult(fallback, "", "", defaultModelProviderID, "")
	}

	model := configDefaultModel(cfg)

	selected, err := resolveSelectedProvider(cfg)
	if err != nil {
		// A bad provider selection must not break the offline paths; fall back to
		// the mock so the engine still runs.
		return assembleResult(fallback, cfg.CodexHome, model, defaultModelProviderID, derefSandboxMode(cfg.SandboxMode))
	}

	resolver, ok := buildProviderAuthResolver(cfg, selected)
	if !ok {
		// No credential source applies to the selected provider; use the mock.
		return assembleResult(fallback, cfg.CodexHome, model, selected.ID, derefSandboxMode(cfg.SandboxMode))
	}

	factory, err := appserver.NewModelClientFactory(appserver.RealModelClientFactoryConfig{
		AuthResolver:   resolver,
		Provider:       selected.Info,
		InstallationID: resolveInstallationID(),
		Fallback:       fallback,
		// Resolve full per-model metadata (verbosity support + default, reasoning
		// support/level, context window, service tier) from the bundled catalog so
		// the request matches the reference binary. Without this, every model
		// resolves to minimal slug-derived defaults and request fields such as
		// text.verbosity diverge from codex.
		ModelCatalog: bundledModelCatalog(),
	})
	if err != nil {
		return nil, appserver.Defaults{}, fmt.Errorf("cli: build model client factory: %w", err)
	}
	return assembleResult(factory, cfg.CodexHome, model, selected.ID, derefSandboxMode(cfg.SandboxMode))
}

// derefSandboxMode returns the configured sandbox mode, or the empty value (which
// assembleResult resolves to read-only) when unset.
func derefSandboxMode(mode *protocol.SandboxMode) protocol.SandboxMode {
	if mode == nil {
		return ""
	}
	return *mode
}

// buildProviderAuthResolver selects the credential resolver for the active
// provider. requires_openai_auth providers (e.g. the built-in OpenAI provider)
// use the login-backed resolver, which honors OPENAI_API_KEY / CODEX_API_KEY /
// auth.json / ChatGPT login. Other providers authenticate from their declared
// env_key. It returns ok == false when no credential source applies, so the
// caller selects the mock fallback.
func buildProviderAuthResolver(cfg loadedConfig, selected selectedProvider) (appserver.AuthResolver, bool) {
	if isOpenAIAuthProvider(selected.Info) {
		return newLoginAuthResolver(authResolverConfig{
			CodexHome:            cfg.CodexHome,
			StoreMode:            cfg.StoreMode,
			ChatgptBaseURL:       cfg.ChatgptBaseURL,
			EnableCodexAPIKeyEnv: true,
		}), true
	}
	if envKeyDefined(selected.Info) {
		return &envKeyAuthResolver{info: selected.Info}, true
	}
	return nil, false
}

// assembleResult builds the engine with the given model-client factory, codex
// home, default model slug, and provider id, returning the assembly alongside the
// resolved per-session Defaults. An empty codexHome is resolved from the
// environment; an empty defaultModel falls back to CODEX_MODEL and then the mock
// slug. The same resolved model + provider id flow into both the assembly's
// models manager and the returned Defaults so the binary's exec/review/TUI paths
// honor the configured selection.
func assembleResult(factory appserver.ModelClientFactory, codexHome, defaultModel, providerID string, sandboxMode protocol.SandboxMode) (*appserver.Assembly, appserver.Defaults, error) {
	if codexHome == "" {
		codexHome = resolveCodexHome()
	}
	if sandboxMode == "" {
		sandboxMode = protocol.SandboxModeReadOnly
	}
	model := resolveDefaultModel(defaultModel)
	asm, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: factory,
		CodexHome:          codexHome,
		DefaultModel:       model,
		// The bundled catalog lets the per-turn tool selection read the real
		// model metadata (shell_type, truncation policy) — without it every turn
		// falls back to slug-derived defaults.
		ModelCatalog: bundledModelCatalog(),
		// Wire the real built-in tool router so the binary actually executes tools
		// (exec_command / shell_command, apply_patch, view_image, update_plan).
		// Without this the assembly defaults to an empty router and every tool call
		// is rejected with "unsupported call". The unified-exec executor backs the
		// exec_command/write_stdin PTY pair that codex advertises by default.
		ToolRouterFactory: func() (core.ToolRouter, error) {
			return core.BuiltinToolRouter(core.BuiltinToolDeps{
				Exec:        newLocalExecService(),
				UnifiedExec: unifiedexec.NewExecutor(nil),
				// request_user_input is advertised by default (codex's
				// experimental_request_user_input_enabled). Headless exec has no
				// interactive client, so calls resolve as cancelled; the TUI /
				// app-server clients supply a real requester when they land.
				UserInput: headlessUserInputRequester{},
			})
		},
	})
	if err != nil {
		return nil, appserver.Defaults{}, err
	}
	defaults := appserver.Defaults{
		Model:      model,
		ProviderID: providerID,
		Cwd:        resolveCwd(),
		UserAgent:  "codex-cli-go",
		// Seed codex's initial context (permissions + environment_context) into new
		// threads, like the reference binary. Network defaults to restricted, which
		// matches the read-only/workspace-write defaults.
		IncludeEnvironmentContext: true,
		SandboxMode:               sandboxMode,
		NetworkAccessEnabled:      false,
	}
	return asm, defaults, nil
}

// bundledModelCatalog returns the model catalog shipped with the binary, used to
// resolve full per-model metadata for the model client. It returns nil when the
// bundle cannot be decoded (the factory then falls back to slug-derived defaults),
// so a corrupt bundle degrades gracefully rather than breaking the assembly.
func bundledModelCatalog() []modelsmanager.ModelInfo {
	resp, err := modelsmanager.BundledModelsResponse()
	if err != nil {
		return nil
	}
	return resp.Models
}

// mockClientFactory builds the scripted-mock ModelClientFactory used as the
// offline/dev fallback. The reply is overridable via CODEX_EXEC_MOCK_REPLY.
func mockClientFactory() appserver.ModelClientFactory {
	reply := os.Getenv("CODEX_EXEC_MOCK_REPLY")
	if reply == "" {
		reply = defaultMockReply
	}
	return func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
		slug := cfg.Model()
		if slug == "" {
			slug = defaultMockModelSlug
		}
		return core.NewMockModelClient(slug, nil, mockTurn(reply)), nil
	}
}

// loadProviderConfig loads and projects the merged CLI configuration used by the
// assembly: the codex home, auth store mode, the active provider selection, the
// configured providers map, and the default model. It returns ok == false when
// configuration cannot be loaded, in which case the caller falls back to the
// pure-mock assembly. The default root options are used because the assembly
// builder has no parsed -c overrides in scope; the richer wiring through
// ParsedCommandLine can replace this when the exec/app-server dispatch threads the
// root options here.
func loadProviderConfig() (loadedConfig, bool) {
	cfg, err := loadConfig(RootOptions{})
	if err != nil {
		return loadedConfig{}, false
	}
	return cfg, true
}

// configDefaultModel returns the configured `model` slug, or "" when unset. The
// assembly's resolveDefaultModel then layers CODEX_MODEL and the mock slug below
// it, so a config model wins over the env var, matching the Rust precedence
// (config model resolved before the env-derived defaults).
func configDefaultModel(cfg loadedConfig) string {
	if cfg.DefaultModel != nil && *cfg.DefaultModel != "" {
		return *cfg.DefaultModel
	}
	return ""
}

// resolveDefaultModel returns the effective default model slug for the assembly.
// The configured model wins; otherwise CODEX_MODEL is honored, and finally the
// mock slug keeps the offline path working out of the box.
func resolveDefaultModel(configModel string) string {
	if configModel != "" {
		return configModel
	}
	if slug := os.Getenv("CODEX_MODEL"); slug != "" {
		return slug
	}
	return defaultMockModelSlug
}

// resolveInstallationID returns the installation identifier sent in routing
// headers, honoring CODEX_INSTALLATION_ID when set.
func resolveInstallationID() string {
	return os.Getenv("CODEX_INSTALLATION_ID")
}

// defaultMockModelSlug is the slug used by the offline mock client.
const defaultMockModelSlug = "gpt-mock"

// mockTurn builds a scripted assistant turn that emits a single message and ends,
// matching the codex-exec binary's mockTurn.
func mockTurn(text string) core.MockTurn {
	mid := "m1"
	end := true
	return core.MockTurn{Events: []api.ResponseEvent{
		{Kind: api.ResponseEventCreated},
		{
			Kind: api.ResponseEventOutputItemDone,
			Item: &protocol.ResponseItem{
				Type:      protocol.ResponseItemKindMessage,
				Role:      "assistant",
				MessageID: &mid,
				Content:   []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
			},
		},
		{Kind: api.ResponseEventCompleted, EndTurn: &end},
	}}
}

// resolveCodexHome resolves the Codex configuration directory, falling back to
// ".codex" when neither CODEX_HOME nor the home directory is available.
func resolveCodexHome() string {
	if home, err := config.FindCodexHome(); err == nil {
		return home
	}
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	return ".codex"
}

// resolveCwd returns the current working directory, defaulting to "." when it
// cannot be determined.
func resolveCwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
