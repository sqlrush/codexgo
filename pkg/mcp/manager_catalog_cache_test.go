package mcp

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/pkg/config"
)

// TestManagerPopulatesCatalogCache covers spec 49 need 2/4 step 1: a successful
// server start publishes its tool catalog into the shared cache, so a later
// lookup for the same identity serves tools without re-querying.
func TestManagerPopulatesCatalogCache(t *testing.T) {
	t.Parallel()
	cache := NewToolCatalogCache()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}

	mgr, results, err := NewManager(context.Background(), cfg,
		ManagerOptions{Factory: factory, CatalogCache: cache})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()
	if len(results) != 1 || results[0].Status != StartupReady {
		t.Fatalf("startup results = %+v", results)
	}

	cx, ok := cache.Context("alpha", cfg["alpha"])
	if !ok {
		t.Fatal("stdio config should be cacheable")
	}
	cached := cx.CurrentTools()
	if len(cached) == 0 {
		t.Fatal("catalog cache not populated after successful start")
	}
	// The cached catalog must match what the live manager exposes.
	if got := len(mgr.clients["alpha"].NamespacedTools()); got != len(cached) {
		t.Fatalf("cached tools = %d, live tools = %d", len(cached), got)
	}
}

// TestManagerAllocatesCacheWhenAbsent: omitting CatalogCache must not panic; the
// manager allocates its own.
func TestManagerAllocatesCacheWhenAbsent(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}
	mgr, _, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()
	if !mgr.catalogCache.valid() {
		t.Fatal("manager did not allocate a catalog cache")
	}
}
