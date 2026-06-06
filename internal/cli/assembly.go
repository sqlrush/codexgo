package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/internal/agentgraph"
	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/ext/goal"
	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/multiagent"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/state"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// defaultMockReply is the canned assistant reply used when CODEXGO_EXEC_MOCK_REPLY
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
		return assembleResult(fallback, "", "", defaultModelProviderID, "", nil)
	}

	model := configDefaultModel(cfg)
	trustGate := buildProjectTrustGate(cfg)

	// codexgo model→provider routing: configured providers that declare a
	// `models` list serve those slugs regardless of the `model_provider`
	// selection, so switching the model alone switches the backend.
	routes := buildModelProviderRoutes(cfg, fallback)

	selected, err := resolveSelectedProvider(cfg)
	if err != nil {
		// A bad provider selection must not break the offline paths; fall back to
		// the mock so the engine still runs.
		routed := appserver.NewModelRoutedClientFactory(routes, fallback)
		return assembleResult(routed, cfg.CodexHome, model, defaultModelProviderID, derefSandboxMode(cfg.SandboxMode), trustGate)
	}

	resolver, ok := buildProviderAuthResolver(cfg, selected)
	if !ok {
		// No credential source applies to the selected provider; use the mock for
		// unrouted models (routed custom-provider models still work).
		routed := appserver.NewModelRoutedClientFactory(routes, fallback)
		return assembleResult(routed, cfg.CodexHome, model, selected.ID, derefSandboxMode(cfg.SandboxMode), trustGate)
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
	// With a real provider client wired, an unconfigured model resolves from the
	// bundled catalog like codex (ModelsManager::get_default_model -> the first
	// picker-visible preset), NOT the offline mock slug — the backend rejects
	// "gpt-mock". CODEXGO_MODEL still wins over the catalog as the env override
	// (resolveDefaultModel precedence), so only the final fallback changes here.
	if model == "" && os.Getenv("CODEXGO_MODEL") == "" {
		if slug := bundledDefaultModelSlug(); slug != "" {
			model = slug
		}
	}
	routed := appserver.NewModelRoutedClientFactory(routes, factory)
	return assembleResult(routed, cfg.CodexHome, model, selected.ID, derefSandboxMode(cfg.SandboxMode), trustGate)
}

// buildModelProviderRoutes builds the codexgo model→provider routing table from
// the configured [model_providers] entries that declare a `models` list AND a
// usable credential source (experimental_bearer_token or env_key). Providers
// without credentials are skipped so their models fall through to the default
// factory (and ultimately the mock) rather than failing the assembly.
func buildModelProviderRoutes(cfg loadedConfig, fallback appserver.ModelClientFactory) map[string]appserver.ModelClientFactory {
	routes := map[string]appserver.ModelClientFactory{}
	for id, info := range cfg.ModelProviders {
		if len(info.Models) == 0 {
			continue
		}
		resolver, ok := buildProviderAuthResolver(cfg, selectedProvider{ID: id, Info: info})
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: model provider %q declares models but no credential source; its models fall back to the default provider\n", id)
			continue
		}
		factory, err := appserver.NewModelClientFactory(appserver.RealModelClientFactoryConfig{
			AuthResolver:   resolver,
			Provider:       info,
			InstallationID: resolveInstallationID(),
			Fallback:       fallback,
			ModelCatalog:   bundledModelCatalog(),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: model provider %q unavailable: %v\n", id, err)
			continue
		}
		for _, slug := range info.Models {
			if slug != "" {
				routes[slug] = factory
			}
		}
	}
	return routes
}

// bundledDefaultModelSlug resolves the default model slug from the bundled
// catalog the way codex's session does with no configured model
// (session/mod.rs get_default_model -> default_model_from_available): the
// first picker-visible preset wins (gpt-5.5 in the 0.136.0 catalog). It
// returns "" when the bundle cannot be decoded so callers can keep their
// existing fallback.
func bundledDefaultModelSlug() string {
	resp, err := modelsmanager.BundledModelsResponse()
	if err != nil {
		return ""
	}
	mgr := modelsmanager.NewStaticModelsManager(nil, resp)
	return mgr.GetDefaultModel(context.Background(), nil, modelsmanager.RefreshOffline)
}

// buildProjectTrustGate returns a per-cwd predicate reporting whether the
// project containing cwd is trusted under the merged config's `[projects]`
// table, mirroring the Rust loader's ProjectTrustContext gate. The headless host
// uses it to enable project-layer `.codex/skills` loading only for trusted
// projects. A gate is always returned (nil cfg fields simply yield "untrusted").
func buildProjectTrustGate(cfg loadedConfig) projectTrustGate {
	merged := cfg.Merged
	markers := cfg.ProjectRootMarkers
	return func(cwd abspath.AbsolutePathBuf) bool {
		if merged == nil {
			return false
		}
		return config.IsProjectTrusted(merged, cwd, markers)
	}
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
// use the login-backed resolver, which honors OPENAI_API_KEY / CODEXGO_API_KEY /
// auth.json / ChatGPT login. Other providers authenticate from a literal
// experimental_bearer_token or their declared env_key. It returns ok == false
// when no credential source applies, so the caller selects the mock fallback.
func buildProviderAuthResolver(cfg loadedConfig, selected selectedProvider) (appserver.AuthResolver, bool) {
	if isOpenAIAuthProvider(selected.Info) {
		return newLoginAuthResolver(authResolverConfig{
			CodexHome:            cfg.CodexHome,
			StoreMode:            cfg.StoreMode,
			ChatgptBaseURL:       cfg.ChatgptBaseURL,
			EnableCodexAPIKeyEnv: true,
		}), true
	}
	if token := selected.Info.ExperimentalBearerToken; token != nil && *token != "" {
		return &bearerTokenAuthResolver{token: *token}, true
	}
	if envKeyDefined(selected.Info) {
		return &envKeyAuthResolver{info: selected.Info}, true
	}
	return nil, false
}

// assembleResult builds the engine with the given model-client factory, codex
// home, default model slug, and provider id, returning the assembly alongside the
// resolved per-session Defaults. An empty codexHome is resolved from the
// environment; an empty defaultModel falls back to CODEXGO_MODEL and then the mock
// slug. The same resolved model + provider id flow into both the assembly's
// models manager and the returned Defaults so the binary's exec/review/TUI paths
// honor the configured selection.
func assembleResult(factory appserver.ModelClientFactory, codexHome, defaultModel, providerID string, sandboxMode protocol.SandboxMode, trustGate projectTrustGate) (*appserver.Assembly, appserver.Defaults, error) {
	if codexHome == "" {
		codexHome = resolveCodexHome()
	}
	if sandboxMode == "" {
		sandboxMode = protocol.SandboxModeReadOnly
	}
	// Wire the provider capability resolver so the turn path can gate
	// namespace/tool_search and hosted web_search on the active provider's
	// upper bounds (all-true for OpenAI/configured providers; Amazon Bedrock
	// disables the hosted tools). Mirrors turn_context.provider.capabilities().
	core.SetProviderCapabilitiesResolver(func(providerID string) core.ProviderCapabilities {
		caps := modelproviderinfo.CapabilitiesForProvider(providerID)
		return core.ProviderCapabilities{
			NamespaceTools:  caps.NamespaceTools,
			ImageGeneration: caps.ImageGeneration,
			WebSearch:       caps.WebSearch,
		}
	})
	model := resolveDefaultModel(defaultModel)
	// Open the SQLite state runtime under the codex home so goal tools persist
	// thread goals exactly like codex (goal_tools_supported requires the state
	// DB). A failed open degrades to no goal tools rather than failing the
	// assembly, mirroring codex's state_db().is_none() path. The runtime stays
	// open for the process lifetime (the pools close on exit).
	var goalStateRuntime *state.StateRuntime
	if rt, err := state.InitRuntime(context.Background(), codexHome, providerID); err == nil {
		goalStateRuntime = rt
	} else {
		fmt.Fprintf(os.Stderr, "warning: state runtime unavailable, goal tools disabled: %v\n", err)
	}
	// The skills manager installs the embedded system skills under
	// CODEXGO_HOME/skills/.system and renders the <skills_instructions> developer
	// section for new threads, like codex's include_skill_instructions default.
	var skillsManager core.SkillsManager
	if sm, err := newAssemblySkillsManagerWithTrust(codexHome, true /* bundled */, trustGate); err == nil {
		skillsManager = sm
	} else {
		fmt.Fprintf(os.Stderr, "warning: skills manager unavailable, skills instructions disabled: %v\n", err)
	}
	// Multi-agent control plane + goal event sink: the thread manager only exists
	// after Assemble returns, while the per-thread router factory below closes over
	// it, so the manager is published through a guarded holder after assembly. The
	// holder is shared by the collab control plane (engine) and the goal event sink
	// (session lookup). The spawn-edge graph is process-wide (shared across roots),
	// like the Rust agent graph.
	threadMgr := &threadManagerHolder{}
	collabGraph := agentgraph.NewInMemoryAgentGraphStore()
	asm, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: factory,
		SkillsManager:      skillsManager,
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
		ToolRouterFactory: func(threadID protocol.ThreadID) (core.ToolRouter, error) {
			deps := core.BuiltinToolDeps{
				Exec:        newLocalExecService(),
				UnifiedExec: unifiedexec.NewExecutor(nil),
				// request_user_input is advertised by default (codex's
				// experimental_request_user_input_enabled). Headless exec has no
				// interactive client, so calls resolve as cancelled; the TUI /
				// app-server clients supply a real requester when they land.
				UserInput: headlessUserInputRequester{},
			}
			if goalStateRuntime != nil {
				// Goal tools persist per-thread goals in the goals DB; the event
				// sink routes thread_goal_updated accounting events to this thread's
				// session event stream (late-bound via the shared thread-manager
				// holder, since the session does not exist yet). The metrics client
				// stays nil (headless default).
				deps.GoalTools = goal.NewToolExecutors(
					threadID,
					goal.NewStateRuntimeBridge(goalStateRuntime),
					newGoalEventSink(threadID, threadMgr),
					nil,
				)
			}
			// Wire the multi-agent control plane so the deferred collab tools
			// (spawn_agent et al., discovered via tool_search) actually execute.
			// Each root thread gets its own Control (registry scope) over the
			// shared engine + spawn-edge graph, mirroring the per-session
			// AgentControl in codex.
			engine := threadMgr.get()
			if engine != nil {
				control, cerr := multiagent.NewControl(multiagent.Config{
					Engine:    engine,
					Graph:     collabGraph,
					SessionID: threadID.ToSessionID(),
				})
				if cerr == nil {
					control.RegisterSessionRoot(threadID, rollout.NewCliSource())
					deps.Collab = multiagent.NewCollabAdapter(control)
				} else {
					fmt.Fprintf(os.Stderr, "warning: collab control unavailable: %v\n", cerr)
				}
			}
			return core.BuiltinToolRouter(deps)
		},
	})
	if err != nil {
		return nil, appserver.Defaults{}, err
	}
	threadMgr.set(asm.ThreadManager)
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
// offline/dev fallback. The reply is overridable via CODEXGO_EXEC_MOCK_REPLY.
func mockClientFactory() appserver.ModelClientFactory {
	reply := os.Getenv("CODEXGO_EXEC_MOCK_REPLY")
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
// assembly's resolveDefaultModel then layers CODEXGO_MODEL and the mock slug below
// it, so a config model wins over the env var, matching the Rust precedence
// (config model resolved before the env-derived defaults).
func configDefaultModel(cfg loadedConfig) string {
	if cfg.DefaultModel != nil && *cfg.DefaultModel != "" {
		return *cfg.DefaultModel
	}
	return ""
}

// resolveDefaultModel returns the effective default model slug for the assembly.
// The configured model wins; otherwise CODEXGO_MODEL is honored, and finally the
// mock slug keeps the offline path working out of the box.
func resolveDefaultModel(configModel string) string {
	if configModel != "" {
		return configModel
	}
	if slug := os.Getenv("CODEXGO_MODEL"); slug != "" {
		return slug
	}
	return defaultMockModelSlug
}

// resolveInstallationID returns the installation identifier sent in routing
// headers, honoring CODEXGO_INSTALLATION_ID when set.
func resolveInstallationID() string {
	return os.Getenv("CODEXGO_INSTALLATION_ID")
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

// resolveCodexHome resolves the codexgo configuration directory, falling back
// to ".codexgo" when neither CODEXGO_HOME nor the home directory is available.
func resolveCodexHome() string {
	if home, err := config.FindCodexHome(); err == nil {
		return home
	}
	if home := os.Getenv("CODEXGO_HOME"); home != "" {
		return home
	}
	return config.DefaultCodexDirName
}

// resolveCwd returns the current working directory, defaulting to "." when it
// cannot be determined.
func resolveCwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
