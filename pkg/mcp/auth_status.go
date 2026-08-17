package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/pkg/config"
)

// McpAuthStatus is the authentication status for an MCP server. It is a port of
// the reference codex_protocol McpAuthStatus enum; the wire form is snake_case.
type McpAuthStatus string

// McpAuthStatus values.
const (
	// McpAuthStatusUnsupported means the server does not advertise OAuth and no
	// other credentials are configured.
	McpAuthStatusUnsupported McpAuthStatus = "unsupported"
	// McpAuthStatusNotLoggedIn means the server supports OAuth but no tokens are
	// stored yet.
	McpAuthStatusNotLoggedIn McpAuthStatus = "not_logged_in"
	// McpAuthStatusBearerToken means a static bearer token (env var or header)
	// is configured.
	McpAuthStatusBearerToken McpAuthStatus = "bearer_token"
	// McpAuthStatusOAuth means OAuth tokens are stored for the server.
	McpAuthStatusOAuth McpAuthStatus = "oauth"
)

// String renders the human-readable label, matching the reference Display impl.
func (s McpAuthStatus) String() string {
	switch s {
	case McpAuthStatusUnsupported:
		return "Unsupported"
	case McpAuthStatusNotLoggedIn:
		return "Not logged in"
	case McpAuthStatusBearerToken:
		return "Bearer token"
	case McpAuthStatusOAuth:
		return "OAuth"
	default:
		return string(s)
	}
}

// discoveryTimeout bounds each OAuth discovery probe, matching the reference
// DISCOVERY_TIMEOUT.
const discoveryTimeout = 5 * time.Second

// oauthDiscoveryHeader and oauthDiscoveryVersion are sent on discovery probes,
// matching the reference constants.
const (
	oauthDiscoveryHeader  = "MCP-Protocol-Version"
	oauthDiscoveryVersion = "2024-11-05"
)

// StreamableHTTPOAuthDiscovery reports the OAuth capabilities advertised by a
// streamable HTTP MCP server. It mirrors the reference struct.
type StreamableHTTPOAuthDiscovery struct {
	// ScopesSupported lists the normalized scopes advertised by the server, or
	// nil when none were advertised.
	ScopesSupported []string
}

// DetermineStreamableHTTPAuthStatus computes the authentication status for a
// streamable HTTP MCP server, porting the reference
// determine_streamable_http_auth_status. The resolution order is: a configured
// bearer-token env var, an Authorization header, stored OAuth tokens, then live
// OAuth discovery.
func DetermineStreamableHTTPAuthStatus(
	ctx context.Context,
	store *OAuthStore,
	serverName, serverURL string,
	bearerTokenEnvVar *string,
	httpHeaders, envHTTPHeaders map[string]string,
	mode config.OAuthCredentialsStoreMode,
) (McpAuthStatus, error) {
	if bearerTokenEnvVar != nil {
		return McpAuthStatusBearerToken, nil
	}

	headers := BuildHTTPHeaders(httpHeaders, envHTTPHeaders, nil)
	if headers.Get("Authorization") != "" {
		return McpAuthStatusBearerToken, nil
	}

	has, err := store.Has(serverName, serverURL, mode)
	if err != nil {
		return "", fmt.Errorf("mcp: check stored OAuth tokens: %w", err)
	}
	if has {
		return McpAuthStatusOAuth, nil
	}

	discovery, derr := discoverStreamableHTTPOAuthWithHeaders(ctx, serverURL, headers)
	if derr != nil {
		// Discovery failures are treated as "unsupported", matching the
		// reference which logs and returns Unsupported.
		return McpAuthStatusUnsupported, nil
	}
	if discovery == nil {
		return McpAuthStatusUnsupported, nil
	}
	return McpAuthStatusNotLoggedIn, nil
}

// SupportsOAuthLogin reports whether a streamable HTTP MCP server advertises an
// OAuth login flow. It mirrors the reference supports_oauth_login.
func SupportsOAuthLogin(ctx context.Context, serverURL string) (bool, error) {
	discovery, err := DiscoverStreamableHTTPOAuth(ctx, serverURL, nil, nil)
	if err != nil {
		return false, err
	}
	return discovery != nil, nil
}

// DiscoverStreamableHTTPOAuth probes a server for RFC 8414 OAuth authorization
// server metadata, returning its discovery info or nil when unsupported. It
// mirrors the reference discover_streamable_http_oauth.
func DiscoverStreamableHTTPOAuth(
	ctx context.Context,
	serverURL string,
	httpHeaders, envHTTPHeaders map[string]string,
) (*StreamableHTTPOAuthDiscovery, error) {
	headers := BuildHTTPHeaders(httpHeaders, envHTTPHeaders, nil)
	return discoverStreamableHTTPOAuthWithHeaders(ctx, serverURL, headers)
}

// oauthDiscoveryMetadata is the subset of the RFC 8414 metadata document Codex
// inspects.
type oauthDiscoveryMetadata struct {
	AuthorizationEndpoint *string  `json:"authorization_endpoint"`
	TokenEndpoint         *string  `json:"token_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// discoverStreamableHTTPOAuthWithHeaders implements RFC 8414 section 3.1
// discovery: each well-known path candidate is probed in turn; the first that
// returns a metadata document with both endpoints wins.
func discoverStreamableHTTPOAuthWithHeaders(
	ctx context.Context,
	serverURL string,
	headers http.Header,
) (*StreamableHTTPOAuthDiscovery, error) {
	base, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("mcp: parse server url: %w", err)
	}

	client := &http.Client{Timeout: discoveryTimeout}

	var lastErr error
	for _, candidate := range discoveryPaths(base.Path) {
		probe := *base
		probe.Path = candidate
		probe.RawQuery = ""
		probe.Fragment = ""

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, probe.String(), nil)
		if reqErr != nil {
			lastErr = reqErr
			continue
		}
		for key, values := range headers {
			for _, v := range values {
				req.Header.Add(key, v)
			}
		}
		req.Header.Set(oauthDiscoveryHeader, oauthDiscoveryVersion)

		resp, doErr := client.Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		var metadata oauthDiscoveryMetadata
		decErr := json.NewDecoder(resp.Body).Decode(&metadata)
		resp.Body.Close()
		if decErr != nil {
			lastErr = decErr
			continue
		}
		if metadata.AuthorizationEndpoint != nil && metadata.TokenEndpoint != nil {
			return &StreamableHTTPOAuthDiscovery{
				ScopesSupported: normalizeScopes(metadata.ScopesSupported),
			}, nil
		}
	}

	_ = lastErr
	return nil, nil
}

// normalizeScopes trims, de-duplicates, and drops empty scopes, returning nil
// when nothing remains. It mirrors the reference normalize_scopes.
func normalizeScopes(scopes []string) []string {
	if scopes == nil {
		return nil
	}
	var normalized []string
	seen := map[string]struct{}{}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// discoveryPaths returns the ordered RFC 8414 well-known path candidates for a
// base path. It mirrors the reference discovery_paths exactly.
func discoveryPaths(basePath string) []string {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(basePath, "/"), "/")
	canonical := "/.well-known/oauth-authorization-server"

	if trimmed == "" {
		return []string{canonical}
	}

	var candidates []string
	pushUnique := func(candidate string) {
		for _, existing := range candidates {
			if existing == candidate {
				return
			}
		}
		candidates = append(candidates, candidate)
	}

	pushUnique(canonical + "/" + trimmed)
	pushUnique("/" + trimmed + "/.well-known/oauth-authorization-server")
	pushUnique(canonical)

	return candidates
}
