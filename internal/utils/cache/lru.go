package cache

import "container/list"

// LruCache is a fixed-capacity least-recently-used cache.
//
// It mirrors the behavior of the Rust `lru::LruCache` type used by the
// reference crate to the extent required by this port: a bounded map where
// reading or writing an entry marks it as most-recently-used, and inserting a
// new entry past the capacity evicts the least-recently-used one.
//
// LruCache is NOT safe for concurrent use on its own; callers must provide
// their own synchronization. [BlockingLruCache] wraps it with a mutex.
type LruCache[K comparable, V any] struct {
	// capacity is the maximum number of live entries. A capacity of zero
	// means the cache is unbounded, matching `LruCache::unbounded`.
	capacity uint
	// unbounded indicates the cache never evicts based on size.
	unbounded bool

	// order is a doubly linked list ordered from most-recently-used (front)
	// to least-recently-used (back). Each element holds an *entry.
	order *list.List
	// items maps keys to their list element for O(1) lookup.
	items map[K]*list.Element
}

// entry is the value stored in each list element.
type entry[K comparable, V any] struct {
	key   K
	value V
}

// newLruCache creates a bounded LRU cache with the provided capacity.
//
// The capacity must be non-zero; callers are expected to validate this at the
// boundary (see [NewBlockingLruCache] and [TryWithCapacity]).
func newLruCache[K comparable, V any](capacity uint) *LruCache[K, V] {
	return &LruCache[K, V]{
		capacity: capacity,
		order:    list.New(),
		items:    make(map[K]*list.Element),
	}
}

// newUnboundedLruCache creates an LRU cache that never evicts based on size,
// mirroring `LruCache::unbounded`.
func newUnboundedLruCache[K comparable, V any]() *LruCache[K, V] {
	return &LruCache[K, V]{
		unbounded: true,
		order:     list.New(),
		items:     make(map[K]*list.Element),
	}
}

// Len reports the number of live entries currently held by the cache.
func (c *LruCache[K, V]) Len() int {
	return len(c.items)
}

// Cap reports the configured capacity. For an unbounded cache it returns zero.
func (c *LruCache[K, V]) Cap() uint {
	if c.unbounded {
		return 0
	}
	return c.capacity
}

// Get returns the value associated with key and marks it as most-recently-used.
//
// The boolean result reports whether the key was present. This mirrors the
// `Option<&V>` returned by the Rust API, where a `Some` access also promotes
// the entry.
func (c *LruCache[K, V]) Get(key K) (V, bool) {
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*entry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Put inserts or updates the value for key and marks it as most-recently-used.
//
// If an entry already existed for key, its previous value is returned together
// with true. Otherwise the zero value and false are returned. When inserting a
// new entry would exceed the capacity, the least-recently-used entry is
// evicted. This matches the semantics of `LruCache::put`.
func (c *LruCache[K, V]) Put(key K, value V) (V, bool) {
	if el, ok := c.items[key]; ok {
		ent := el.Value.(*entry[K, V])
		prev := ent.value
		ent.value = value
		c.order.MoveToFront(el)
		return prev, true
	}

	el := c.order.PushFront(&entry[K, V]{key: key, value: value})
	c.items[key] = el

	if !c.unbounded && uint(len(c.items)) > c.capacity {
		c.evictOldest()
	}

	var zero V
	return zero, false
}

// Pop removes the entry for key, returning its value and whether it existed.
// This matches the semantics of `LruCache::pop`.
func (c *LruCache[K, V]) Pop(key K) (V, bool) {
	if el, ok := c.items[key]; ok {
		ent := el.Value.(*entry[K, V])
		c.order.Remove(el)
		delete(c.items, key)
		return ent.value, true
	}
	var zero V
	return zero, false
}

// Clear removes every entry from the cache, matching `LruCache::clear`.
func (c *LruCache[K, V]) Clear() {
	c.order.Init()
	c.items = make(map[K]*list.Element)
}

// evictOldest removes the least-recently-used entry. It assumes the cache is
// non-empty.
func (c *LruCache[K, V]) evictOldest() {
	back := c.order.Back()
	if back == nil {
		return
	}
	ent := back.Value.(*entry[K, V])
	c.order.Remove(back)
	delete(c.items, ent.key)
}
