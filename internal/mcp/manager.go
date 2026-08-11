package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// StartupStatus reports the outcome of starting a single MCP server.
type StartupStatus int

// StartupStatus values.
const (
	// StartupReady means the server initialized and tools were discovered.
	StartupReady StartupStatus = iota
	// StartupFailed means startup errored before the server became ready.
	StartupFailed
)

// ServerStartupResult records the per-server startup outcome surfaced by the
// manager after [NewManager] returns.
type ServerStartupResult struct {
	// ServerName is the MCP server name from config.
	ServerName string
	// Status is the startup outcome.
	Status StartupStatus
	// Err is the failure cause when Status == StartupFailed.
	Err error
}

// ManagerOptions configures a [Manager] at construction time.
type ManagerOptions struct {
	// Factory builds transports per server. When nil, a default factory using
	// the system keyring and the given store mode is used.
	Factory TransportFactory
	// StoreMode selects the OAuth credential backend for streamable HTTP
	// servers. Defaults to Auto.
	StoreMode config.OAuthCredentialsStoreMode
	// FallbackCwd is the working directory used for stdio servers that omit cwd.
	FallbackCwd string
	// ElicitationHandler answers server-initiated elicitation/create requests.
	// When nil, such requests are declined.
	ElicitationHandler ServerRequestHandler
	// ProtocolMode selects the MCP compatibility policy (spec 49 need 1). The
	// zero value is Legacy (2025-06-18); set ProtocolModeV20260728 to offer the
	// 2026-07-28 revision with legacy fallback.
	ProtocolMode ProtocolMode
	// CatalogCache is the process-scoped tool catalog cache (spec 49 need 2/4).
	// Share one instance across manager restarts within a process so reconnects
	// reuse cached tool lists. The zero value causes NewManager to allocate one.
	CatalogCache McpToolCatalogCache
	// EnvLookup overrides environment-variable resolution; nil reads the
	// process environment.
	EnvLookup EnvLookup
	// CodexHome locates the OAuth fallback credentials file; empty resolves it
	// from the environment.
	CodexHome string
}

// Manager owns the set of running MCP server connections, keyed by server name.
// It aggregates tools and resources across servers, routes tool and resource
// calls to the owning server, and exposes the namespaced tool surface used by
// the rest of Codex. A Manager is safe for concurrent use after construction.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*ManagedClient

	catalogCache    McpToolCatalogCache
	prefixToolNames bool
}

// NewManager starts every enabled MCP server in mcpServers concurrently, each
// bounded by its configured startup timeout, and returns the manager alongside
// the per-server startup results. Servers that fail to start are omitted from the
// manager but reported in the results. The context bounds the whole startup; a
// per-server transport that ignores cancellation is still bounded by its
// initialize timeout.
func NewManager(ctx context.Context, mcpServers map[string]config.McpServerConfig, opts ManagerOptions) (*Manager, []ServerStartupResult, error) {
	storeMode := opts.StoreMode
	if storeMode == "" {
		storeMode = config.OAuthCredentialsStoreAuto
	}

	factory := opts.Factory
	if factory == nil {
		store := NewOAuthStore(opts.CodexHome)
		factory = NewDefaultTransportFactory(store, storeMode, nil, opts.EnvLookup)
	}

	// Ensure a process-scoped tool catalog cache (spec 49 need 2/4). A caller
	// (app-server/TUI) may pass one to persist across manager restarts.
	if !opts.CatalogCache.valid() {
		opts.CatalogCache = NewToolCatalogCache()
	}

	type startupResult struct {
		name    string
		managed *ManagedClient
		err     error
	}

	resultsCh := make(chan startupResult, len(mcpServers))
	var wg sync.WaitGroup

	for name, cfg := range mcpServers {
		if !cfg.Enabled {
			continue
		}
		if err := validateServerName(name); err != nil {
			resultsCh <- startupResult{name: name, err: err}
			continue
		}
		wg.Add(1)
		go func(name string, cfg config.McpServerConfig) {
			defer wg.Done()
			managed, err := startServer(ctx, name, cfg, factory, opts)
			resultsCh <- startupResult{name: name, managed: managed, err: err}
		}(name, cfg)
	}

	wg.Wait()
	close(resultsCh)

	manager := &Manager{clients: make(map[string]*ManagedClient), catalogCache: opts.CatalogCache}
	var results []ServerStartupResult
	for res := range resultsCh {
		if res.err != nil {
			results = append(results, ServerStartupResult{ServerName: res.name, Status: StartupFailed, Err: res.err})
			continue
		}
		manager.clients[res.name] = res.managed
		results = append(results, ServerStartupResult{ServerName: res.name, Status: StartupReady})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].ServerName < results[j].ServerName })
	return manager, results, nil
}

// startServer opens a transport, wraps it in a client with the elicitation
// handler, and runs the full per-server startup handshake and tool discovery.
func startServer(ctx context.Context, name string, cfg config.McpServerConfig, factory TransportFactory, opts ManagerOptions) (*ManagedClient, error) {
	transport, err := factory.NewTransport(ctx, name, cfg, opts.FallbackCwd)
	if err != nil {
		return nil, fmt.Errorf("mcp: open transport for server %q: %w", name, err)
	}

	clientOpts := []ClientOption{}
	if opts.ElicitationHandler != nil {
		clientOpts = append(clientOpts, WithServerRequestHandler(opts.ElicitationHandler))
	} else {
		clientOpts = append(clientOpts, WithServerRequestHandler(declineElicitation))
	}
	client := NewClient(transport, clientOpts...)

	// Reserve a fetch ticket before discovery so a concurrent (re)start cannot
	// publish a staler catalog over ours (spec 49 need 2/4).
	cacheCtx, cacheable := opts.CatalogCache.Context(name, cfg)
	var ticket McpToolCatalogFetchTicket
	if cacheable {
		ticket = cacheCtx.BeginFetch()
	}

	filter := ToolFilterFromConfig(cfg)
	managed, err := startManagedClient(ctx, name, client, filter, opts.ProtocolMode, startupTimeoutFor(cfg), toolTimeoutFor(cfg))
	if err != nil {
		return nil, err
	}
	if cacheable {
		cacheCtx.PublishIfNewest(ticket, managed.NamespacedTools())
	}
	return managed, nil
}

// declineElicitation is the default server-request handler: it declines every
// elicitation/create request and rejects any other server-initiated request.
func declineElicitation(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	if method == MethodElicitationCreate {
		result := ElicitationResult{Action: ElicitationActionDecline}
		return json.Marshal(result)
	}
	return nil, fmt.Errorf("mcp: unsupported server request %q", method)
}

// HasServers reports whether any servers are currently connected.
func (m *Manager) HasServers() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients) > 0
}

// ServerNames returns the connected server names in sorted order.
func (m *Manager) ServerNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ServerInfo returns the advertised implementation metadata for a server.
func (m *Manager) ServerInfo(serverName string) (Implementation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[serverName]
	if !ok {
		return Implementation{}, false
	}
	return client.ServerInfo, true
}

// ListAllTools returns every connected server's tools, namespaced for routing.
// Tools are ordered by server name then raw tool name for determinism.
func (m *Manager) ListAllTools() []NamespacedTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []NamespacedTool
	for _, name := range m.sortedServerNamesLocked() {
		out = append(out, m.clients[name].NamespacedTools()...)
	}
	return out
}

// ListAllToolSpecs lowers every connected tool into a Responses API tool spec
// named "mcp__<server>__<tool>". deferred selects deferred-loading specs.
func (m *Manager) ListAllToolSpecs(deferred bool) ([]tools.ToolSpec, error) {
	return ToolSpecs(m.ListAllTools(), deferred)
}

// ListAllToolInfos lowers every connected tool into a [tools.McpToolInfo],
// carrying the server identity needed to route eager tool calls back to the
// owning server. Ordered like ListAllTools (server name, then tool name).
func (m *Manager) ListAllToolInfos() []tools.McpToolInfo {
	all := m.ListAllTools()
	infos := make([]tools.McpToolInfo, 0, len(all))
	for _, nt := range all {
		infos = append(infos, nt.ToolInfo())
	}
	return infos
}

// CallTool routes a tool call to the named server, validating the tool against
// the server's filter. arguments and meta must each be a JSON object or nil.
func (m *Manager) CallTool(ctx context.Context, serverName, toolName string, arguments, meta json.RawMessage) (protocol.CallToolResult, error) {
	client, err := m.clientByName(serverName)
	if err != nil {
		return protocol.CallToolResult{}, err
	}
	result, err := client.CallTool(ctx, toolName, arguments, meta)
	if err != nil {
		return protocol.CallToolResult{}, fmt.Errorf("mcp: tool call failed for %s/%s: %w", serverName, toolName, err)
	}
	return result, nil
}

// CallQualifiedTool routes a tool call addressed by its fully-qualified
// "mcp__<server>__<tool>" name.
func (m *Manager) CallQualifiedTool(ctx context.Context, qualifiedName string, arguments, meta json.RawMessage) (protocol.CallToolResult, error) {
	server, tool, err := ParseFullyQualifiedToolName(qualifiedName)
	if err != nil {
		return protocol.CallToolResult{}, err
	}
	return m.CallTool(ctx, server, tool, arguments, meta)
}

// ListResources returns all resources from the named server, paging through the
// server's resources/list endpoint.
func (m *Manager) ListResources(ctx context.Context, serverName string) ([]protocol.Resource, error) {
	client, err := m.clientByName(serverName)
	if err != nil {
		return nil, err
	}
	resources, err := client.Client.ListAllResources(ctx, client.ToolTimeout)
	if err != nil {
		return nil, fmt.Errorf("mcp: resources/list failed for %q: %w", serverName, err)
	}
	return resources, nil
}

// ListResourceTemplates returns all resource templates from the named server.
func (m *Manager) ListResourceTemplates(ctx context.Context, serverName string) ([]protocol.ResourceTemplate, error) {
	client, err := m.clientByName(serverName)
	if err != nil {
		return nil, err
	}
	templates, err := client.Client.ListAllResourceTemplates(ctx, client.ToolTimeout)
	if err != nil {
		return nil, fmt.Errorf("mcp: resources/templates/list failed for %q: %w", serverName, err)
	}
	return templates, nil
}

// ReadResource reads a resource by URI from the named server.
func (m *Manager) ReadResource(ctx context.Context, serverName, uri string) (ReadResourceResult, error) {
	client, err := m.clientByName(serverName)
	if err != nil {
		return ReadResourceResult{}, err
	}
	result, err := client.Client.ReadResource(ctx, uri, client.ToolTimeout)
	if err != nil {
		return ReadResourceResult{}, fmt.Errorf("mcp: resources/read failed for %q (%s): %w", serverName, uri, err)
	}
	return result, nil
}

// ListAllResources aggregates resources across every connected server, keyed by
// server name. Servers that error are skipped.
func (m *Manager) ListAllResources(ctx context.Context) map[string][]protocol.Resource {
	out := map[string][]protocol.Resource{}
	for _, name := range m.ServerNames() {
		resources, err := m.ListResources(ctx, name)
		if err != nil {
			continue
		}
		out[name] = resources
	}
	return out
}

// Shutdown closes every connected server, terminating stdio processes and HTTP
// streams. After Shutdown the manager holds no clients.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	clients := m.clients
	m.clients = make(map[string]*ManagedClient)
	m.mu.Unlock()
	for _, client := range clients {
		_ = client.Close()
	}
}

// clientByName returns the managed client for a server or an error when unknown.
func (m *Manager) clientByName(serverName string) (*ManagedClient, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[serverName]
	if !ok {
		return nil, fmt.Errorf("mcp: unknown MCP server %q", serverName)
	}
	return client, nil
}

// sortedServerNamesLocked returns connected server names in sorted order. The
// caller must hold at least a read lock.
func (m *Manager) sortedServerNamesLocked() []string {
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
