package filewatcher

import "sync"

// FileWatcherSubscriber is the handle used to register watched paths for one
// logical consumer. Obtain one from [FileWatcher.AddSubscriber].
//
// In codex the subscriber unregisters on Drop. Go has no destructors, so call
// [FileWatcherSubscriber.Close] when finished; this removes the subscriber, its
// registrations, and closes its receiver.
type FileWatcherSubscriber struct {
	id     subscriberID
	fw     *FileWatcher
	closed bool
	mu     sync.Mutex
}

// RegisterPaths registers the provided paths for this subscriber and returns a
// [WatchRegistration] guard whose [WatchRegistration.Close] unregisters them. It
// mirrors codex's register_paths. The input slice is not mutated.
func (s *FileWatcherSubscriber) RegisterPaths(watchedPaths []WatchPath) *WatchRegistration {
	deduped := dedupeWatchedPaths(watchedPaths)
	regs := make([]subscriberWatchRegistration, 0, len(deduped))
	keys := make([]subscriberWatchKey, 0, len(deduped))
	for _, requested := range deduped {
		actual, matched, fallback := actualWatchPath(requested)
		key := subscriberWatchKey{requested: requested, matched: matched}
		regs = append(regs, subscriberWatchRegistration{
			key:      key,
			actual:   actual,
			fallback: fallback,
		})
		keys = append(keys, key)
	}

	s.fw.registerPaths(s.id, regs)

	return &WatchRegistration{
		fw:           s.fw,
		subscriberID: s.id,
		watchedPaths: keys,
	}
}

// RegisterPath is a convenience for registering a single path.
func (s *FileWatcherSubscriber) RegisterPath(path string, recursive bool) *WatchRegistration {
	return s.RegisterPaths([]WatchPath{{Path: path, Recursive: recursive}})
}

// Close removes this subscriber and all of its registrations and closes its
// receiver. It is idempotent and is the Go analogue of codex's Drop impl.
func (s *FileWatcherSubscriber) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.fw.removeSubscriber(s.id)
}

// WatchRegistration is an explicit-cleanup guard for a set of active path
// registrations. Call [WatchRegistration.Close] to unregister them. It is the Go
// analogue of codex's RAII WatchRegistration; the zero value is a valid no-op.
type WatchRegistration struct {
	fw           *FileWatcher
	subscriberID subscriberID
	watchedPaths []subscriberWatchKey
	closed       bool
	mu           sync.Mutex
}

// Close unregisters the paths held by this registration. It is idempotent.
func (r *WatchRegistration) Close() {
	r.mu.Lock()
	if r.closed || r.fw == nil {
		r.closed = true
		r.mu.Unlock()
		return
	}
	r.closed = true
	keys := r.watchedPaths
	fw := r.fw
	id := r.subscriberID
	r.mu.Unlock()
	fw.unregisterPaths(id, keys)
}

// WatchCountsForTest returns the (nonRecursive, recursive) registration counts
// for an OS watch path, or ok=false if the path has no registrations. It mirrors
// codex's watch_counts_for_test.
func (fw *FileWatcher) WatchCountsForTest(path string) (nonRecursive, recursive int, ok bool) {
	fw.stateMu.RLock()
	defer fw.stateMu.RUnlock()
	counts, ok := fw.state.pathRefCounts[path]
	if !ok {
		return 0, 0, false
	}
	return counts.nonRecursive, counts.recursive, true
}

// ModeForTest returns the recorded OS watch mode for path, or ok=false if not
// watched. It is the Go analogue of inspecting FileWatcherInner.watched_paths in
// codex's tests.
func (fw *FileWatcher) ModeForTest(path string) (recursive bool, ok bool) {
	if fw.inner == nil {
		return false, false
	}
	m, ok := fw.inner.mode(path)
	if !ok {
		return false, false
	}
	return m == modeRecursive, true
}
