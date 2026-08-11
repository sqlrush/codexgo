package mcp

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func stdioCfg(cmd string) config.McpServerConfig {
	return config.McpServerConfig{
		Transport: config.McpServerTransportConfig{Kind: config.McpTransportStdio, Command: cmd},
	}
}

func toolsFor(names ...string) []NamespacedTool {
	out := make([]NamespacedTool, 0, len(names))
	for _, n := range names {
		out = append(out, NamespacedTool{ServerName: "s", Tool: protocol.Tool{Name: n}})
	}
	return out
}

// withFrozenClock runs fn with nowFunc pinned; restores on return.
func withFrozenClock(t *testing.T, at time.Time, fn func(advance func(time.Duration))) {
	t.Helper()
	orig := nowFunc
	cur := at
	nowFunc = func() time.Time { return cur }
	t.Cleanup(func() { nowFunc = orig })
	fn(func(d time.Duration) { cur = cur.Add(d) })
}

func TestPublishAndCurrentTools(t *testing.T) {
	withFrozenClock(t, time.Unix(1000, 0), func(advance func(time.Duration)) {
		c := NewToolCatalogCache()
		cx, ok := c.Context("srv", stdioCfg("bin"))
		if !ok {
			t.Fatal("stdio config should be cacheable")
		}
		if cx.HasTools() {
			t.Fatal("empty entry reports tools")
		}
		cx.PublishIfNewest(cx.BeginFetch(), toolsFor("a", "b"))
		got := cx.CurrentTools()
		if len(got) != 2 || got[0].Tool.Name != "a" {
			t.Fatalf("current tools = %+v", got)
		}

		// TTL expiry: after 30min+ the snapshot is stale.
		advance(toolCatalogCacheTTL + time.Second)
		if cx.CurrentTools() != nil {
			t.Fatal("snapshot should be stale after TTL")
		}
	})
}

func TestPublishIfNewestDropsStale(t *testing.T) {
	c := NewToolCatalogCache()
	cx, _ := c.Context("srv", stdioCfg("bin"))
	t1 := cx.BeginFetch()
	t2 := cx.BeginFetch() // newer

	cx.PublishIfNewest(t2, toolsFor("new"))
	cx.PublishIfNewest(t1, toolsFor("old")) // stale, must be dropped

	got := cx.CurrentTools()
	if len(got) != 1 || got[0].Tool.Name != "new" {
		t.Fatalf("stale publish overwrote newest: %+v", got)
	}
}

func TestDisableDropsAndBlocks(t *testing.T) {
	c := NewToolCatalogCache()
	cx, _ := c.Context("srv", stdioCfg("bin"))
	cx.PublishIfNewest(cx.BeginFetch(), toolsFor("a"))
	cx.Disable()
	if cx.HasTools() {
		t.Fatal("disable should drop snapshot")
	}
	cx.PublishIfNewest(cx.BeginFetch(), toolsFor("b"))
	if cx.HasTools() {
		t.Fatal("disabled entry must not accept publishes")
	}
}

func TestOptionalStartupDeadline(t *testing.T) {
	withFrozenClock(t, time.Unix(2000, 0), func(advance func(time.Duration)) {
		c := NewToolCatalogCache()
		cx, _ := c.Context("srv", stdioCfg("bin"))
		def := nowFunc().Add(5 * time.Second)

		// No snapshot: first default is pinned and returned.
		if got := cx.OptionalStartupDeadline(def); !got.Equal(def) {
			t.Fatalf("first deadline = %v, want %v", got, def)
		}
		later := def.Add(10 * time.Second)
		if got := cx.OptionalStartupDeadline(later); !got.Equal(def) {
			t.Fatalf("deadline not pinned: got %v, want %v", got, def)
		}

		// Fresh snapshot: caller may use its own default (not pinned).
		cx.PublishIfNewest(cx.BeginFetch(), toolsFor("a"))
		if got := cx.OptionalStartupDeadline(later); !got.Equal(later) {
			t.Fatalf("with fresh snapshot deadline = %v, want %v", got, later)
		}
	})
}

func TestHTTPTransportNotCacheable(t *testing.T) {
	c := NewToolCatalogCache()
	httpCfg := config.McpServerConfig{
		Transport: config.McpServerTransportConfig{Kind: config.McpTransportStreamableHTTP, URL: "http://x"},
	}
	if _, ok := c.Context("srv", httpCfg); ok {
		t.Fatal("HTTP transport must not be cacheable")
	}
}

func TestRemoteEnvVarNotCacheable(t *testing.T) {
	c := NewToolCatalogCache()
	remote := "remote"
	cfg := stdioCfg("bin")
	cfg.Transport.EnvVars = []config.McpServerEnvVar{{Name: "TOKEN", Source: &remote}}
	if _, ok := c.Context("srv", cfg); ok {
		t.Fatal("remote-sourced env var must disable caching")
	}
}

func TestDistinctIdentitiesAndLRUEviction(t *testing.T) {
	c := NewToolCatalogCache()
	// Different commands → different identities → independent entries.
	a, _ := c.Context("srv", stdioCfg("binA"))
	b, _ := c.Context("srv", stdioCfg("binB"))
	a.PublishIfNewest(a.BeginFetch(), toolsFor("a"))
	b.PublishIfNewest(b.BeginFetch(), toolsFor("b"))
	if a.CurrentTools()[0].Tool.Name != "a" || b.CurrentTools()[0].Tool.Name != "b" {
		t.Fatal("distinct configs must not share a catalog")
	}

	// Same identity resolves to same entry.
	a2, _ := c.Context("srv", stdioCfg("binA"))
	if !a2.HasTools() {
		t.Fatal("same identity should hit the existing entry")
	}

	// Overflow capacity → oldest evicted (LRU bound holds).
	for i := 0; i < toolCatalogCacheCapacity+5; i++ {
		cx, _ := c.Context("srv", stdioCfg(fmt.Sprintf("overflow-%d", i)))
		cx.PublishIfNewest(cx.BeginFetch(), toolsFor("x"))
	}
	c.mu.Lock()
	n := c.entries.ll.Len()
	c.mu.Unlock()
	if n > toolCatalogCacheCapacity {
		t.Fatalf("LRU exceeded capacity: %d > %d", n, toolCatalogCacheCapacity)
	}
}

func TestConcurrentPublishSafe(t *testing.T) {
	c := NewToolCatalogCache()
	cx, _ := c.Context("srv", stdioCfg("bin"))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cx.PublishIfNewest(cx.BeginFetch(), toolsFor("t"))
			_ = cx.CurrentTools()
		}()
	}
	wg.Wait()
	if !cx.HasTools() {
		t.Fatal("concurrent publishes lost the snapshot")
	}
}
