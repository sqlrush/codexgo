package cache

import (
	"encoding/hex"
	"errors"
	"sync"
	"testing"
)

func mustNew[K comparable, V any](t *testing.T, capacity uint) *BlockingLruCache[K, V] {
	t.Helper()
	c, err := NewBlockingLruCache[K, V](capacity)
	if err != nil {
		t.Fatalf("NewBlockingLruCache(%d) returned error: %v", capacity, err)
	}
	return c
}

func TestNewBlockingLruCacheCapacityValidation(t *testing.T) {
	tests := []struct {
		name     string
		capacity uint
		wantErr  bool
	}{
		{name: "zero is rejected", capacity: 0, wantErr: true},
		{name: "one is accepted", capacity: 1, wantErr: false},
		{name: "large is accepted", capacity: 1024, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := NewBlockingLruCache[string, int](tt.capacity)
			if tt.wantErr {
				if !errors.Is(err, ErrZeroCapacity) {
					t.Fatalf("expected ErrZeroCapacity, got %v", err)
				}
				if c != nil {
					t.Fatalf("expected nil cache on error, got %v", c)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c == nil {
				t.Fatal("expected non-nil cache")
			}
		})
	}
}

func TestTryWithCapacity(t *testing.T) {
	tests := []struct {
		name     string
		capacity uint
		wantOK   bool
	}{
		{name: "zero yields no cache", capacity: 0, wantOK: false},
		{name: "non-zero yields a cache", capacity: 3, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := TryWithCapacity[string, int](tt.capacity)
			if ok != tt.wantOK {
				t.Fatalf("TryWithCapacity ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && c == nil {
				t.Fatal("expected non-nil cache when ok is true")
			}
			if !ok && c != nil {
				t.Fatal("expected nil cache when ok is false")
			}
		})
	}
}

// Mirrors the Rust stores_and_retrieves_values test.
func TestStoresAndRetrievesValues(t *testing.T) {
	c := mustNew[string, int](t, 2)

	if _, ok := c.Get("first"); ok {
		t.Fatal("expected miss before insert")
	}

	if _, existed := c.Insert("first", 1); existed {
		t.Fatal("expected no previous entry on first insert")
	}

	got, ok := c.Get("first")
	if !ok || got != 1 {
		t.Fatalf("Get(first) = (%d, %v), want (1, true)", got, ok)
	}
}

// Mirrors the Rust evicts_least_recently_used test.
func TestEvictsLeastRecentlyUsed(t *testing.T) {
	c := mustNew[string, int](t, 2)

	c.Insert("a", 1)
	c.Insert("b", 2)

	// Touching "a" makes it most-recently-used, so "b" becomes the LRU victim.
	if got, ok := c.Get("a"); !ok || got != 1 {
		t.Fatalf("Get(a) = (%d, %v), want (1, true)", got, ok)
	}

	c.Insert("c", 3)

	if _, ok := c.Get("b"); ok {
		t.Fatal("expected b to be evicted")
	}
	if got, ok := c.Get("a"); !ok || got != 1 {
		t.Fatalf("Get(a) = (%d, %v), want (1, true)", got, ok)
	}
	if got, ok := c.Get("c"); !ok || got != 3 {
		t.Fatalf("Get(c) = (%d, %v), want (3, true)", got, ok)
	}
}

func TestInsertReturnsPrevious(t *testing.T) {
	c := mustNew[string, int](t, 2)

	if prev, existed := c.Insert("k", 1); existed || prev != 0 {
		t.Fatalf("first Insert = (%d, %v), want (0, false)", prev, existed)
	}
	if prev, existed := c.Insert("k", 2); !existed || prev != 1 {
		t.Fatalf("second Insert = (%d, %v), want (1, true)", prev, existed)
	}
	if got, ok := c.Get("k"); !ok || got != 2 {
		t.Fatalf("Get(k) = (%d, %v), want (2, true)", got, ok)
	}
}

func TestRemove(t *testing.T) {
	c := mustNew[string, int](t, 2)
	c.Insert("k", 5)

	if got, ok := c.Remove("k"); !ok || got != 5 {
		t.Fatalf("Remove(k) = (%d, %v), want (5, true)", got, ok)
	}
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected k to be gone after remove")
	}
	if _, ok := c.Remove("k"); ok {
		t.Fatal("expected second remove to report absence")
	}
}

func TestClear(t *testing.T) {
	c := mustNew[string, int](t, 4)
	c.Insert("a", 1)
	c.Insert("b", 2)
	c.Clear()

	if _, ok := c.Get("a"); ok {
		t.Fatal("expected a gone after clear")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("expected b gone after clear")
	}
}

func TestGetOrInsertWith(t *testing.T) {
	c := mustNew[string, int](t, 2)

	calls := 0
	factory := func() int {
		calls++
		return 42
	}

	if got := c.GetOrInsertWith("k", factory); got != 42 {
		t.Fatalf("first GetOrInsertWith = %d, want 42", got)
	}
	if got := c.GetOrInsertWith("k", factory); got != 42 {
		t.Fatalf("second GetOrInsertWith = %d, want 42", got)
	}
	if calls != 1 {
		t.Fatalf("factory invoked %d times, want 1", calls)
	}
}

func TestGetOrTryInsertWith(t *testing.T) {
	c := mustNew[string, int](t, 2)
	sentinel := errors.New("boom")

	// Error path: nothing cached, error propagated.
	if _, err := c.GetOrTryInsertWith("k", func() (int, error) {
		return 0, sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected nothing cached after failed factory")
	}

	// Success path: value cached, factory not re-invoked.
	calls := 0
	ok := func() (int, error) {
		calls++
		return 7, nil
	}
	if got, err := c.GetOrTryInsertWith("k", ok); err != nil || got != 7 {
		t.Fatalf("GetOrTryInsertWith = (%d, %v), want (7, nil)", got, err)
	}
	if got, err := c.GetOrTryInsertWith("k", ok); err != nil || got != 7 {
		t.Fatalf("cached GetOrTryInsertWith = (%d, %v), want (7, nil)", got, err)
	}
	if calls != 1 {
		t.Fatalf("factory invoked %d times, want 1", calls)
	}
}

func TestWithMutEnabled(t *testing.T) {
	c := mustNew[string, int](t, 4)

	result := WithMut(c, func(inner *LruCache[string, int]) (out int) {
		inner.Put("tmp", 3)
		v, _ := inner.Get("tmp")
		return v
	})
	if result != 3 {
		t.Fatalf("WithMut result = %d, want 3", result)
	}
	// Mutations through WithMut persist on an enabled cache.
	if got, ok := c.Get("tmp"); !ok || got != 3 {
		t.Fatalf("Get(tmp) = (%d, %v), want (3, true)", got, ok)
	}
}

func TestLockEnabled(t *testing.T) {
	c := mustNew[string, int](t, 4)

	inner, unlock := c.Lock()
	if inner == nil || unlock == nil {
		t.Fatal("expected non-nil inner and unlock on enabled cache")
	}
	inner.Put("x", 9)
	unlock()

	if got, ok := c.Get("x"); !ok || got != 9 {
		t.Fatalf("Get(x) = (%d, %v), want (9, true)", got, ok)
	}
}

// Mirrors the Rust disabled_without_runtime test: every operation is a no-op
// and WithMut operates on a throwaway unbounded cache whose contents vanish.
func TestDisabledNoOpBehavior(t *testing.T) {
	c, err := NewDisabledBlockingLruCache[string, int](2)
	if err != nil {
		t.Fatalf("NewDisabledBlockingLruCache returned error: %v", err)
	}
	if c.Enabled() {
		t.Fatal("expected cache to start disabled")
	}

	if _, existed := c.Insert("first", 1); existed {
		t.Fatal("disabled Insert should report no previous entry")
	}
	if _, ok := c.Get("first"); ok {
		t.Fatal("disabled Get should report absence")
	}

	if got := c.GetOrInsertWith("first", func() int { return 2 }); got != 2 {
		t.Fatalf("disabled GetOrInsertWith = %d, want 2", got)
	}
	if _, ok := c.Get("first"); ok {
		t.Fatal("disabled GetOrInsertWith should not store")
	}

	if _, ok := c.Remove("first"); ok {
		t.Fatal("disabled Remove should report absence")
	}
	c.Clear() // must not panic

	result := WithMut(c, func(inner *LruCache[string, int]) (out int) {
		inner.Put("tmp", 3)
		v, _ := inner.Get("tmp")
		return v
	})
	if result != 3 {
		t.Fatalf("disabled WithMut result = %d, want 3", result)
	}
	if _, ok := c.Get("tmp"); ok {
		t.Fatal("disabled WithMut should not persist into the real cache")
	}

	inner, unlock := c.Lock()
	if inner != nil || unlock != nil {
		t.Fatal("disabled Lock should return (nil, nil)")
	}
}

func TestSetEnabledToggle(t *testing.T) {
	c := mustNew[string, int](t, 2)
	c.Insert("k", 1)

	c.SetEnabled(false)
	if c.Enabled() {
		t.Fatal("expected disabled after SetEnabled(false)")
	}
	if _, ok := c.Get("k"); ok {
		t.Fatal("disabled Get should report absence even with retained data")
	}

	// Re-enabling exposes the retained contents again.
	c.SetEnabled(true)
	if got, ok := c.Get("k"); !ok || got != 1 {
		t.Fatalf("Get(k) after re-enable = (%d, %v), want (1, true)", got, ok)
	}
}

func TestSha1Digest(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantHex string
	}{
		{
			name:    "empty input",
			input:   nil,
			wantHex: "da39a3ee5e6b4b0d3255bfef95601890afd80709",
		},
		{
			name:    "abc",
			input:   []byte("abc"),
			wantHex: "a9993e364706816aba3e25717850c26c9cd0d89d",
		},
		{
			name:    "the quick brown fox",
			input:   []byte("The quick brown fox jumps over the lazy dog"),
			wantHex: "2fd4e1c67a2d28fced849ee1bb76e7391b93eb12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sha1Digest(tt.input)
			if len(got) != Sha1DigestSize {
				t.Fatalf("digest length = %d, want %d", len(got), Sha1DigestSize)
			}
			if hex.EncodeToString(got[:]) != tt.wantHex {
				t.Fatalf("Sha1Digest = %s, want %s", hex.EncodeToString(got[:]), tt.wantHex)
			}
		})
	}
}

func TestSha1DigestDoesNotMutateInput(t *testing.T) {
	input := []byte("immutable")
	snapshot := make([]byte, len(input))
	copy(snapshot, input)

	_ = Sha1Digest(input)

	if string(input) != string(snapshot) {
		t.Fatalf("input mutated: got %q, want %q", input, snapshot)
	}
}

func TestConcurrentAccessIsSafe(t *testing.T) {
	c := mustNew[int, int](t, 64)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Insert(n%64, n)
			_, _ = c.Get(n % 64)
			_ = c.GetOrInsertWith(n%64, func() int { return n })
		}(i)
	}
	wg.Wait()

	if c.Enabled() != true {
		t.Fatal("cache unexpectedly disabled")
	}
}
