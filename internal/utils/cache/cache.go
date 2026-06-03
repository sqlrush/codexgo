// Package cache provides a small, generic LRU cache utility together with a
// content-hashing helper.
//
// It is a faithful Go port of the codex-utils-cache Rust crate. The public
// surface mirrors the Rust API:
//
//   - BlockingLruCache: a fixed-capacity LRU cache guarded by a mutex.
//   - Sha1Digest: the SHA-1 digest of a byte slice, useful as a content-based
//     cache key.
//
// # Runtime-gating divergence
//
// The Rust crate guards every operation behind a Tokio runtime check
// (`lock_if_runtime`): when invoked outside an async runtime, calls become
// no-ops. Go has no equivalent notion of an ambient async runtime — a
// [sync.Mutex] works correctly from any goroutine — so the Go port behaves as
// the Rust crate does when a runtime IS present, which is the only meaningful
// production configuration. The externally observable cache semantics
// (storage, retrieval, LRU eviction, fallible insertion) are preserved exactly
// for that configuration.
//
// To support callers that intentionally want the disabled, no-op behavior of
// the Rust crate's out-of-runtime path, [BlockingLruCache] exposes an explicit
// SetEnabled toggle and a NewDisabledBlockingLruCache constructor.
package cache

import "sync"

// BlockingLruCache is a fixed-capacity LRU cache protected by a mutex.
//
// All methods are safe for concurrent use. The zero value is not usable;
// construct instances with [NewBlockingLruCache], [TryWithCapacity], or
// [NewDisabledBlockingLruCache].
type BlockingLruCache[K comparable, V any] struct {
	mu      sync.Mutex
	inner   *LruCache[K, V]
	enabled bool
}

// NewBlockingLruCache creates a cache with the provided capacity.
//
// The capacity must be greater than zero, mirroring the Rust constructor that
// accepts a NonZeroUsize. A capacity of zero returns an error so the boundary
// is validated explicitly rather than panicking.
func NewBlockingLruCache[K comparable, V any](capacity uint) (*BlockingLruCache[K, V], error) {
	if capacity == 0 {
		return nil, ErrZeroCapacity
	}
	return &BlockingLruCache[K, V]{
		inner:   newLruCache[K, V](capacity),
		enabled: true,
	}, nil
}

// TryWithCapacity builds a cache if capacity is non-zero, returning (nil, false)
// otherwise. It mirrors the Rust `try_with_capacity`, which returns
// `Option<Self>`.
func TryWithCapacity[K comparable, V any](capacity uint) (*BlockingLruCache[K, V], bool) {
	c, err := NewBlockingLruCache[K, V](capacity)
	if err != nil {
		return nil, false
	}
	return c, true
}

// NewDisabledBlockingLruCache creates a cache that behaves as the Rust crate
// does outside a Tokio runtime: every operation is a no-op.
//
// This exists so callers that depend on the Rust crate's runtime-gated,
// disabled behavior can reproduce it deterministically. The capacity is still
// recorded so the cache can be re-enabled later via SetEnabled.
func NewDisabledBlockingLruCache[K comparable, V any](capacity uint) (*BlockingLruCache[K, V], error) {
	if capacity == 0 {
		return nil, ErrZeroCapacity
	}
	return &BlockingLruCache[K, V]{
		inner:   newLruCache[K, V](capacity),
		enabled: false,
	}, nil
}

// Enabled reports whether the cache is active. A disabled cache treats every
// operation as a no-op, matching the Rust out-of-runtime behavior.
func (c *BlockingLruCache[K, V]) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// SetEnabled toggles whether the cache is active. Disabling does not clear the
// existing contents; it only suppresses subsequent operations, matching the
// Rust crate where the runtime check, not the data, gates each call.
func (c *BlockingLruCache[K, V]) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
}

// GetOrInsertWith returns the cached value for key, or computes it with value,
// inserts it, and returns it.
//
// The value factory is invoked at most once and only when the key is absent.
// When the cache is disabled the factory is always invoked and nothing is
// stored, mirroring the Rust fallback path.
func (c *BlockingLruCache[K, V]) GetOrInsertWith(key K, value func() V) V {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return value()
	}
	if v, ok := c.inner.Get(key); ok {
		return v
	}
	v := value()
	c.inner.Put(key, v)
	return v
}

// GetOrTryInsertWith is the fallible variant of [BlockingLruCache.GetOrInsertWith].
//
// If the value factory returns an error, nothing is stored and the error is
// propagated. When the cache is disabled the factory is always invoked and its
// result returned without caching, mirroring the Rust fallback path.
func (c *BlockingLruCache[K, V]) GetOrTryInsertWith(key K, value func() (V, error)) (V, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return value()
	}
	if v, ok := c.inner.Get(key); ok {
		return v, nil
	}
	v, err := value()
	if err != nil {
		var zero V
		return zero, err
	}
	c.inner.Put(key, v)
	return v, nil
}

// Get returns the value for key and whether it was present. Accessing an entry
// marks it most-recently-used. A disabled cache always reports absence.
func (c *BlockingLruCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		var zero V
		return zero, false
	}
	return c.inner.Get(key)
}

// Insert stores value for key, returning the previous entry and whether one
// existed. A disabled cache stores nothing and always reports absence,
// mirroring the Rust `insert` returning `None` outside a runtime.
func (c *BlockingLruCache[K, V]) Insert(key K, value V) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		var zero V
		return zero, false
	}
	return c.inner.Put(key, value)
}

// Remove deletes the entry for key, returning it and whether it existed. A
// disabled cache removes nothing and always reports absence.
func (c *BlockingLruCache[K, V]) Remove(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		var zero V
		return zero, false
	}
	return c.inner.Pop(key)
}

// Clear removes every entry. On a disabled cache it is a no-op.
func (c *BlockingLruCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return
	}
	c.inner.Clear()
}

// WithMut executes callback with exclusive access to the underlying
// [LruCache], returning the callback's result.
//
// When the cache is disabled, the callback runs against a fresh unbounded
// throwaway cache whose contents are discarded afterward, exactly matching the
// Rust crate's out-of-runtime branch that operates on `LruCache::unbounded`.
func WithMut[K comparable, V any, R any](c *BlockingLruCache[K, V], callback func(*LruCache[K, V]) R) R {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		disabled := newUnboundedLruCache[K, V]()
		return callback(disabled)
	}
	return callback(c.inner)
}

// Lock returns the underlying [LruCache] together with an unlock function for
// direct, exclusive access, or (nil, nil) when the cache is disabled.
//
// This is the analog of the Rust `blocking_lock`, which yields the guard only
// when a runtime is available. Callers MUST invoke the returned unlock function
// exactly once when finished; using defer is recommended:
//
//	inner, unlock := c.Lock()
//	if unlock != nil {
//	    defer unlock()
//	    // use inner
//	}
func (c *BlockingLruCache[K, V]) Lock() (*LruCache[K, V], func()) {
	c.mu.Lock()
	if !c.enabled {
		c.mu.Unlock()
		return nil, nil
	}
	return c.inner, c.mu.Unlock
}
