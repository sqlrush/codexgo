package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/keyring"
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
	// StartupDeferred means the server's tools are served from the cached
	// catalog while its live connection is established in the background
	// (spec 49 need 4 step 3). Its tools are usable; a tool call waits for the
	// connection to land.
	StartupDeferred
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
	// Keyring is the credential store the default OAuth store persists tokens
	// to. Hosts with an OS keyring pass keyring/system.NewDefaultStore(); nil
	// means no system keyring (Auto mode falls back to the credentials file,
	// Keyring mode reports not-available). Ignored when Factory is set.
	Keyring keyring.Store
}

// Manager owns the set of running MCP server connections, keyed by server name.
// It aggregates tools and resources across servers, routes tool and resource
// calls to the owning server, and exposes the namespaced tool surface used by
// the rest of Codex. A Manager is safe for concurrent use after construction.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*ManagedClient
	// configs records the config each connected server was started from, so
	// Reconcile can tell a changed server from an untouched one.
	configs map[string]config.McpServerConfig
	// pending holds the resolution handles of servers still connecting behind a
	// cache-backed placeholder (spec 49 need 4 step 3).
	pending map[string]*pendingStartup
	// closed is set by Shutdown; the manager starts no further servers after it.
	closed bool

	// lifetime bounds background startups and is cancelled by Shutdown, which
	// then waits out background. Held on the struct because it scopes the
	// manager's own lifetime rather than any single call.
	lifetime         context.Context
	cancelBackground context.CancelFunc
	background       sync.WaitGroup

	// factory and opts are retained so Reconcile can start servers the same way
	// NewManager did.
	factory TransportFactory
	opts    ManagerOptions

	catalogCache    McpToolCatalogCache
	prefixToolNames bool
}

// pendingStartup is the resolution handle for one background server startup.
// done is closed once the live connection has either replaced the cache-backed
// placeholder or failed; err is set before done closes. cancel abandons the
// startup when Reconcile supersedes it.
type pendingStartup struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

// NewManager starts every enabled MCP server in mcpServers concurrently, each
// bounded by its configured startup timeout, and returns the manager alongside
// the per-server startup results. Servers that fail to start are omitted from the
// manager but reported in the results.
//
// A server with a fresh cached tool catalog does not block startup at all: its
// tools are exposed immediately from the cache and the live connection is
// established in the background (StartupDeferred, spec 49 need 4 step 3).
//
// The context bounds the whole startup and the background connections that
// outlive it; a per-server transport that ignores cancellation is still bounded
// by its initialize timeout. Shutdown cancels any background startup still
// running.
func NewManager(ctx context.Context, mcpServers map[string]config.McpServerConfig, opts ManagerOptions) (*Manager, []ServerStartupResult, error) {
	storeMode := opts.StoreMode
	if storeMode == "" {
		storeMode = config.OAuthCredentialsStoreAuto
	}

	factory := opts.Factory
	if factory == nil {
		store := NewOAuthStore(opts.Keyring, opts.CodexHome)
		factory = NewDefaultTransportFactory(store, storeMode, nil, opts.EnvLookup)
	}

	// Ensure a process-scoped tool catalog cache (spec 49 need 2/4). A caller
	// (app-server/TUI) may pass one to persist across manager restarts.
	if !opts.CatalogCache.valid() {
		opts.CatalogCache = NewToolCatalogCache()
	}

	manager := &Manager{
		clients:      make(map[string]*ManagedClient),
		configs:      make(map[string]config.McpServerConfig),
		pending:      make(map[string]*pendingStartup),
		factory:      factory,
		opts:         opts,
		catalogCache: opts.CatalogCache,
	}
	bgCtx, cancelBackground := context.WithCancel(ctx)
	manager.lifetime = bgCtx
	manager.cancelBackground = cancelBackground

	type startupResult struct {
		name     string
		managed  *ManagedClient
		deferred bool
		err      error
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
		// spec 49 need 4 step 3: a fresh cached catalog makes this server's tools
		// available at once, so a slow connect never delays the assembly.
		if cached := freshCachedCatalog(opts.CatalogCache, name, cfg); cached != nil {
			manager.startInBackground(bgCtx, name, cfg, factory, opts, cached)
			resultsCh <- startupResult{name: name, deferred: true}
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

	var results []ServerStartupResult
	for res := range resultsCh {
		switch {
		case res.err != nil:
			results = append(results, ServerStartupResult{ServerName: res.name, Status: StartupFailed, Err: res.err})
		case res.deferred:
			results = append(results, ServerStartupResult{ServerName: res.name, Status: StartupDeferred})
		default:
			manager.install(res.name, mcpServers[res.name], res.managed)
			results = append(results, ServerStartupResult{ServerName: res.name, Status: StartupReady})
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].ServerName < results[j].ServerName })
	return manager, results, nil
}

// install records a fully-started client and the config it came from. It takes
// the lock because background startups may be publishing concurrently.
func (m *Manager) install(name string, cfg config.McpServerConfig, client *ManagedClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[name] = client
	m.configs[name] = cfg
}

// freshCachedCatalog returns a fresh cached tool catalog for a server, or nil
// when the transport is not cacheable (non-stdio, remote-sourced env var) or no
// fresh snapshot exists.
func freshCachedCatalog(cache McpToolCatalogCache, serverName string, cfg config.McpServerConfig) []NamespacedTool {
	cacheCtx, ok := cache.Context(serverName, cfg)
	if !ok {
		return nil
	}
	return cacheCtx.CurrentTools()
}

// startInBackground publishes a cache-backed placeholder immediately and resolves
// the live connection on a background goroutine (spec 49 need 4 step 3). Readers
// of the tool surface (ListAllTools and friends) see the cached catalog right
// away; tool calls wait on the pending handle for the real connection.
func (m *Manager) startInBackground(
	ctx context.Context,
	name string,
	cfg config.McpServerConfig,
	factory TransportFactory,
	opts ManagerOptions,
	cached []NamespacedTool,
) {
	startCtx, cancel := context.WithCancel(ctx)
	pending := &pendingStartup{done: make(chan struct{}), cancel: cancel}
	m.mu.Lock()
	m.clients[name] = cachedManagedClient(name, cfg, cached)
	m.configs[name] = cfg
	m.pending[name] = pending
	m.mu.Unlock()

	m.background.Add(1)
	go func() {
		defer m.background.Done()
		defer cancel()
		managed, err := startServer(startCtx, name, cfg, factory, opts)
		m.resolvePending(name, pending, managed, err)
	}()
}

// cachedManagedClient builds a tools-only client from a cached catalog. It holds
// no transport: the manager routes calls only after the live client replaces it.
// The current filter is re-applied because the cache identity does not cover the
// enabled/disabled tool lists — a tool disabled since caching stays hidden.
func cachedManagedClient(name string, cfg config.McpServerConfig, cached []NamespacedTool) *ManagedClient {
	filter := ToolFilterFromConfig(cfg)
	discovered := make([]protocol.Tool, 0, len(cached))
	for _, nt := range cached {
		discovered = append(discovered, nt.Tool)
	}
	return &ManagedClient{
		ServerName:  name,
		Tools:       filterTools(discovered, filter),
		ToolTimeout: toolTimeoutFor(cfg),
		filter:      filter,
	}
}

// resolvePending swaps the placeholder for the live client — or drops the server
// when its startup failed, so the tool surface stops advertising tools that
// cannot be called (the same contract as a synchronous startup failure) — and
// then wakes every waiting tool call.
//
// A startup that is no longer this server's current one (Shutdown or Reconcile
// superseded it) installs nothing and closes whatever it opened.
func (m *Manager) resolvePending(name string, pending *pendingStartup, managed *ManagedClient, err error) {
	m.mu.Lock()
	current := !m.closed && m.pending[name] == pending
	if current {
		if err != nil {
			delete(m.clients, name)
			delete(m.configs, name)
		} else {
			m.clients[name] = managed
		}
		delete(m.pending, name)
	}
	m.mu.Unlock()

	if !current && managed != nil {
		// Superseded: close the connection we just opened so the stdio process
		// does not outlive its owner.
		_ = managed.Close()
	}
	pending.err = err
	close(pending.done)
}

// ErrManagerClosed is returned by Reconcile after Shutdown.
var ErrManagerClosed = errors.New("mcp: manager is shut down")

// Reconcile brings the running server set in line with servers (spec 49 need 4
// step 4): newly-configured servers start, removed servers close, and servers
// whose config changed reconnect. A server whose config is unchanged keeps its
// existing connection — its in-flight calls are untouched, which is the whole
// point of reconciling instead of restarting the manager.
//
// The returned results cover only the servers that were started (or attempted);
// unchanged and removed servers produce no result. Disabled servers count as
// removed.
func (m *Manager) Reconcile(ctx context.Context, servers map[string]config.McpServerConfig) ([]ServerStartupResult, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrManagerClosed
	}
	running := make(map[string]config.McpServerConfig, len(m.configs))
	for name, cfg := range m.configs {
		running[name] = cfg
	}
	m.mu.Unlock()

	var toStart []string   // not running yet
	var toReplace []string // running with a changed config
	var toStop []string
	for name, cfg := range servers {
		if !cfg.Enabled {
			continue
		}
		current, isRunning := running[name]
		switch {
		case !isRunning:
			toStart = append(toStart, name)
		case !reflect.DeepEqual(current, cfg):
			// Changed: reconnect, swapping the old connection out only once the
			// new one is up, so the tool surface is never silently emptied.
			toReplace = append(toReplace, name)
		}
	}
	for name := range running {
		if cfg, keep := servers[name]; !keep || !cfg.Enabled {
			toStop = append(toStop, name)
		}
	}

	for _, name := range toStop {
		m.stopServer(name)
	}

	sort.Strings(toStart)
	sort.Strings(toReplace)
	results := make([]ServerStartupResult, 0, len(toStart)+len(toReplace))
	for _, name := range toStart {
		cfg := servers[name]
		if err := validateServerName(name); err != nil {
			results = append(results, ServerStartupResult{ServerName: name, Status: StartupFailed, Err: err})
			continue
		}
		// A newly-added server with a fresh cached catalog starts non-blocking,
		// exactly as it would at construction (step 3).
		if cached := freshCachedCatalog(m.opts.CatalogCache, name, cfg); cached != nil {
			m.startInBackground(m.lifetime, name, cfg, m.factory, m.opts, cached)
			results = append(results, ServerStartupResult{ServerName: name, Status: StartupDeferred})
			continue
		}
		results = append(results, m.startAndInstall(ctx, name, cfg))
	}
	// Reconnects run after the additions so a changed server keeps serving its
	// old connection for as short a window as the new one needs.
	for _, name := range toReplace {
		results = append(results, m.startAndInstall(ctx, name, servers[name]))
	}
	return results, nil
}

// startAndInstall starts one server and installs it, closing whatever connection
// it displaced.
func (m *Manager) startAndInstall(ctx context.Context, name string, cfg config.McpServerConfig) ServerStartupResult {
	managed, err := startServer(ctx, name, cfg, m.factory, m.opts)
	if err != nil {
		return ServerStartupResult{ServerName: name, Status: StartupFailed, Err: err}
	}
	if replaced := m.replace(name, cfg, managed); replaced != nil {
		_ = replaced.Close()
	}
	return ServerStartupResult{ServerName: name, Status: StartupReady}
}

// stopServer removes a server, abandoning any background startup still running
// for it, and closes its connection.
func (m *Manager) stopServer(name string) {
	m.mu.Lock()
	client := m.clients[name]
	pending := m.pending[name]
	delete(m.clients, name)
	delete(m.configs, name)
	delete(m.pending, name)
	m.mu.Unlock()

	if pending != nil {
		// The goroutine sees it is no longer current and closes what it opened.
		pending.cancel()
	}
	if client != nil {
		_ = client.Close()
	}
}

// replace installs a freshly-started client and returns the connection it
// displaced (nil when there was none) for the caller to close. Any background
// startup for that server is abandoned.
func (m *Manager) replace(name string, cfg config.McpServerConfig, client *ManagedClient) *ManagedClient {
	m.mu.Lock()
	previous := m.clients[name]
	pending := m.pending[name]
	m.clients[name] = client
	m.configs[name] = cfg
	delete(m.pending, name)
	closed := m.closed
	m.mu.Unlock()

	if pending != nil {
		pending.cancel()
	}
	if closed {
		// Shutdown ran while this server was connecting: do not leak it.
		return client
	}
	if previous != nil && previous.LiveConnection() {
		return previous
	}
	return nil
}

// startServer opens a transport, wraps it in a client with the elicitation
// handler, and runs the full per-server startup handshake and tool discovery.
// Per-server startup retry (spec 49 need 4 step 2): a transient failure (dead
// process, slow first connect) no longer means "failed forever". Bounded so a
// genuinely-broken server does not stall startup.
const (
	maxStartupAttempts = 3
	startupRetryBase   = 250 * time.Millisecond
	startupRetryCap    = 2 * time.Second
)

func startServer(ctx context.Context, name string, cfg config.McpServerConfig, factory TransportFactory, opts ManagerOptions) (*ManagedClient, error) {
	// Reserve a fetch ticket before discovery so a concurrent (re)start cannot
	// publish a staler catalog over ours (spec 49 need 2/4).
	cacheCtx, cacheable := opts.CatalogCache.Context(name, cfg)
	var ticket McpToolCatalogFetchTicket
	if cacheable {
		ticket = cacheCtx.BeginFetch()
	}

	var managed *ManagedClient
	var err error
	for attempt := 0; attempt < maxStartupAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(startupBackoff(attempt)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		managed, err = attemptStartServer(ctx, name, cfg, factory, opts)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	if cacheable {
		cacheCtx.PublishIfNewest(ticket, managed.NamespacedTools())
	}
	return managed, nil
}

// attemptStartServer runs one connect+handshake+discovery attempt. Each attempt
// opens a fresh transport so a dead stdio process from a prior try is replaced.
func attemptStartServer(ctx context.Context, name string, cfg config.McpServerConfig, factory TransportFactory, opts ManagerOptions) (*ManagedClient, error) {
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

	filter := ToolFilterFromConfig(cfg)
	return startManagedClient(ctx, name, client, filter, opts.ProtocolMode, startupTimeoutFor(cfg), toolTimeoutFor(cfg))
}

// startupBackoff is an exponential backoff (250ms, 500ms, …) capped at 2s.
func startupBackoff(attempt int) time.Duration {
	d := startupRetryBase << (attempt - 1)
	if d <= 0 || d > startupRetryCap {
		return startupRetryCap
	}
	return d
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

// ServerInfo returns the advertised implementation metadata for a server. A
// server still connecting behind a cached catalog reports zero-value metadata
// until its live connection lands (spec 49 need 4 step 3).
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
	client, err := m.clientByName(ctx, serverName)
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
	client, err := m.clientByName(ctx, serverName)
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
	client, err := m.clientByName(ctx, serverName)
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
	client, err := m.clientByName(ctx, serverName)
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
// streams. Background startups still connecting are cancelled and waited out, so
// a connection that lands during the race is closed too (spec 49 need 4 step 3).
// After Shutdown the manager holds no clients.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closed = true
	clients := m.clients
	cancel := m.cancelBackground
	m.clients = make(map[string]*ManagedClient)
	m.configs = make(map[string]config.McpServerConfig)
	m.pending = make(map[string]*pendingStartup)
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.background.Wait()

	for _, client := range clients {
		_ = client.Close()
	}
}

// clientByName returns the live managed client for a server, or an error when
// the server is unknown. A server still resolving behind a cache-backed
// placeholder (spec 49 need 4 step 3) is waited out rather than dispatched to:
// the call blocks until its connection lands, its startup fails, or ctx expires.
func (m *Manager) clientByName(ctx context.Context, serverName string) (*ManagedClient, error) {
	client, pending, err := m.lookup(serverName)
	if err != nil {
		return nil, err
	}
	if client.LiveConnection() {
		return client, nil
	}
	if pending == nil {
		return nil, fmt.Errorf("mcp: MCP server %q has no live connection", serverName)
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("mcp: waiting for MCP server %q to connect: %w", serverName, ctx.Err())
	case <-pending.done:
	}
	if pending.err != nil {
		return nil, fmt.Errorf("mcp: MCP server %q failed to start: %w", serverName, pending.err)
	}

	client, _, err = m.lookup(serverName)
	if err != nil {
		return nil, err
	}
	if !client.LiveConnection() {
		return nil, fmt.Errorf("mcp: MCP server %q has no live connection", serverName)
	}
	return client, nil
}

// lookup returns a server's current client and its pending-startup handle (nil
// when the connection is already live).
func (m *Manager) lookup(serverName string) (*ManagedClient, *pendingStartup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[serverName]
	if !ok {
		return nil, nil, fmt.Errorf("mcp: unknown MCP server %q", serverName)
	}
	return client, m.pending[serverName], nil
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
