package modelproviderinfo

import (
	"encoding/json"
	"fmt"
)

// ModelProviderInfo is the serializable representation of a provider definition.
//
// It mirrors the Rust ModelProviderInfo struct field-for-field. Serde does not
// apply a rename_all, so JSON/TOML keys are the snake_case field names as
// written. Optional fields (Go pointers / nilable maps) without a
// skip_serializing_if always serialize (null when absent), matching Rust's
// Option<T> with no skip. `name` defaults to "" and the bool fields default to
// false; `wire_api` defaults to "responses".
type ModelProviderInfo struct {
	// Name is the friendly display name.
	Name string
	// BaseURL is the base URL for the provider's OpenAI-compatible API.
	BaseURL *string
	// EnvKey is the environment variable that stores the user's API key.
	EnvKey *string
	// EnvKeyInstructions helps the user obtain and set a valid env_key value.
	EnvKeyInstructions *string
	// ExperimentalBearerToken is a literal bearer token (discouraged; prefer
	// EnvKey for security).
	ExperimentalBearerToken *string
	// Auth is command-backed bearer-token configuration for this provider.
	Auth *ModelProviderAuthInfo
	// Aws is AWS SigV4 auth configuration for this provider.
	Aws *ModelProviderAwsAuthInfo
	// WireApi is the wire protocol this provider expects.
	WireApi WireApi
	// QueryParams are optional query parameters appended to the base URL.
	QueryParams map[string]string
	// HTTPHeaders are additional HTTP headers to include (name -> value).
	HTTPHeaders map[string]string
	// EnvHTTPHeaders are HTTP headers whose values come from environment
	// variables (header name -> env var). Unset/empty env vars are skipped.
	EnvHTTPHeaders map[string]string
	// RequestMaxRetries is the max retries for a failed HTTP request.
	RequestMaxRetries *uint64
	// StreamMaxRetries is the max reconnection attempts for a dropped stream.
	StreamMaxRetries *uint64
	// StreamIdleTimeoutMS is the idle timeout (ms) before treating a stream as lost.
	StreamIdleTimeoutMS *uint64
	// WebsocketConnectTimeoutMS is the max time (ms) for a websocket connect.
	WebsocketConnectTimeoutMS *uint64
	// RequiresOpenAIAuth indicates the provider needs an OpenAI API key or
	// ChatGPT login token (drives the first-run login screen).
	RequiresOpenAIAuth bool
	// SupportsWebsockets indicates the provider supports the Responses API
	// WebSocket transport.
	SupportsWebsockets bool
}

// DefaultModelProviderInfo returns the zero-value provider with serde defaults
// applied (name "", wire_api "responses", bools false). It mirrors the Rust
// #[derive(Default)] where WireApi::default() is Responses.
func DefaultModelProviderInfo() ModelProviderInfo {
	return ModelProviderInfo{WireApi: WireApiResponses}
}

// providerInfoJSON is the wire shape of ModelProviderInfo, listing fields in the
// same declaration order as the Rust struct so canonical key ordering matches.
type providerInfoJSON struct {
	Name                      string                    `json:"name"`
	BaseURL                   *string                   `json:"base_url"`
	EnvKey                    *string                   `json:"env_key"`
	EnvKeyInstructions        *string                   `json:"env_key_instructions"`
	ExperimentalBearerToken   *string                   `json:"experimental_bearer_token"`
	Auth                      *ModelProviderAuthInfo    `json:"auth"`
	Aws                       *ModelProviderAwsAuthInfo `json:"aws"`
	WireApi                   WireApi                   `json:"wire_api"`
	QueryParams               map[string]string         `json:"query_params"`
	HTTPHeaders               map[string]string         `json:"http_headers"`
	EnvHTTPHeaders            map[string]string         `json:"env_http_headers"`
	RequestMaxRetries         *uint64                   `json:"request_max_retries"`
	StreamMaxRetries          *uint64                   `json:"stream_max_retries"`
	StreamIdleTimeoutMS       *uint64                   `json:"stream_idle_timeout_ms"`
	WebsocketConnectTimeoutMS *uint64                   `json:"websocket_connect_timeout_ms"`
	RequiresOpenAIAuth        bool                      `json:"requires_openai_auth"`
	SupportsWebsockets        bool                      `json:"supports_websockets"`
}

// MarshalJSON encodes the provider, emitting every field (null for absent
// optionals) so the JSON matches Rust serde output.
func (p ModelProviderInfo) MarshalJSON() ([]byte, error) {
	wireAPI := p.WireApi
	if wireAPI == "" {
		wireAPI = WireApiResponses
	}
	return json.Marshal(providerInfoJSON{
		Name:                      p.Name,
		BaseURL:                   p.BaseURL,
		EnvKey:                    p.EnvKey,
		EnvKeyInstructions:        p.EnvKeyInstructions,
		ExperimentalBearerToken:   p.ExperimentalBearerToken,
		Auth:                      p.Auth,
		Aws:                       p.Aws,
		WireApi:                   wireAPI,
		QueryParams:               p.QueryParams,
		HTTPHeaders:               p.HTTPHeaders,
		EnvHTTPHeaders:            p.EnvHTTPHeaders,
		RequestMaxRetries:         p.RequestMaxRetries,
		StreamMaxRetries:          p.StreamMaxRetries,
		StreamIdleTimeoutMS:       p.StreamIdleTimeoutMS,
		WebsocketConnectTimeoutMS: p.WebsocketConnectTimeoutMS,
		RequiresOpenAIAuth:        p.RequiresOpenAIAuth,
		SupportsWebsockets:        p.SupportsWebsockets,
	})
}

// UnmarshalJSON decodes a provider, applying serde defaults: an absent name is
// "", absent wire_api becomes Responses, and absent bools are false. Unknown
// fields are ignored (deny_unknown_fields only affects the JSON schema, not
// serde deserialization).
func (p *ModelProviderInfo) UnmarshalJSON(data []byte) error {
	type rawProviderInfo struct {
		Name                      *string                   `json:"name"`
		BaseURL                   *string                   `json:"base_url"`
		EnvKey                    *string                   `json:"env_key"`
		EnvKeyInstructions        *string                   `json:"env_key_instructions"`
		ExperimentalBearerToken   *string                   `json:"experimental_bearer_token"`
		Auth                      *ModelProviderAuthInfo    `json:"auth"`
		Aws                       *ModelProviderAwsAuthInfo `json:"aws"`
		WireApi                   *WireApi                  `json:"wire_api"`
		QueryParams               map[string]string         `json:"query_params"`
		HTTPHeaders               map[string]string         `json:"http_headers"`
		EnvHTTPHeaders            map[string]string         `json:"env_http_headers"`
		RequestMaxRetries         *uint64                   `json:"request_max_retries"`
		StreamMaxRetries          *uint64                   `json:"stream_max_retries"`
		StreamIdleTimeoutMS       *uint64                   `json:"stream_idle_timeout_ms"`
		WebsocketConnectTimeoutMS *uint64                   `json:"websocket_connect_timeout_ms"`
		RequiresOpenAIAuth        *bool                     `json:"requires_openai_auth"`
		SupportsWebsockets        *bool                     `json:"supports_websockets"`
	}
	var raw rawProviderInfo
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode model provider info: %w", err)
	}

	out := DefaultModelProviderInfo()
	if raw.Name != nil {
		out.Name = *raw.Name
	}
	out.BaseURL = raw.BaseURL
	out.EnvKey = raw.EnvKey
	out.EnvKeyInstructions = raw.EnvKeyInstructions
	out.ExperimentalBearerToken = raw.ExperimentalBearerToken
	out.Auth = raw.Auth
	out.Aws = raw.Aws
	if raw.WireApi != nil {
		out.WireApi = *raw.WireApi
	}
	out.QueryParams = raw.QueryParams
	out.HTTPHeaders = raw.HTTPHeaders
	out.EnvHTTPHeaders = raw.EnvHTTPHeaders
	out.RequestMaxRetries = raw.RequestMaxRetries
	out.StreamMaxRetries = raw.StreamMaxRetries
	out.StreamIdleTimeoutMS = raw.StreamIdleTimeoutMS
	out.WebsocketConnectTimeoutMS = raw.WebsocketConnectTimeoutMS
	if raw.RequiresOpenAIAuth != nil {
		out.RequiresOpenAIAuth = *raw.RequiresOpenAIAuth
	}
	if raw.SupportsWebsockets != nil {
		out.SupportsWebsockets = *raw.SupportsWebsockets
	}
	*p = out
	return nil
}

// Equal reports whether two providers are equal by value, mirroring the Rust
// derived PartialEq (nil and empty maps compare equal; nil and empty Args within
// Auth compare equal).
func (p ModelProviderInfo) Equal(other ModelProviderInfo) bool {
	if p.Name != other.Name ||
		!optStringEqual(p.BaseURL, other.BaseURL) ||
		!optStringEqual(p.EnvKey, other.EnvKey) ||
		!optStringEqual(p.EnvKeyInstructions, other.EnvKeyInstructions) ||
		!optStringEqual(p.ExperimentalBearerToken, other.ExperimentalBearerToken) ||
		!effectiveWireApi(p.WireApi).Equal(effectiveWireApi(other.WireApi)) ||
		!optUint64Equal(p.RequestMaxRetries, other.RequestMaxRetries) ||
		!optUint64Equal(p.StreamMaxRetries, other.StreamMaxRetries) ||
		!optUint64Equal(p.StreamIdleTimeoutMS, other.StreamIdleTimeoutMS) ||
		!optUint64Equal(p.WebsocketConnectTimeoutMS, other.WebsocketConnectTimeoutMS) ||
		p.RequiresOpenAIAuth != other.RequiresOpenAIAuth ||
		p.SupportsWebsockets != other.SupportsWebsockets {
		return false
	}
	if !authEqual(p.Auth, other.Auth) || !awsEqual(p.Aws, other.Aws) {
		return false
	}
	return stringMapEqual(p.QueryParams, other.QueryParams) &&
		stringMapEqual(p.HTTPHeaders, other.HTTPHeaders) &&
		stringMapEqual(p.EnvHTTPHeaders, other.EnvHTTPHeaders)
}

// Equal reports WireApi equality.
func (w WireApi) Equal(other WireApi) bool {
	return effectiveWireApi(w) == effectiveWireApi(other)
}

func effectiveWireApi(w WireApi) WireApi {
	if w == "" {
		return WireApiResponses
	}
	return w
}

func authEqual(a, b *ModelProviderAuthInfo) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func awsEqual(a, b *ModelProviderAwsAuthInfo) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func optUint64Equal(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
