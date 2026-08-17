package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/config"
)

// openFactory records the transports it hands out and never blocks.
func openFactory(respond responder) *gatedFactory {
	f := newGatedFactory(respond)
	close(f.release)
	return f
}

func transportClosed(t *fakeTransport) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// TestReconcileStartsNewServer covers spec 49 need 4 step 4: a server added to
// the config starts, while the untouched server keeps its existing connection.
func TestReconcileStartsNewServer(t *testing.T) {
	t.Parallel()
	factory := openFactory(scriptedServer())
	before := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	mgr, _, err := NewManager(context.Background(), before, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()
	alpha, _, _ := mgr.lookup("alpha")

	after := map[string]config.McpServerConfig{
		"alpha": stdioServerConfig(),
		"beta":  stdioServerConfig(),
	}
	results, err := mgr.Reconcile(context.Background(), after)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 1 || results[0].ServerName != "beta" || results[0].Status != StartupReady {
		t.Fatalf("results = %+v, want beta ready", results)
	}
	if names := mgr.ServerNames(); len(names) != 2 {
		t.Fatalf("ServerNames = %v, want alpha and beta", names)
	}
	if current, _, _ := mgr.lookup("alpha"); current != alpha {
		t.Fatal("untouched server was reconnected; its session should be left alone")
	}
}

// TestReconcileStopsRemovedServer: dropping a server from the config closes its
// connection so the stdio process does not linger.
func TestReconcileStopsRemovedServer(t *testing.T) {
	t.Parallel()
	factory := openFactory(scriptedServer())
	mgr, _, err := NewManager(context.Background(),
		map[string]config.McpServerConfig{"alpha": stdioServerConfig()},
		ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	if _, err := mgr.Reconcile(context.Background(), map[string]config.McpServerConfig{}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if mgr.HasServers() {
		t.Fatal("removed server still connected")
	}
	for _, transport := range factory.transports() {
		if !transportClosed(transport) {
			t.Fatal("removed server's transport was left open")
		}
	}
}

// TestReconcileReconnectsChangedServer: a config change reconnects only that
// server — a fresh connection replaces the old one, which is closed.
func TestReconcileReconnectsChangedServer(t *testing.T) {
	t.Parallel()
	factory := openFactory(scriptedServer())
	mgr, _, err := NewManager(context.Background(),
		map[string]config.McpServerConfig{"alpha": stdioServerConfig()},
		ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()
	old, _, _ := mgr.lookup("alpha")

	changed := stdioServerConfig()
	changed.Transport.Args = []string{"--verbose"}
	results, err := mgr.Reconcile(context.Background(), map[string]config.McpServerConfig{"alpha": changed})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 1 || results[0].Status != StartupReady {
		t.Fatalf("results = %+v, want alpha ready", results)
	}
	current, _, _ := mgr.lookup("alpha")
	if current == old {
		t.Fatal("changed server kept its stale connection")
	}
	if !transportClosed(factory.transports()[0]) {
		t.Fatal("the replaced connection was not closed")
	}
}

// TestReconcileNoChangeIsNoOp: reconciling the same config touches nothing.
func TestReconcileNoChangeIsNoOp(t *testing.T) {
	t.Parallel()
	factory := openFactory(scriptedServer())
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	mgr, _, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()
	before, _, _ := mgr.lookup("alpha")

	results, err := mgr.Reconcile(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none for an unchanged config", results)
	}
	if after, _, _ := mgr.lookup("alpha"); after != before {
		t.Fatal("unchanged server was reconnected")
	}
	if calls := factory.calls.Load(); calls != 1 {
		t.Fatalf("transport opened %d times, want 1 (no reconnect)", calls)
	}
}

// TestReconcileSupersedesPendingStartup: removing a server whose background
// startup is still in flight must not let that startup reinstall it later.
func TestReconcileSupersedesPendingStartup(t *testing.T) {
	t.Parallel()
	cache := NewToolCatalogCache()
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	warmCatalogCache(t, cache, cfg)

	factory := newGatedFactory(scriptedServer()) // alpha is stuck connecting
	mgr, results, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory, CatalogCache: cache})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()
	if results[0].Status != StartupDeferred {
		t.Fatalf("expected a deferred startup, got %+v", results)
	}

	if _, err := mgr.Reconcile(context.Background(), map[string]config.McpServerConfig{}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if mgr.HasServers() {
		t.Fatal("removed server still present after Reconcile")
	}

	close(factory.release) // the superseded startup may now finish
	time.Sleep(50 * time.Millisecond)
	if mgr.HasServers() {
		t.Fatal("a superseded background startup reinstalled its server")
	}
}

// TestReconcileAfterShutdownFails: a shut-down manager refuses to start servers
// rather than silently resurrecting itself.
func TestReconcileAfterShutdownFails(t *testing.T) {
	t.Parallel()
	factory := openFactory(scriptedServer())
	mgr, _, err := NewManager(context.Background(),
		map[string]config.McpServerConfig{"alpha": stdioServerConfig()},
		ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.Shutdown()

	if _, err := mgr.Reconcile(context.Background(),
		map[string]config.McpServerConfig{"alpha": stdioServerConfig()}); err == nil {
		t.Fatal("expected Reconcile to fail on a shut-down manager")
	}
}

// TestReconcileReportsFailedServer: a new server that cannot start is reported
// failed and left out, like a failed startup at construction.
func TestReconcileReportsFailedServer(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}
	mgr, _, err := NewManager(context.Background(),
		map[string]config.McpServerConfig{"alpha": stdioServerConfig()},
		ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	// "beta" has no scripted server, so its transport cannot be opened.
	results, err := mgr.Reconcile(context.Background(), map[string]config.McpServerConfig{
		"alpha": stdioServerConfig(),
		"beta":  stdioServerConfig(),
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) != 1 || results[0].ServerName != "beta" || results[0].Status != StartupFailed {
		t.Fatalf("results = %+v, want beta failed", results)
	}
	if names := mgr.ServerNames(); len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("ServerNames = %v, want only alpha", names)
	}
}
