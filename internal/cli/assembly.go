package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// defaultMockReply is the canned assistant reply used when CODEX_EXEC_MOCK_REPLY
// is unset, matching the existing codex-exec binary so the engine produces a
// complete turn out of the box without credentials.
const defaultMockReply = "Hello from codex (mock model). Set a real model client to run live."

// buildAssembly constructs the Codex engine, selecting a real provider-backed
// model client when credentials are present and a scripted mock otherwise.
//
// It resolves the Codex home + store mode from the loaded configuration, builds a
// login-backed auth resolver, and wires appserver.NewModelClientFactory with the
// mock as the offline/dev fallback. This mirrors the Rust ModelClient::new path
// plus the exec provider-selection fallback. Configuration errors degrade to the
// pure-mock assembly so the exec / mcp-server / app-server paths still run.
func buildAssembly() (*appserver.Assembly, error) {
	fallback := mockClientFactory()

	resolverCfg, ok := loadAuthResolverConfig()
	if !ok {
		// Configuration could not be loaded; run with the mock so the engine still
		// works offline (e.g. in tests or a fresh checkout without a config file).
		return assembleWithFactory(fallback, resolverCfg.CodexHome)
	}

	resolver := newLoginAuthResolver(resolverCfg)
	// The OpenAI provider is built with a nil base URL so ToAPIProvider selects the
	// correct default per resolved auth mode (the ChatGPT codex endpoint for
	// ChatGPT-style auth, the public OpenAI API for API-key auth). The configured
	// ChatGPT base URL only influences agent-identity JWKS verification, handled by
	// the resolver, not the Responses endpoint default.
	factory, err := appserver.NewModelClientFactory(appserver.RealModelClientFactoryConfig{
		AuthResolver:   resolver,
		Provider:       modelproviderinfo.CreateOpenAIProvider(nil),
		InstallationID: resolveInstallationID(),
		Fallback:       fallback,
	})
	if err != nil {
		return nil, fmt.Errorf("cli: build model client factory: %w", err)
	}
	return assembleWithFactory(factory, resolverCfg.CodexHome)
}

// assembleWithFactory builds the engine with the given model-client factory and
// codex home, applying the shared defaults.
func assembleWithFactory(factory appserver.ModelClientFactory, codexHome string) (*appserver.Assembly, error) {
	if codexHome == "" {
		codexHome = resolveCodexHome()
	}
	return appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: factory,
		CodexHome:          codexHome,
		DefaultModel:       defaultModelSlug(),
	})
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

// loadAuthResolverConfig projects the loaded CLI configuration into the auth
// resolver config. It returns ok == false when configuration cannot be loaded,
// in which case the caller falls back to the pure-mock assembly. The default root
// options are used because buildAssembly has no parsed -c overrides in scope; the
// richer wiring through ParsedCommandLine can replace this when the exec/app-server
// dispatch threads the root options here.
func loadAuthResolverConfig() (authResolverConfig, bool) {
	cfg, err := loadConfig(RootOptions{})
	if err != nil {
		return authResolverConfig{}, false
	}
	return authResolverConfig{
		CodexHome:            cfg.CodexHome,
		StoreMode:            cfg.StoreMode,
		ChatgptBaseURL:       cfg.ChatgptBaseURL,
		EnableCodexAPIKeyEnv: true,
	}, true
}

// defaultModelSlug returns the configured default model slug for the assembly. It
// honors CODEX_MODEL when set and otherwise falls back to the mock slug so the
// offline path keeps working out of the box.
func defaultModelSlug() string {
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
