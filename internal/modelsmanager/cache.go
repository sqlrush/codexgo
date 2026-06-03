package modelsmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// modelsCacheManager manages loading and saving of the models cache on disk.
// Rust: cache::ModelsCacheManager.
type modelsCacheManager struct {
	cachePath string
	cacheTTL  time.Duration
	// now is injectable for tests; defaults to time.Now. Production code uses
	// the wall clock exclusively.
	now func() time.Time
}

// newModelsCacheManager constructs a cache manager for cachePath with the given
// TTL. Rust: ModelsCacheManager::new.
func newModelsCacheManager(cachePath string, cacheTTL time.Duration) *modelsCacheManager {
	return &modelsCacheManager{
		cachePath: cachePath,
		cacheTTL:  cacheTTL,
		now:       time.Now,
	}
}

// loadFresh attempts to load a fresh cache entry, returning (nil, nil) when the
// cache is absent, stale, or for a different client version. Rust: load_fresh.
func (m *modelsCacheManager) loadFresh(expectedVersion string) (*modelsCache, error) {
	cache, err := m.load()
	if err != nil {
		return nil, fmt.Errorf("load models cache: %w", err)
	}
	if cache == nil {
		return nil, nil
	}
	if cache.ClientVersion == nil || *cache.ClientVersion != expectedVersion {
		return nil, nil
	}
	if !cache.isFresh(m.cacheTTL, m.now()) {
		return nil, nil
	}
	return cache, nil
}

// persistCache writes a fresh cache snapshot to disk. Errors are returned for
// the caller to log; Rust logs internally and is infallible at the call site.
// Rust: persist_cache.
func (m *modelsCacheManager) persistCache(models []ModelInfo, etag *string, clientVersion string) error {
	version := clientVersion
	cache := &modelsCache{
		FetchedAt:     m.now().UTC(),
		Etag:          cloneStringPtr(etag),
		ClientVersion: &version,
		Models:        cloneModels(models),
	}
	if err := m.saveInternal(cache); err != nil {
		return fmt.Errorf("write models cache: %w", err)
	}
	return nil
}

// renewCacheTTL updates the cached fetched_at timestamp to now, refreshing the
// TTL window. Returns an error when no cache file exists. Rust: renew_cache_ttl.
func (m *modelsCacheManager) renewCacheTTL() error {
	cache, err := m.load()
	if err != nil {
		return fmt.Errorf("load models cache: %w", err)
	}
	if cache == nil {
		return fmt.Errorf("renew models cache: %w", fs.ErrNotExist)
	}
	cache.FetchedAt = m.now().UTC()
	return m.saveInternal(cache)
}

// load reads and decodes the cache file, returning (nil, nil) when it is absent.
// Rust: load.
func (m *modelsCacheManager) load() (*modelsCache, error) {
	contents, err := os.ReadFile(m.cachePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cache file %q: %w", m.cachePath, err)
	}
	var cache modelsCache
	if err := json.Unmarshal(contents, &cache); err != nil {
		return nil, fmt.Errorf("decode cache file %q: %w", m.cachePath, err)
	}
	return &cache, nil
}

// saveInternal serializes and writes the cache, creating parent directories as
// needed. The JSON is pretty-printed with two-space indentation to match Rust's
// serde_json::to_vec_pretty. Rust: save_internal.
func (m *modelsCacheManager) saveInternal(cache *modelsCache) error {
	if parent := filepath.Dir(m.cachePath); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create cache parent dir %q: %w", parent, err)
		}
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode models cache: %w", err)
	}
	if err := os.WriteFile(m.cachePath, data, 0o644); err != nil {
		return fmt.Errorf("write cache file %q: %w", m.cachePath, err)
	}
	return nil
}

// setTTL overrides the cache TTL. Test-only counterpart to Rust's set_ttl.
func (m *modelsCacheManager) setTTL(ttl time.Duration) {
	m.cacheTTL = ttl
}

// manipulateCacheForTest applies fn to the cached fetched_at timestamp and
// rewrites the cache. Test-only counterpart to Rust's manipulate_cache_for_test.
func (m *modelsCacheManager) manipulateCacheForTest(fn func(*time.Time)) error {
	cache, err := m.load()
	if err != nil {
		return err
	}
	if cache == nil {
		return fmt.Errorf("manipulate models cache: %w", fs.ErrNotExist)
	}
	fn(&cache.FetchedAt)
	return m.saveInternal(cache)
}

// mutateCacheForTest applies fn to the entire cache and rewrites it. Test-only
// counterpart to Rust's mutate_cache_for_test.
func (m *modelsCacheManager) mutateCacheForTest(fn func(*modelsCache)) error {
	cache, err := m.load()
	if err != nil {
		return err
	}
	if cache == nil {
		return fmt.Errorf("mutate models cache: %w", fs.ErrNotExist)
	}
	fn(cache)
	return m.saveInternal(cache)
}

// modelsCache is the serialized snapshot of models and metadata cached on disk.
// Rust: cache::ModelsCache.
type modelsCache struct {
	FetchedAt     time.Time   `json:"fetched_at"`
	Etag          *string     `json:"etag,omitempty"`
	ClientVersion *string     `json:"client_version,omitempty"`
	Models        []ModelInfo `json:"models"`
}

// isFresh reports whether the entry has not exceeded ttl as of now. A zero TTL
// is always stale. Rust: ModelsCache::is_fresh.
func (c *modelsCache) isFresh(ttl time.Duration, now time.Time) bool {
	if ttl <= 0 {
		return false
	}
	age := now.Sub(c.FetchedAt)
	return age <= ttl
}

// cloneModels returns a defensive copy of the supplied models slice so that the
// cache snapshot does not alias caller-owned data.
func cloneModels(models []ModelInfo) []ModelInfo {
	if models == nil {
		return nil
	}
	out := make([]ModelInfo, len(models))
	copy(out, models)
	return out
}
