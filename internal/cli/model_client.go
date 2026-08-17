package cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/client"
	"github.com/sqlrush/codexgo/pkg/config"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file bridges the CLI's credential layer (internal/login + internal/config)
// onto the appserver model-client factory seam. It resolves credentials with the
// same precedence as the Rust AuthManager (env -> ephemeral -> agent-identity ->
// persistent store) and, when present, builds a provider-backed
// core.ResponsesModelClient via appserver.NewModelClientFactory; otherwise the
// scripted mock fallback runs so the engine works offline / in development.

// authResolverConfig configures [newLoginAuthResolver]. The HTTP client and the
// clock are injected so the resolver can be exercised in tests without touching
// the keyring or the network.
type authResolverConfig struct {
	// CodexHome is the resolved Codex configuration directory.
	CodexHome string
	// StoreMode is the auth credentials store mode (file/keyring/auto/ephemeral).
	StoreMode config.AuthCredentialsStoreMode
	// ChatgptBaseURL overrides the ChatGPT backend base URL (agent-identity JWKS).
	ChatgptBaseURL *string
	// EnableCodexAPIKeyEnv enables the CODEXGO_API_KEY env var auth path.
	EnableCodexAPIKeyEnv bool
	// HTTPClient is used for agent-identity JWT verification. When nil a default
	// login HTTP client is constructed lazily.
	HTTPClient *http.Client
}

// loginAuthResolver resolves credentials through internal/login and adapts the
// resulting login.CodexAuth into an appserver.ResolvedAuth. It implements
// appserver.AuthResolver.
type loginAuthResolver struct {
	cfg authResolverConfig
}

// newLoginAuthResolver builds a login-backed appserver.AuthResolver.
func newLoginAuthResolver(cfg authResolverConfig) *loginAuthResolver {
	return &loginAuthResolver{cfg: cfg}
}

// compile-time assertion that loginAuthResolver satisfies appserver.AuthResolver.
var _ appserver.AuthResolver = (*loginAuthResolver)(nil)

// Resolve loads credentials and adapts them into an appserver.ResolvedAuth. When
// no credentials are present it returns HasCredentials == false so the assembly
// selects the mock fallback. Errors are wrapped with %w.
func (r *loginAuthResolver) Resolve(ctx context.Context, _ protocol.ThreadID, _ core.SessionConfiguration) (appserver.ResolvedAuth, error) {
	httpClient := r.cfg.HTTPClient
	if httpClient == nil {
		hc, err := login.NewHTTPClient()
		if err != nil {
			return appserver.ResolvedAuth{}, fmt.Errorf("cli: build login http client: %w", err)
		}
		httpClient = hc
	}

	auth, err := login.LoadAuth(
		ctx,
		httpClient,
		r.cfg.CodexHome,
		r.cfg.EnableCodexAPIKeyEnv,
		r.cfg.StoreMode,
		r.cfg.ChatgptBaseURL,
	)
	if err != nil {
		return appserver.ResolvedAuth{}, fmt.Errorf("cli: load auth: %w", err)
	}
	if auth == nil {
		// LoadAuth does not consult OPENAI_API_KEY (that var is read during login,
		// not at runtime). Honor it directly so a bare API key in the environment
		// still selects the real client, matching the cheap presence check.
		if envKey := login.ReadOpenAIAPIKeyFromEnv(); envKey != nil {
			apiKeyAuth := login.FromAPIKey(*envKey)
			auth = &apiKeyAuth
		} else {
			return appserver.ResolvedAuth{}, nil
		}
	}

	provider, err := newCodexAuthProvider(*auth)
	if err != nil {
		return appserver.ResolvedAuth{}, fmt.Errorf("cli: build auth provider: %w", err)
	}

	mode := auth.APIAuthMode()
	return appserver.ResolvedAuth{
		HasCredentials: true,
		AuthProvider:   provider,
		AuthMode:       &mode,
	}, nil
}

// codexAuthProvider adapts a login.CodexAuth into an api.AuthProvider. It attaches
// the bearer token (API key or ChatGPT access token) plus the account-id and
// FedRAMP routing headers, mirroring the header contribution of the Rust
// SharedAuthProvider used by ModelClient.
//
// The auth snapshot is captured at construction time and never mutated, matching
// the immutability of login.CodexAuth.
type codexAuthProvider struct {
	token     string
	accountID string
	fedramp   bool
}

// newCodexAuthProvider builds a codexAuthProvider from a CodexAuth snapshot.
// Agent-identity auth has no bearer token and is rejected here; the request
// signing for that mode is a deferred concern (see core/client.go STUB note).
func newCodexAuthProvider(auth login.CodexAuth) (api.AuthProvider, error) {
	token, err := auth.GetToken()
	if err != nil {
		return nil, fmt.Errorf("cli: read auth token: %w", err)
	}
	provider := codexAuthProvider{token: token, fedramp: auth.IsFedrampAccount()}
	if id := auth.GetAccountID(); id != nil {
		provider.accountID = *id
	}
	return provider, nil
}

// compile-time assertion that codexAuthProvider satisfies api.AuthProvider.
var _ api.AuthProvider = codexAuthProvider{}

// AddAuthHeaders attaches the Authorization bearer header plus the account-id and
// FedRAMP routing headers when available.
func (p codexAuthProvider) AddAuthHeaders(headers http.Header) {
	if p.token != "" {
		headers.Set("Authorization", "Bearer "+p.token)
	}
	if p.accountID != "" {
		headers.Set("chatgpt-account-id", p.accountID)
	}
	if p.fedramp {
		headers.Set("OpenAI-FedRAMP", "1")
	}
}

// ApplyAuth returns a copy of the request with the auth headers applied.
func (p codexAuthProvider) ApplyAuth(_ context.Context, req client.Request) (client.Request, *api.AuthError) {
	out := req.WithCompression(req.Compression)
	p.AddAuthHeaders(out.Headers)
	return out, nil
}

// hasAnyCredentials reports whether any credential source is present without
// performing network I/O: an env API key, or a persisted auth.json. It mirrors
// the cheap presence check the exec provider selection uses to decide between the
// real and mock clients up front (the resolver re-checks per spawn).
func hasAnyCredentials(cfg authResolverConfig) bool {
	if login.ReadOpenAIAPIKeyFromEnv() != nil {
		return true
	}
	if cfg.EnableCodexAPIKeyEnv && login.ReadCodexAPIKeyFromEnv() != nil {
		return true
	}
	if login.ReadCodexAccessTokenFromEnv() != nil {
		return true
	}
	stored, err := login.LoadAuthDotJson(cfg.CodexHome, cfg.StoreMode)
	if err != nil {
		return false
	}
	return stored != nil
}
