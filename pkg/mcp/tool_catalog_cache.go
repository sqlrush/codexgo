package mcp

import (
	"container/list"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
)

// Port of upstream codex-mcp/src/tool_catalog_cache.rs (spec 49 need 2): a
// process-scoped LRU of recent reusable MCP tool catalogs, so a reconnecting or
// restarting server can serve its tool list from cache while the live
// connection re-establishes (non-blocking startup). In-memory only — no disk
// (decision point 3, method a).
const (
	toolCatalogCacheCapacity = 32
	toolCatalogCacheTTL      = 30 * time.Minute
)

// nowFunc is overridable in tests for deterministic TTL assertions; production
// uses the wall clock.
var nowFunc = time.Now

// McpToolCatalogCache is a clonable handle to a shared, process-scoped cache.
// Copies share the same underlying entries (like the Rust Arc<Mutex<..>>).
type McpToolCatalogCache struct {
	mu      *sync.Mutex
	entries *lruCache
}

// NewToolCatalogCache builds an empty cache with the default capacity.
func NewToolCatalogCache() McpToolCatalogCache {
	return McpToolCatalogCache{
		mu:      &sync.Mutex{},
		entries: newLRUCache(toolCatalogCacheCapacity),
	}
}

// valid reports whether the cache has been initialized (vs the zero value).
func (c McpToolCatalogCache) valid() bool { return c.mu != nil && c.entries != nil }

// Context resolves (or creates) the cache entry for a server's stable identity.
// Returns ok=false for transports that cannot be safely shared across
// connections (currently: non-stdio, or stdio with a remote-sourced env var),
// matching upstream's None return.
func (c McpToolCatalogCache) Context(serverName string, cfg config.McpServerConfig) (McpToolCatalogCacheContext, bool) {
	id, ok := newToolCatalogIdentity(serverName, cfg)
	if !ok {
		return McpToolCatalogCacheContext{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries.getOrInsert(id, func() *toolCatalogCacheEntry {
		return &toolCatalogCacheEntry{}
	})
	return McpToolCatalogCacheContext{entry: entry}, true
}

// toolCatalogCacheEntry is the per-server cached state. Guarded by its own mutex
// so distinct servers never contend.
type toolCatalogCacheEntry struct {
	mu                     sync.Mutex
	snapshot               *toolCatalogSnapshot
	optionalStartupSet     bool
	optionalStartup        time.Time
	lastAcceptedGeneration uint64
	disabledByServer       bool

	nextFetchGeneration atomic.Uint64
}

type toolCatalogSnapshot struct {
	tools       []NamespacedTool
	publishedAt time.Time
}

func (s *toolCatalogSnapshot) fresh() bool {
	return nowFunc().Sub(s.publishedAt) <= toolCatalogCacheTTL
}

// McpToolCatalogCacheContext is a handle to one server's cache entry.
type McpToolCatalogCacheContext struct {
	entry *toolCatalogCacheEntry
}

// HasTools reports whether a fresh cached catalog is available.
func (cx McpToolCatalogCacheContext) HasTools() bool {
	return cx.CurrentTools() != nil
}

// CurrentTools returns a copy of the cached catalog when fresh, else nil.
func (cx McpToolCatalogCacheContext) CurrentTools() []NamespacedTool {
	cx.entry.mu.Lock()
	defer cx.entry.mu.Unlock()
	if cx.entry.snapshot == nil || !cx.entry.snapshot.fresh() {
		return nil
	}
	out := make([]NamespacedTool, len(cx.entry.snapshot.tools))
	copy(out, cx.entry.snapshot.tools)
	return out
}

// OptionalStartupDeadline supports non-blocking startup: when a fresh catalog is
// available (or the server disabled caching), the caller may use the default
// deadline; otherwise the entry pins the first-seen default so concurrent
// starts agree on one deadline.
func (cx McpToolCatalogCacheContext) OptionalStartupDeadline(defaultDeadline time.Time) time.Time {
	cx.entry.mu.Lock()
	defer cx.entry.mu.Unlock()
	if cx.entry.disabledByServer ||
		(cx.entry.snapshot != nil && cx.entry.snapshot.fresh()) {
		return defaultDeadline
	}
	if !cx.entry.optionalStartupSet {
		cx.entry.optionalStartup = defaultDeadline
		cx.entry.optionalStartupSet = true
	}
	return cx.entry.optionalStartup
}

// BeginFetch issues a monotonically increasing ticket so only the newest
// in-flight fetch may publish its result.
func (cx McpToolCatalogCacheContext) BeginFetch() McpToolCatalogFetchTicket {
	return McpToolCatalogFetchTicket{generation: cx.entry.nextFetchGeneration.Add(1)}
}

// Disable records that the server opted out of catalog caching and drops any
// snapshot.
func (cx McpToolCatalogCacheContext) Disable() {
	cx.entry.mu.Lock()
	defer cx.entry.mu.Unlock()
	cx.entry.disabledByServer = true
	cx.entry.snapshot = nil
}

// PublishIfNewest stores tools only when this ticket is the newest accepted and
// the server has not disabled caching. Stale fetches are dropped.
func (cx McpToolCatalogCacheContext) PublishIfNewest(ticket McpToolCatalogFetchTicket, tools []NamespacedTool) {
	cx.entry.mu.Lock()
	defer cx.entry.mu.Unlock()
	if cx.entry.disabledByServer || ticket.generation <= cx.entry.lastAcceptedGeneration {
		return
	}
	stored := make([]NamespacedTool, len(tools))
	copy(stored, tools)
	cx.entry.lastAcceptedGeneration = ticket.generation
	cx.entry.optionalStartupSet = false
	cx.entry.snapshot = &toolCatalogSnapshot{tools: stored, publishedAt: nowFunc()}
}

// McpToolCatalogFetchTicket orders concurrent fetches for one entry.
type McpToolCatalogFetchTicket struct {
	generation uint64
}

// toolCatalogIdentity keys the LRU. It is a comparable string composed from the
// server name, resolved environment id, and a fingerprint of the stdio
// transport config — the parts that must match for two connections to safely
// share a catalog.
type toolCatalogIdentity string

func newToolCatalogIdentity(serverName string, cfg config.McpServerConfig) (toolCatalogIdentity, bool) {
	fp, ok := stdioTransportFingerprint(cfg)
	if !ok {
		return "", false
	}
	return toolCatalogIdentity(serverName + "\x00" + cfg.EnvironmentID + "\x00" + fp), true
}

// stdioTransportFingerprint returns a SHA-1 fingerprint of a stdio server's
// launch config, or ok=false for non-stdio transports (HTTP catalogs need a
// canonical resolved-auth identity before they can be shared — upstream parity).
func stdioTransportFingerprint(cfg config.McpServerConfig) (string, bool) {
	t := cfg.Transport
	if t.Kind != config.McpTransportStdio {
		return "", false
	}
	// A remote-sourced env var makes the launch non-deterministic to fingerprint.
	for _, ev := range t.EnvVars {
		if ev.Source != nil && *ev.Source == "remote" {
			return "", false
		}
	}
	payload := struct {
		Command       string
		Args          []string
		Env           *map[string]string
		EnvVars       []config.McpServerEnvVar
		Cwd           *string
		EnvironmentID string
	}{t.Command, t.Args, t.Env, t.EnvVars, t.Cwd, cfg.EnvironmentID}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	sum := sha1.Sum(buf) //nolint:gosec // fingerprint identity, not a security primitive
	return hex.EncodeToString(sum[:]), true
}

// lruCache is a minimal bounded LRU (zero external deps, honoring codexgo's
// cgo-free/minimal-dependency posture).
type lruCache struct {
	capacity int
	ll       *list.List
	items    map[toolCatalogIdentity]*list.Element
}

type lruPair struct {
	key   toolCatalogIdentity
	value *toolCatalogCacheEntry
}

func newLRUCache(capacity int) *lruCache {
	return &lruCache{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[toolCatalogIdentity]*list.Element, capacity),
	}
}

func (c *lruCache) getOrInsert(key toolCatalogIdentity, make func() *toolCatalogCacheEntry) *toolCatalogCacheEntry {
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*lruPair).value
	}
	entry := make()
	el := c.ll.PushFront(&lruPair{key: key, value: entry})
	c.items[key] = el
	if c.ll.Len() > c.capacity {
		c.evictOldest()
	}
	return entry
}

func (c *lruCache) evictOldest() {
	el := c.ll.Back()
	if el == nil {
		return
	}
	c.ll.Remove(el)
	delete(c.items, el.Value.(*lruPair).key)
}
