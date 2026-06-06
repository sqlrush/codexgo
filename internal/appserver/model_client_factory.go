package appserver

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file wires the REAL model client into the assembly. It mirrors the Rust
// codex-core client.rs ModelClient::new path (auth-manager + provider ->
// ResponsesModelClient) and the exec/lib.rs provider selection: when resolved
// credentials are present a provider-backed [core.ResponsesModelClient] is
// built, and otherwise the assembly falls back to a mock client so the engine
// still runs offline / in development.

// ResolvedAuth is the outcome of resolving credentials for a turn. It is the
// decoupling seam between the assembly (which must not depend on internal/login
// or the keyring) and the credential source. Callers translate their concrete
// auth (e.g. login.CodexAuth) into this neutral shape.
//
// HasCredentials reports whether usable credentials were found. When false the
// assembly selects the mock fallback. When true, AuthProvider must be non-nil.
type ResolvedAuth struct {
	// HasCredentials reports whether usable credentials are present.
	HasCredentials bool
	// AuthProvider applies authentication to outbound Responses requests. It is
	// required when HasCredentials is true and ignored otherwise.
	AuthProvider api.AuthProvider
	// AuthMode is the resolved auth mode, used to pick the provider base URL
	// (ChatGPT vs. public OpenAI API). When nil the public OpenAI default is used.
	AuthMode *appserverproto.AuthMode
}

// AuthResolver resolves the credentials to use for a spawning thread. It is the
// small interface the assembly depends on so the real-vs-mock decision can be
// tested without a keyring, filesystem, or network. Implementations must be safe
// for concurrent use; Resolve is called once per spawned thread.
type AuthResolver interface {
	// Resolve returns the credentials for the thread. A nil error with
	// HasCredentials == false means "no credentials present"; the assembly then
	// uses the mock fallback. A non-nil error aborts the spawn.
	Resolve(ctx context.Context, threadID protocol.ThreadID, cfg core.SessionConfiguration) (ResolvedAuth, error)
}

// AuthResolverFunc adapts a function to the [AuthResolver] interface.
type AuthResolverFunc func(ctx context.Context, threadID protocol.ThreadID, cfg core.SessionConfiguration) (ResolvedAuth, error)

// Resolve calls the underlying function.
func (f AuthResolverFunc) Resolve(ctx context.Context, threadID protocol.ThreadID, cfg core.SessionConfiguration) (ResolvedAuth, error) {
	return f(ctx, threadID, cfg)
}

// RealModelClientFactoryConfig configures [NewModelClientFactory], the
// dependency-injection builder that returns a [ModelClientFactory] selecting a
// real provider-backed client when credentials are present and a mock fallback
// otherwise.
type RealModelClientFactoryConfig struct {
	// AuthResolver resolves per-thread credentials; required.
	AuthResolver AuthResolver

	// Provider is the resolved model-provider info used to build the api.Provider
	// for the targeted auth mode. When zero, the built-in OpenAI provider is used.
	Provider modelproviderinfo.ModelProviderInfo

	// Models, when set, supplies model metadata (context window, reasoning
	// support, service tiers) for the requested slug. When nil, minimal metadata
	// is derived from the slug, matching ModelInfoFromSlug fallback behavior.
	Models core.ModelsManager

	// ModelCatalog, when non-empty, is consulted to resolve full ModelInfo for the
	// requested slug. It is the authoritative metadata source; when empty the slug
	// fallback is used. This avoids forcing the assembly to own a models manager.
	ModelCatalog []modelsmanager.ModelInfo

	// Transport executes Responses requests. When nil the api/core defaults
	// (http.DefaultClient) are used.
	Transport client.HTTPTransport

	// InstallationID is the stable installation identifier sent in routing
	// headers and client metadata.
	InstallationID string

	// Fallback builds the offline/dev model client used when no credentials are
	// present; required. The cli/exec assembly supplies a scripted mock here.
	Fallback ModelClientFactory
}

// NewModelRoutedClientFactory wraps per-model-slug factories around a default
// factory: a session whose model slug appears in routes streams through that
// provider's factory; every other slug uses def. This is the codexgo
// model→provider routing extension — selecting "glm-5.1" reaches the glm
// provider while "gpt-5.5" keeps using the default (login-backed) provider,
// with no `model_provider` switch needed.
func NewModelRoutedClientFactory(routes map[string]ModelClientFactory, def ModelClientFactory) ModelClientFactory {
	if len(routes) == 0 {
		return def
	}
	return func(ctx context.Context, threadID protocol.ThreadID, sessionCfg core.SessionConfiguration) (core.ModelClient, error) {
		if factory, ok := routes[sessionCfg.Model()]; ok {
			return factory(ctx, threadID, sessionCfg)
		}
		return def(ctx, threadID, sessionCfg)
	}
}

// NewModelClientFactory builds a [ModelClientFactory] that selects a real
// provider-backed [core.ResponsesModelClient] when [AuthResolver] reports
// credentials and otherwise delegates to the configured mock Fallback. It is the
// Go analogue of the Rust ModelClient::new wiring combined with the exec
// provider-selection fallback.
//
// Errors are wrapped with %w. The returned factory is safe for concurrent use.
func NewModelClientFactory(cfg RealModelClientFactoryConfig) (ModelClientFactory, error) {
	if cfg.AuthResolver == nil {
		return nil, fmt.Errorf("appserver: RealModelClientFactoryConfig.AuthResolver is required")
	}
	if cfg.Fallback == nil {
		return nil, fmt.Errorf("appserver: RealModelClientFactoryConfig.Fallback is required")
	}

	provider := cfg.Provider
	if provider.Name == "" && provider.BaseURL == nil {
		provider = modelproviderinfo.CreateOpenAIProvider(nil)
	}

	return func(ctx context.Context, threadID protocol.ThreadID, sessionCfg core.SessionConfiguration) (core.ModelClient, error) {
		resolved, err := cfg.AuthResolver.Resolve(ctx, threadID, sessionCfg)
		if err != nil {
			return nil, fmt.Errorf("appserver: resolve auth for thread %s: %w", threadID.String(), err)
		}

		// No credentials: fall back to the mock so the engine still runs offline.
		if !resolved.HasCredentials || resolved.AuthProvider == nil {
			return cfg.Fallback(ctx, threadID, sessionCfg)
		}

		clientCfg, err := buildResponsesClientConfig(provider, resolved, sessionCfg, threadID, cfg)
		if err != nil {
			return nil, fmt.Errorf("appserver: build responses client config for thread %s: %w", threadID.String(), err)
		}

		// codexgo extension: providers declaring wire_api = "chat" stream over
		// the chat-completions protocol (GLM, DeepSeek, …). See DEVIATIONS.md
		// "wire_api chat".
		if provider.WireApi == modelproviderinfo.WireApiChat {
			chatClient, err := core.NewChatModelClient(clientCfg)
			if err != nil {
				return nil, fmt.Errorf("appserver: build chat model client for thread %s: %w", threadID.String(), err)
			}
			return chatClient, nil
		}

		client, err := core.NewResponsesModelClient(clientCfg)
		if err != nil {
			return nil, fmt.Errorf("appserver: build responses model client for thread %s: %w", threadID.String(), err)
		}
		return client, nil
	}, nil
}

// buildResponsesClientConfig assembles the immutable [core.ModelClientConfig] for
// a provider-backed client, mirroring the field mapping in the Rust
// ModelClient::new / Prompt-derived request construction.
func buildResponsesClientConfig(
	provider modelproviderinfo.ModelProviderInfo,
	resolved ResolvedAuth,
	sessionCfg core.SessionConfiguration,
	threadID protocol.ThreadID,
	cfg RealModelClientFactoryConfig,
) (core.ModelClientConfig, error) {
	slug := sessionCfg.Model()
	modelInfo := resolveModelInfo(slug, cfg.ModelCatalog)

	// Render the base instructions for the effective personality. codex resolves
	// it as cli.or(config.personality).or(Pragmatic) when the Personality feature
	// is enabled — and it is on by default (config/mod.rs) — then derives the
	// session base instructions via model_info.get_model_instructions(personality)
	// (session/mod.rs). gpt-5.5's models.json `base_instructions` bakes the
	// "friendly" personality, so sending that field verbatim diverged from codex,
	// which renders the template with the resolved (default Pragmatic) personality.
	modelInfo.BaseInstructions = modelInfo.GetModelInstructions(resolvePersonality(sessionCfg.Personality))

	apiProvider := provider.ToAPIProvider(resolved.AuthMode)

	clientCfg := core.ModelClientConfig{
		SessionID:      threadID.ToSessionID(),
		ThreadID:       threadID,
		InstallationID: cfg.InstallationID,
		Provider:       apiProvider,
		Auth:           resolved.AuthProvider,
		Transport:      cfg.Transport,
		ModelInfo:      modelInfo,
		// codex resolves the reasoning summary as
		// config.model_reasoning_summary.unwrap_or(model_info.default_reasoning_summary)
		// (turn_context.rs). Default to the model's value (gpt-5.5 = "none", so no
		// summary is sent) and let an explicit session override win below.
		ReasoningSummary: modelInfo.DefaultReasoningSummary,
		ServiceTier:      modelInfo.ServiceTierForRequest(sessionCfg.ServiceTier),
		// Mirror the Rust Prompt default (output_schema_strict: true). codex exec
		// exposes no flag to change it, so an --output-schema request must send
		// "strict": true to match the reference binary's text.format block.
		OutputSchemaStrict: true,
		// codex derives parallel_tool_calls from the model capability
		// (compact_remote.rs: prompt.parallel_tool_calls =
		// turn_context.model_info.supports_parallel_tool_calls). gpt-5.5 sets it
		// true; without this the request always sent false.
		ParallelToolCalls: modelInfo.SupportsParallelToolCalls,
	}

	if effort := reasoningEffortFor(sessionCfg); effort != nil {
		clientCfg.ReasoningEffort = effort
	}
	if sessionCfg.ModelReasoningSummary != nil {
		clientCfg.ReasoningSummary = *sessionCfg.ModelReasoningSummary
	}

	return clientCfg, nil
}

// resolveModelInfo resolves model metadata for slug from the supplied catalog,
// falling back to minimal slug-derived metadata when the catalog is empty or has
// no matching entry. It mirrors constructModelInfoFromCandidates without the
// config-override layer (owned by the models-manager area).
func resolveModelInfo(slug string, catalog []modelsmanager.ModelInfo) modelsmanager.ModelInfo {
	for i := range catalog {
		if catalog[i].Slug == slug {
			info := catalog[i]
			info.Slug = slug
			return info
		}
	}
	return modelsmanager.ModelInfoFromSlug(slug)
}

// resolvePersonality returns the effective model personality, mirroring codex's
// cli.or(config.personality).or(Pragmatic) resolution (config/mod.rs): an explicit
// session personality wins; otherwise it defaults to Pragmatic (the Personality
// feature is enabled by default in codex 0.136.0). The returned pointer is safe to
// pass to GetModelInstructions, which falls back to base instructions for models
// without a personality template.
func resolvePersonality(p *protocol.Personality) *protocol.Personality {
	if p != nil {
		return p
	}
	pragmatic := protocol.PersonalityPragmatic
	return &pragmatic
}

// reasoningEffortFor extracts the per-turn reasoning effort from the session
// collaboration mode, returning nil to fall back to the model default.
func reasoningEffortFor(cfg core.SessionConfiguration) *protocol.ReasoningEffort {
	effort := cfg.CollaborationMode.Settings.ReasoningEffort
	if effort == nil {
		return nil
	}
	value := *effort
	return &value
}
