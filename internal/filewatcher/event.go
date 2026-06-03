// Package filewatcher watches subscribed files or directories and routes
// coarse-grained change notifications to the subscribers that own matching
// watched paths.
//
// It is a faithful Go port of codex's `file-watcher` crate. The crate is built
// on the Rust `notify` library; this port is built on
// github.com/fsnotify/fsnotify. The observable model is preserved:
//
//   - Subscribers register one or more [WatchPath] entries and receive a
//     dedicated [Receiver] of coalesced change notifications.
//   - Registrations are reference-counted per path so multiple subscribers (or
//     repeated registrations) share a single OS watch.
//   - Missing targets fall back to watching the nearest existing directory
//     ancestor, and the actual OS watch migrates closer to the requested path
//     as missing components are created.
//   - [ThrottledWatchReceiver] and [DebouncedWatchReceiver] coalesce rapid
//     bursts of notifications.
package filewatcher

import "path/filepath"

// FileWatcherEvent is a coalesced file change notification for a subscriber.
type FileWatcherEvent struct {
	// Paths holds the changed paths delivered in sorted order with duplicates
	// removed.
	Paths []string
}

// Equal reports whether two events carry the same set of paths in the same
// order. It is primarily useful in tests.
func (e FileWatcherEvent) Equal(other FileWatcherEvent) bool {
	if len(e.Paths) != len(other.Paths) {
		return false
	}
	for i := range e.Paths {
		if e.Paths[i] != other.Paths[i] {
			return false
		}
	}
	return true
}

// WatchPath is a path subscription registered by a [FileWatcherSubscriber].
//
// The zero value is not meaningful; construct it explicitly. Paths are compared
// and stored verbatim (as provided), mirroring codex which keeps the requested
// namespace distinct from the canonical one.
type WatchPath struct {
	// Path is the root path to watch.
	Path string
	// Recursive reports whether events below Path should match recursively.
	Recursive bool
}

// clean returns a copy of wp with a lexically cleaned path. Cleaning keeps
// matching predictable across "a/b" vs "a/b/" style inputs without touching the
// filesystem (no canonicalization).
func (wp WatchPath) clean() WatchPath {
	return WatchPath{Path: filepath.Clean(wp.Path), Recursive: wp.Recursive}
}
