package modelproviderinfo

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// Validate checks the mutually-exclusive auth configuration for this provider.
// It returns an error string (not wrapped, to match the Rust Result<(), String>)
// describing the first conflict found.
//
// Rules, in order:
//   - aws cannot be combined with supports_websockets.
//   - aws cannot be combined with env_key, experimental_bearer_token, auth, or
//     requires_openai_auth (conflicts reported together, in field order).
//   - when auth is set, auth.command must be non-empty.
//   - auth cannot be combined with env_key, experimental_bearer_token, or
//     requires_openai_auth (conflicts reported together, in field order).
func (p ModelProviderInfo) Validate() error {
	if p.Aws != nil {
		if p.SupportsWebsockets {
			// TODO: AWS SigV4 signing for WebSocket upgrade requests is not yet
			// supported, so AWS-authenticated providers cannot enable
			// Responses-over-WebSocket.
			return &ValidationError{Message: "provider aws cannot be combined with supports_websockets"}
		}

		var conflicts []string
		if p.EnvKey != nil {
			conflicts = append(conflicts, "env_key")
		}
		if p.ExperimentalBearerToken != nil {
			conflicts = append(conflicts, "experimental_bearer_token")
		}
		if p.Auth != nil {
			conflicts = append(conflicts, "auth")
		}
		if p.RequiresOpenAIAuth {
			conflicts = append(conflicts, "requires_openai_auth")
		}
		if len(conflicts) > 0 {
			return &ValidationError{Message: "provider aws cannot be combined with " + strings.Join(conflicts, ", ")}
		}
	}

	if p.Auth == nil {
		return nil
	}

	if strings.TrimSpace(p.Auth.Command) == "" {
		return &ValidationError{Message: "provider auth.command must not be empty"}
	}

	var conflicts []string
	if p.EnvKey != nil {
		conflicts = append(conflicts, "env_key")
	}
	if p.ExperimentalBearerToken != nil {
		conflicts = append(conflicts, "experimental_bearer_token")
	}
	if p.RequiresOpenAIAuth {
		conflicts = append(conflicts, "requires_openai_auth")
	}
	if len(conflicts) == 0 {
		return nil
	}
	return &ValidationError{Message: "provider auth cannot be combined with " + strings.Join(conflicts, ", ")}
}

// ValidationError is returned by Validate. Its message matches the Rust String
// error verbatim.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// buildHeaderMap assembles the static and env-derived HTTP headers, mirroring
// the Rust build_header_map: invalid header names/values are silently skipped,
// and env headers are only included when the env var is set to a non-empty
// (after trimming) value.
func (p ModelProviderInfo) buildHeaderMap() http.Header {
	headers := http.Header{}
	for name, value := range p.HTTPHeaders {
		if isValidHeaderName(name) && isValidHeaderValue(value) {
			headers.Set(name, value)
		}
	}
	for header, envVar := range p.EnvHTTPHeaders {
		val, ok := os.LookupEnv(envVar)
		if !ok || strings.TrimSpace(val) == "" {
			continue
		}
		if isValidHeaderName(header) && isValidHeaderValue(val) {
			headers.Set(header, val)
		}
	}
	return headers
}

// ToAPIProvider converts this provider into an api.Provider for the given auth
// mode. The default base URL is the ChatGPT codex endpoint for ChatGPT-style
// auth modes, otherwise the public OpenAI API. It mirrors the Rust
// to_api_provider.
func (p ModelProviderInfo) ToAPIProvider(authMode *appserverproto.AuthMode) api.Provider {
	defaultBaseURL := openaiDefaultBaseURL
	if isChatGPTAuthMode(authMode) {
		defaultBaseURL = ChatGPTCodexBaseURL
	}
	baseURL := defaultBaseURL
	if p.BaseURL != nil {
		baseURL = *p.BaseURL
	}

	return api.Provider{
		Name:        p.Name,
		BaseURL:     baseURL,
		QueryParams: cloneStringMap(p.QueryParams),
		Headers:     p.buildHeaderMap(),
		Retry: api.RetryConfig{
			MaxAttempts:    p.EffectiveRequestMaxRetries(),
			BaseDelay:      retryBaseDelay,
			Retry429:       false,
			Retry5xx:       true,
			RetryTransport: true,
		},
		StreamIdleTimeout: p.StreamIdleTimeout(),
	}
}

func isChatGPTAuthMode(authMode *appserverproto.AuthMode) bool {
	if authMode == nil {
		return false
	}
	switch *authMode {
	case appserverproto.AuthModeChatgpt,
		appserverproto.AuthModeChatgptAuthTokens,
		appserverproto.AuthModeAgentIdentity:
		return true
	default:
		return false
	}
}

// APIKey returns the API key for this provider when env_key is set: the value of
// the named environment variable if present and non-empty (after trimming).
// When env_key is nil it returns (nil, nil). When env_key is set but the
// variable is missing/empty it returns an *EnvVarError. It mirrors api_key.
func (p ModelProviderInfo) APIKey() (*string, error) {
	if p.EnvKey == nil {
		return nil, nil
	}
	envKey := *p.EnvKey
	val, ok := os.LookupEnv(envKey)
	if !ok || strings.TrimSpace(val) == "" {
		return nil, &EnvVarError{Var: envKey, Instructions: cloneStringPtr(p.EnvKeyInstructions)}
	}
	value := val
	return &value, nil
}

// EffectiveRequestMaxRetries returns the effective max request retries,
// defaulting to 4 and capped at 100. It mirrors the Rust request_max_retries
// method (renamed to avoid colliding with the RequestMaxRetries field).
func (p ModelProviderInfo) EffectiveRequestMaxRetries() uint64 {
	value := defaultRequestMaxRetries
	if p.RequestMaxRetries != nil {
		value = *p.RequestMaxRetries
	}
	if value > maxRequestMaxRetries {
		return maxRequestMaxRetries
	}
	return value
}

// EffectiveStreamMaxRetries returns the effective max stream reconnection
// attempts, defaulting to 5 and capped at 100. It mirrors the Rust
// stream_max_retries method (renamed to avoid colliding with the
// StreamMaxRetries field).
func (p ModelProviderInfo) EffectiveStreamMaxRetries() uint64 {
	value := defaultStreamMaxRetries
	if p.StreamMaxRetries != nil {
		value = *p.StreamMaxRetries
	}
	if value > maxStreamMaxRetries {
		return maxStreamMaxRetries
	}
	return value
}

// StreamIdleTimeout returns the effective idle timeout for streaming responses,
// defaulting to 300000ms.
func (p ModelProviderInfo) StreamIdleTimeout() time.Duration {
	ms := defaultStreamIdleTimeoutMS
	if p.StreamIdleTimeoutMS != nil {
		ms = *p.StreamIdleTimeoutMS
	}
	return time.Duration(ms) * time.Millisecond
}

// WebsocketConnectTimeout returns the effective websocket connect timeout,
// defaulting to 15000ms.
func (p ModelProviderInfo) WebsocketConnectTimeout() time.Duration {
	ms := DefaultWebsocketConnectTimeoutMS
	if p.WebsocketConnectTimeoutMS != nil {
		ms = *p.WebsocketConnectTimeoutMS
	}
	return time.Duration(ms) * time.Millisecond
}

// IsOpenAI reports whether this provider is the built-in OpenAI provider, by name.
func (p ModelProviderInfo) IsOpenAI() bool { return p.Name == openaiProviderName }

// IsAmazonBedrock reports whether this provider is the built-in Amazon Bedrock
// provider, by name.
func (p ModelProviderInfo) IsAmazonBedrock() bool { return p.Name == amazonBedrockProviderName }

// SupportsRemoteCompaction reports whether the provider supports remote
// compaction: the OpenAI provider or any Azure Responses provider.
func (p ModelProviderInfo) SupportsRemoteCompaction() bool {
	baseURL := ""
	if p.BaseURL != nil {
		baseURL = *p.BaseURL
	}
	return p.IsOpenAI() || api.IsAzureResponsesProvider(p.Name, baseURL)
}

// HasCommandAuth reports whether the provider uses command-backed bearer-token auth.
func (p ModelProviderInfo) HasCommandAuth() bool { return p.Auth != nil }

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
