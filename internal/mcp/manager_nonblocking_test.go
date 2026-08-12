package mcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
)

// gatedFactory blocks every NewTransport call until release is closed, modeling
// a server that is slow to come up. It records the transports it handed out so a
// test can assert they were closed.
type gatedFactory struct {
	release chan struct{}
	respond responder
	fail    error

	calls atomic.Int64

	mu       sync.Mutex
	produced []*fakeTransport
}

func newGatedFactory(respond responder) *gatedFactory {
	return &gatedFactory{release: make(chan struct{}), respond: respond}
}

func (f *gatedFactory) NewTransport(ctx context.Context, _ string, _ config.McpServerConfig, _ string) (Transport, error) {
	f.calls.Add(1)
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if f.fail != nil {
		return nil, f.fail
	}
	transport := newFakeTransport(f.respond)
	f.mu.Lock()
	f.produced = append(f.produced, transport)
	f.mu.Unlock()
	return transport, nil
}

func (f *gatedFactory) transports() []*fakeTransport {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*fakeTransport, len(f.produced))
	copy(out, f.produced)
	return out
}

// warmCatalogCache runs one successful manager startup so the shared cache holds
// a fresh catalog for "alpha", then shuts that manager down.
func warmCatalogCache(t *testing.T, cache McpToolCatalogCache, cfg map[string]config.McpServerConfig) {
	t.Helper()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}
	mgr, results, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory, CatalogCache: cache})
	if err != nil {
		t.Fatalf("warm NewManager: %v", err)
	}
	if len(results) != 1 || results[0].Status != StartupReady {
		t.Fatalf("warm startup results = %+v", results)
	}
	mgr.Shutdown()
}

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestNonBlockingStartupServesCachedCatalog covers spec 49 need 4 step 3: with a
// fresh cached catalog, a slow server no longer blocks assembly — its tools are
// exposed immediately and the live connection lands in the background.
func TestNonBlockingStartupServesCachedCatalog(t *testing.T) {
	t.Parallel()
	cache := NewToolCatalogCache()
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	warmCatalogCache(t, cache, cfg)

	factory := newGatedFactory(scriptedServer()) // still gated: connect cannot finish
	start := time.Now()
	mgr, results, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory, CatalogCache: cache})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("assembly blocked on the slow server: %v", elapsed)
	}
	if len(results) != 1 || results[0].Status != StartupDeferred {
		t.Fatalf("startup results = %+v, want one StartupDeferred", results)
	}
	if tools := mgr.ListAllTools(); len(tools) != 2 {
		t.Fatalf("cached tools exposed = %d, want 2 (echo, secret)", len(tools))
	}

	close(factory.release) // let the live connection come up
	waitFor(t, 3*time.Second, "live connection to replace the cached placeholder", func() bool {
		client, _, err := mgr.lookup("alpha")
		return err == nil && client.LiveConnection()
	})
	if tools := mgr.ListAllTools(); len(tools) != 2 {
		t.Fatalf("tools after refresh = %d, want 2", len(tools))
	}
}

// TestNonBlockingStartupCallToolWaitsForLiveConnection: a tool call issued while
// the placeholder is still in place waits for the live connection instead of
// dispatching into a nil transport.
func TestNonBlockingStartupCallToolWaitsForLiveConnection(t *testing.T) {
	t.Parallel()
	cache := NewToolCatalogCache()
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	warmCatalogCache(t, cache, cfg)

	factory := newGatedFactory(scriptedServer())
	mgr, _, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory, CatalogCache: cache})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(factory.release)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := mgr.CallTool(ctx, "alpha", "echo", nil, nil)
	if err != nil {
		t.Fatalf("CallTool while pending: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected tool result content once the live connection landed")
	}
}

// TestNonBlockingStartupCallToolHonorsContext: if the live connection never
// arrives, a waiting call fails with the caller's deadline rather than hanging.
func TestNonBlockingStartupCallToolHonorsContext(t *testing.T) {
	t.Parallel()
	cache := NewToolCatalogCache()
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	warmCatalogCache(t, cache, cfg)

	factory := newGatedFactory(scriptedServer()) // never released
	mgr, _, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory, CatalogCache: cache})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := mgr.CallTool(ctx, "alpha", "echo", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CallTool err = %v, want context deadline", err)
	}
}

// TestNonBlockingStartupFailureDropsServer: when the background connection never
// comes up, the server stops advertising tools it cannot serve — same contract as
// a synchronous startup failure.
func TestNonBlockingStartupFailureDropsServer(t *testing.T) {
	t.Parallel()
	cache := NewToolCatalogCache()
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	warmCatalogCache(t, cache, cfg)

	factory := newGatedFactory(scriptedServer())
	factory.fail = errors.New("connect refused")
	close(factory.release) // fail fast on every attempt

	mgr, results, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory, CatalogCache: cache})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()
	if len(results) != 1 || results[0].Status != StartupDeferred {
		t.Fatalf("startup results = %+v, want StartupDeferred", results)
	}

	waitFor(t, 5*time.Second, "failed server to be dropped", func() bool {
		return !mgr.HasServers()
	})
	if _, err := mgr.CallTool(context.Background(), "alpha", "echo", nil, nil); err == nil {
		t.Fatal("expected an error calling a server whose startup failed")
	}
}

// TestShutdownDuringPendingStartup: shutting down mid-startup cancels the
// background connect, returns promptly, and closes any connection that landed in
// the race window (no leaked stdio process).
func TestShutdownDuringPendingStartup(t *testing.T) {
	t.Parallel()
	cache := NewToolCatalogCache()
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	warmCatalogCache(t, cache, cfg)

	factory := newGatedFactory(scriptedServer()) // never released: connect is stuck
	mgr, _, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory, CatalogCache: cache})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	done := make(chan struct{})
	go func() { mgr.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown blocked on a pending background startup")
	}
	if mgr.HasServers() {
		t.Fatal("expected no servers after Shutdown")
	}
	for _, transport := range factory.transports() {
		transport.mu.Lock()
		closed := transport.closed
		transport.mu.Unlock()
		if !closed {
			t.Fatal("a transport opened during the shutdown race was left open")
		}
	}
}

// TestCachedPlaceholderAppliesCurrentToolFilter: the cache identity does not
// cover the enabled/disabled tool lists, so a placeholder must re-apply the
// current filter — a tool disabled since the catalog was cached stays hidden.
func TestCachedPlaceholderAppliesCurrentToolFilter(t *testing.T) {
	t.Parallel()
	cache := NewToolCatalogCache()
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	warmCatalogCache(t, cache, cfg)

	disabled := stdioServerConfig()
	disabled.DisabledTools = &[]string{"secret"}
	factory := newGatedFactory(scriptedServer())
	mgr, _, err := NewManager(context.Background(),
		map[string]config.McpServerConfig{"alpha": disabled},
		ManagerOptions{Factory: factory, CatalogCache: cache})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	tools := mgr.ListAllTools()
	if len(tools) != 1 || tools[0].Tool.Name != "echo" {
		t.Fatalf("placeholder tools = %+v, want only echo (secret disabled)", tools)
	}
}
