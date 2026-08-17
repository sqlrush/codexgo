package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/pkg/client"
)

// RetryConfig is the high-level retry configuration for a provider. It mirrors
// the Rust `RetryConfig`.
type RetryConfig struct {
	MaxAttempts    uint64
	BaseDelay      time.Duration
	Retry429       bool
	Retry5xx       bool
	RetryTransport bool
}

// ToPolicy converts the provider RetryConfig into a client.RetryPolicy, mirroring
// the Rust `RetryConfig::to_policy`.
func (c RetryConfig) ToPolicy() client.RetryPolicy {
	return client.RetryPolicy{
		MaxAttempts: c.MaxAttempts,
		BaseDelay:   c.BaseDelay,
		RetryOn: client.RetryOn{
			Retry429:       c.Retry429,
			Retry5xx:       c.Retry5xx,
			RetryTransport: c.RetryTransport,
		},
	}
}

// DefaultRetryConfig returns the codex default request retry configuration: 4
// attempts, 200ms base delay, retry on 5xx and transport errors only. It matches
// the ApiRetryConfig assembled in model-provider-info.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    client.DefaultRequestMaxRetries,
		BaseDelay:      client.DefaultBaseDelay,
		Retry429:       false,
		Retry5xx:       true,
		RetryTransport: true,
	}
}

// Provider is the HTTP endpoint configuration for a concrete API deployment. It
// mirrors the Rust `Provider` struct.
type Provider struct {
	Name              string
	BaseURL           string
	QueryParams       map[string]string
	Headers           http.Header
	Retry             RetryConfig
	StreamIdleTimeout time.Duration
}

// URLForPath joins the provider base URL with a path and appends sorted query
// params. It mirrors the Rust `url_for_path`; query params are sorted by key so
// the output is deterministic.
func (p Provider) URLForPath(path string) string {
	base := strings.TrimRight(p.BaseURL, "/")
	trimmedPath := strings.TrimLeft(path, "/")
	var url string
	if trimmedPath == "" {
		url = base
	} else {
		url = base + "/" + trimmedPath
	}
	if len(p.QueryParams) > 0 {
		keys := make([]string, 0, len(p.QueryParams))
		for k := range p.QueryParams {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+p.QueryParams[k])
		}
		url += "?" + strings.Join(parts, "&")
	}
	return url
}

// BuildRequest builds a client.Request for the given method and path, seeded with
// a copy of the provider headers. It mirrors the Rust `build_request`.
func (p Provider) BuildRequest(method, path string) client.Request {
	req := client.NewRequest(method, p.URLForPath(path))
	req.Headers = cloneHeader(p.Headers)
	return req
}

// IsAzureResponsesEndpoint reports whether this provider targets Azure. It
// mirrors the Rust `is_azure_responses_endpoint`.
func (p Provider) IsAzureResponsesEndpoint() bool {
	return IsAzureResponsesProvider(p.Name, p.BaseURL)
}

// WebsocketURLForPath returns the ws/wss URL for a path. It mirrors the Rust
// `websocket_url_for_path`: http->ws, https->wss, ws/wss unchanged.
func (p Provider) WebsocketURLForPath(path string) string {
	url := p.URLForPath(path)
	switch {
	case strings.HasPrefix(url, "https://"):
		return "wss://" + strings.TrimPrefix(url, "https://")
	case strings.HasPrefix(url, "http://"):
		return "ws://" + strings.TrimPrefix(url, "http://")
	default:
		return url
	}
}

// IsAzureResponsesProvider reports whether a provider name/base URL targets
// Azure. It mirrors the Rust `is_azure_responses_provider`.
func IsAzureResponsesProvider(name, baseURL string) bool {
	if strings.EqualFold(name, "azure") {
		return true
	}
	return matchesAzureResponsesBaseURL(baseURL)
}

func matchesAzureResponsesBaseURL(baseURL string) bool {
	lower := strings.ToLower(baseURL)
	markers := []string{
		"openai.azure.",
		"cognitiveservices.azure.",
		"aoai.azure.",
		"azure-api.",
		"azurefd.",
		"windows.net/openai",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// cloneHeader returns a deep copy of an http.Header, never nil.
func cloneHeader(h http.Header) http.Header {
	out := http.Header{}
	for k, vs := range h {
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}
