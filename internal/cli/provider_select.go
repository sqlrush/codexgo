package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/client"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/modelproviderinfo"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// defaultModelProviderID is the provider id used when `model_provider` is unset,
// matching the Rust default of "openai" in core/src/config/mod.rs.
const defaultModelProviderID = modelproviderinfo.OpenAIProviderID

// selectedProvider bundles the resolved provider id and its ModelProviderInfo as
// chosen from the merged catalog. It mirrors the (model_provider_id,
// model_provider) pair the Rust Config resolution produces.
type selectedProvider struct {
	// ID is the resolved `model_provider` selection (the active provider id).
	ID string
	// Info is the ModelProviderInfo for ID from the merged built-in + configured
	// catalog.
	Info modelproviderinfo.ModelProviderInfo
}

// resolveSelectedProvider resolves the active provider from the loaded config:
// it merges the configured `[model_providers]` map onto the built-in catalog
// (honoring `openai_base_url`), reads the `model_provider` selection (defaulting
// to "openai"), and looks up its ModelProviderInfo. It mirrors the Rust
// merge_configured_model_providers + model_provider_id lookup in
// core/src/config/mod.rs. Errors are wrapped with %w.
func resolveSelectedProvider(cfg loadedConfig) (selectedProvider, error) {
	catalog, err := modelproviderinfo.MergeConfiguredModelProviders(
		modelproviderinfo.BuiltInModelProviders(openaiBaseURLOverride(cfg)),
		cfg.ModelProviders,
	)
	if err != nil {
		return selectedProvider{}, fmt.Errorf("cli: merge model providers: %w", err)
	}

	id := defaultModelProviderID
	if cfg.ModelProviderID != nil && *cfg.ModelProviderID != "" {
		id = *cfg.ModelProviderID
	}

	info, ok := catalog[id]
	if !ok {
		if id == modelproviderinfo.LegacyOllamaChatProviderID {
			return selectedProvider{}, fmt.Errorf("cli: %s", modelproviderinfo.OllamaChatProviderRemovedError)
		}
		return selectedProvider{}, fmt.Errorf("cli: model provider %q not found", id)
	}
	if err := info.Validate(); err != nil {
		return selectedProvider{}, fmt.Errorf("cli: validate model provider %q: %w", id, err)
	}
	return selectedProvider{ID: id, Info: info}, nil
}

// openaiBaseURLOverride returns the trimmed non-empty `openai_base_url` override,
// or nil when unset/empty. It matches the Rust filter applied before passing it
// to built_in_model_providers.
func openaiBaseURLOverride(cfg loadedConfig) *string {
	if cfg.OpenAIBaseURL == nil {
		return nil
	}
	if strings.TrimSpace(*cfg.OpenAIBaseURL) == "" {
		return nil
	}
	value := *cfg.OpenAIBaseURL
	return &value
}

// isOpenAIAuthProvider reports whether the selected provider relies on the
// built-in OpenAI auth path (OPENAI_API_KEY / CODEXGO_API_KEY / auth.json / ChatGPT
// login) rather than a provider-specific `env_key` bearer token. It matches the
// Rust requires_openai_auth semantics: such providers go through the AuthManager,
// while env_key-only providers authenticate from their named environment variable.
func isOpenAIAuthProvider(info modelproviderinfo.ModelProviderInfo) bool {
	return info.RequiresOpenAIAuth
}

// envKeyAuthResolver resolves credentials from a provider's `env_key`
// environment variable, building a static bearer api.AuthProvider. It mirrors
// the Rust create_model_provider path where a non-OpenAI provider authenticates
// with the API key read from its env_key. When the variable is unset/empty it
// reports HasCredentials == false so the assembly falls back to the mock.
type envKeyAuthResolver struct {
	// info is the selected provider; APIKey reads its env_key value.
	info modelproviderinfo.ModelProviderInfo
}

// compile-time assertion that envKeyAuthResolver satisfies appserver.AuthResolver.
var _ appserver.AuthResolver = (*envKeyAuthResolver)(nil)

// Resolve reads the provider's env_key value and returns a bearer auth provider.
// A missing/empty env_key value yields HasCredentials == false (mock fallback),
// preserving the offline/dev behavior. The auth mode is reported as apikey so the
// api.Provider keeps the configured base URL rather than the ChatGPT default.
func (r *envKeyAuthResolver) Resolve(_ context.Context, _ protocol.ThreadID, _ core.SessionConfiguration) (appserver.ResolvedAuth, error) {
	key, err := r.info.APIKey()
	if err != nil {
		// env_key is set but the variable is missing/empty: no usable credential,
		// so fall back to the mock rather than aborting the spawn.
		return appserver.ResolvedAuth{}, nil
	}
	if key == nil || *key == "" {
		return appserver.ResolvedAuth{}, nil
	}

	mode := appserverproto.AuthModeApiKey
	return appserver.ResolvedAuth{
		HasCredentials: true,
		AuthProvider:   staticBearerAuthProvider{token: *key},
		AuthMode:       &mode,
	}, nil
}

// staticBearerAuthProvider is a minimal api.AuthProvider that attaches a fixed
// bearer token (a provider env_key value). It never mutates the incoming request.
type staticBearerAuthProvider struct{ token string }

// compile-time assertion that staticBearerAuthProvider satisfies api.AuthProvider.
var _ api.AuthProvider = staticBearerAuthProvider{}

// AddAuthHeaders sets the Authorization bearer header.
func (p staticBearerAuthProvider) AddAuthHeaders(headers http.Header) {
	if p.token != "" {
		headers.Set("Authorization", "Bearer "+p.token)
	}
}

// ApplyAuth returns a copy of req with the bearer header applied.
func (p staticBearerAuthProvider) ApplyAuth(_ context.Context, req client.Request) (client.Request, *api.AuthError) {
	out := req.WithCompression(req.Compression)
	p.AddAuthHeaders(out.Headers)
	return out, nil
}

// envKeyDefined reports whether the provider declares an env_key. Used to decide
// whether the env_key auth path applies at all.
func envKeyDefined(info modelproviderinfo.ModelProviderInfo) bool {
	return info.EnvKey != nil && *info.EnvKey != ""
}

// bearerTokenAuthResolver resolves credentials from a provider's literal
// `experimental_bearer_token` config value. It lets a custom provider (GLM,
// DeepSeek, …) carry its API key inside config.toml without requiring an
// environment variable.
type bearerTokenAuthResolver struct {
	token string
}

// compile-time assertion that bearerTokenAuthResolver satisfies appserver.AuthResolver.
var _ appserver.AuthResolver = (*bearerTokenAuthResolver)(nil)

// Resolve returns a static bearer auth provider for the configured token.
func (r *bearerTokenAuthResolver) Resolve(_ context.Context, _ protocol.ThreadID, _ core.SessionConfiguration) (appserver.ResolvedAuth, error) {
	if r.token == "" {
		return appserver.ResolvedAuth{}, nil
	}
	mode := appserverproto.AuthModeApiKey
	return appserver.ResolvedAuth{
		HasCredentials: true,
		AuthProvider:   staticBearerAuthProvider{token: r.token},
		AuthMode:       &mode,
	}, nil
}
