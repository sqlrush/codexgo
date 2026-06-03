package imageutil

import (
	"container/list"
	"sync"
)

// lruCache is a minimal, concurrency-safe least-recently-used cache.
//
// It is the Go analogue of the upstream `BlockingLruCache`: a fixed-capacity
// LRU map guarded by a mutex. Unlike the Rust version it does not no-op outside
// of a runtime; in Go there is no such concept, so the cache is always active.
//
// Values are stored and returned by the caller's type; this package always
// stores immutable EncodedImage snapshots, so no defensive copying is performed
// here.
type lruCache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List
	items    map[K]*list.Element
}

type lruEntry[K comparable, V any] struct {
	key   K
	value V
}

// newLRUCache creates an LRU cache with the given capacity. A capacity of zero
// or less is clamped to one, mirroring the upstream `NonZeroUsize` guard.
func newLRUCache[K comparable, V any](capacity int) *lruCache[K, V] {
	if capacity < 1 {
		capacity = 1
	}
	return &lruCache[K, V]{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[K]*list.Element, capacity),
	}
}

// get returns the cached value for key and marks it most-recently-used. The
// boolean reports whether the key was present.
func (c *lruCache[K, V]) get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getLocked(key)
}

func (c *lruCache[K, V]) getLocked(key K) (V, bool) {
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*lruEntry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// putLocked inserts or updates key with value. The caller must hold the mutex.
func (c *lruCache[K, V]) putLocked(key K, value V) {
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		el.Value.(*lruEntry[K, V]).value = value
		return
	}
	el := c.ll.PushFront(&lruEntry[K, V]{key: key, value: value})
	c.items[key] = el
	if c.ll.Len() > c.capacity {
		c.evictOldestLocked()
	}
}

func (c *lruCache[K, V]) evictOldestLocked() {
	el := c.ll.Back()
	if el == nil {
		return
	}
	c.ll.Remove(el)
	delete(c.items, el.Value.(*lruEntry[K, V]).key)
}

// getOrTryInsertWith returns the cached value for key, or computes it via
// factory, stores it, and returns it. If factory returns an error the cache is
// left unchanged and the error is propagated.
//
// This mirrors `BlockingLruCache::get_or_try_insert_with`. The factory is
// invoked outside the lock so that potentially slow image decoding does not
// serialize unrelated cache lookups; the cost is that concurrent misses for the
// same key may each run the factory, with the last writer winning. This matches
// the externally observable contract (a correct value is always returned).
func (c *lruCache[K, V]) getOrTryInsertWith(key K, factory func() (V, error)) (V, error) {
	if v, ok := c.get(key); ok {
		return v, nil
	}

	v, err := factory()
	if err != nil {
		var zero V
		return zero, err
	}

	c.mu.Lock()
	c.putLocked(key, v)
	c.mu.Unlock()
	return v, nil
}

// clear removes all entries from the cache.
func (c *lruCache[K, V]) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = make(map[K]*list.Element, c.capacity)
}
