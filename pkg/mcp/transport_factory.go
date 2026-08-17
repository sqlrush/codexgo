package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sqlrush/codexgo/pkg/config"
)

// dereferenceMap returns the value of a *map or nil.
func dereferenceMap(m *map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	return *m
}

// resolveBearerToken reads the bearer token from the configured env var, mirroring
// the reference resolve_bearer_token: a missing or empty value is an error.
func resolveBearerToken(serverName string, bearerTokenEnvVar *string, lookup EnvLookup) (string, error) {
	if bearerTokenEnvVar == nil {
		return "", nil
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup(*bearerTokenEnvVar)
	if !ok {
		return "", fmt.Errorf("mcp: environment variable %s for MCP server %q is not set", *bearerTokenEnvVar, serverName)
	}
	if value == "" {
		return "", fmt.Errorf("mcp: environment variable %s for MCP server %q is empty", *bearerTokenEnvVar, serverName)
	}
	return value, nil
}

// TransportFactory builds a [Transport] for a server config. The connection
// manager uses it so tests can substitute in-memory transports.
type TransportFactory interface {
	// NewTransport opens a transport for the named server. fallbackCwd is the
	// caller's working directory used when a stdio server config omits cwd.
	NewTransport(ctx context.Context, serverName string, cfg config.McpServerConfig, fallbackCwd string) (Transport, error)
}

// defaultTransportFactory builds real stdio and streamable HTTP transports from
// config, applying OAuth/bearer credentials and header rules.
type defaultTransportFactory struct {
	store      *OAuthStore
	storeMode  config.OAuthCredentialsStoreMode
	httpClient *http.Client
	lookup     EnvLookup
}

// NewDefaultTransportFactory builds the production transport factory. The
// OAuthStore supplies stored tokens for streamable HTTP servers; storeMode
// selects the credential backend. httpClient and lookup may be nil to use
// http.DefaultClient and the process environment.
func NewDefaultTransportFactory(store *OAuthStore, storeMode config.OAuthCredentialsStoreMode, httpClient *http.Client, lookup EnvLookup) TransportFactory {
	return &defaultTransportFactory{
		store:      store,
		storeMode:  storeMode,
		httpClient: httpClient,
		lookup:     lookup,
	}
}

// NewTransport opens the transport described by cfg.
func (f *defaultTransportFactory) NewTransport(ctx context.Context, serverName string, cfg config.McpServerConfig, fallbackCwd string) (Transport, error) {
	switch cfg.Transport.Kind {
	case config.McpTransportStdio:
		return f.newStdioTransport(ctx, cfg, fallbackCwd)
	case config.McpTransportStreamableHTTP:
		return f.newHTTPTransport(serverName, cfg)
	default:
		return nil, fmt.Errorf("mcp: unknown transport kind for server %q", serverName)
	}
}

// newStdioTransport launches a local stdio server, building the child env from
// the allowlist plus the configured env_vars and literal env overrides.
func (f *defaultTransportFactory) newStdioTransport(ctx context.Context, cfg config.McpServerConfig, fallbackCwd string) (Transport, error) {
	transport := cfg.Transport
	env, err := BuildStdioEnv(dereferenceMap(transport.Env), transport.EnvVars, f.lookup)
	if err != nil {
		return nil, err
	}
	cwd := ""
	if transport.Cwd != nil {
		cwd = *transport.Cwd
	}
	command := StdioCommand{
		Program: transport.Command,
		Args:    transport.Args,
		Env:     env,
		Cwd:     cwd,
	}
	return LaunchStdio(ctx, command, fallbackCwd)
}

// newHTTPTransport builds a streamable HTTP transport, resolving the bearer
// token, default headers, and any stored OAuth access token. The credential
// precedence matches the reference create_pending_transport: an env bearer token
// or an explicit Authorization header takes priority; otherwise stored OAuth
// tokens supply a bearer access token.
func (f *defaultTransportFactory) newHTTPTransport(serverName string, cfg config.McpServerConfig) (Transport, error) {
	transport := cfg.Transport
	bearerToken, err := resolveBearerToken(serverName, transport.BearerTokenEnvVar, f.lookup)
	if err != nil {
		return nil, err
	}

	headers := BuildHTTPHeaders(dereferenceMap(transport.HTTPHeaders), dereferenceMap(transport.EnvHTTPHeaders), f.lookup)

	hasAuthHeader := headers.Get("Authorization") != ""
	if bearerToken == "" && !hasAuthHeader && f.store != nil {
		tokens, lerr := f.store.Load(serverName, transport.URL, f.storeMode)
		if lerr == nil && tokens != nil {
			bearerToken = tokens.AccessToken()
		}
	}

	return NewHTTPTransport(HTTPTransportConfig{
		URL:            transport.URL,
		BearerToken:    bearerToken,
		DefaultHeaders: headers,
		HTTPClient:     f.httpClient,
	})
}

// validateServerName enforces the reference server-name pattern
// ^[a-zA-Z0-9_-]+$ so namespaced tool names remain well-formed.
func validateServerName(serverName string) error {
	if serverName == "" {
		return fmt.Errorf("mcp: server name must not be empty")
	}
	for _, r := range serverName {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return fmt.Errorf("mcp: invalid MCP server name %q: must match ^[a-zA-Z0-9_-]+$", serverName)
		}
	}
	return nil
}

// startupTimeoutFor resolves the startup timeout for a server config, falling
// back to the default. The config value is in fractional seconds.
func startupTimeoutFor(cfg config.McpServerConfig) time.Duration {
	if cfg.StartupTimeoutSec != nil {
		return secondsToDuration(*cfg.StartupTimeoutSec)
	}
	return DefaultStartupTimeout
}

// toolTimeoutFor resolves the tool-call timeout for a server config, falling back
// to the default. The config value is in fractional seconds.
func toolTimeoutFor(cfg config.McpServerConfig) time.Duration {
	if cfg.ToolTimeoutSec != nil {
		return secondsToDuration(*cfg.ToolTimeoutSec)
	}
	return DefaultToolTimeout
}

// secondsToDuration converts fractional seconds into a time.Duration.
func secondsToDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}
