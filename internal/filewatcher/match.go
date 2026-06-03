package filewatcher

import (
	"path/filepath"
	"strings"
)

// subscriberWatchState is the mutable per-subscriber watch state, mirroring
// codex's SubscriberWatchState. It is referenced by pointer so matching can
// update lastExists and fallback in place.
type subscriberWatchState struct {
	// actual is the existing path passed to the OS watcher and used for
	// ref-counting. Usually requested, but missing targets use an ancestor.
	actual WatchPath
	count  int
	// lastExists records whether the requested path existed the last time an
	// ancestor event was handled, preserving delete notifications for fallbacks.
	lastExists bool
	// fallback reports whether this watch began from a missing path. Such
	// watches normalize ancestor create/delete events back to requested.
	fallback bool
}

// subscriberWatchKey is the immutable per-subscriber watch identity, mirroring
// codex's SubscriberWatchKey. It is comparable so it can be a map key.
type subscriberWatchKey struct {
	// requested is the original path requested by the subscriber. Notifications
	// are reported in this namespace so clients never see canonicalization
	// artifacts.
	requested WatchPath
	// matched is the canonical equivalent of requested used to match backend
	// events. Some backends report canonical paths (e.g. /private/var/...) even
	// when the watch was registered through /var/....
	matched WatchPath
}

// changedPathForEvent converts one raw backend event path into the
// subscriber-visible path, or returns ("", false) when the event does not match
// this watch. It mirrors codex's changed_path_for_event.
func changedPathForEvent(key subscriberWatchKey, state *subscriberWatchState, eventPath string) (string, bool) {
	if p, ok := changedPathForMatchedPath(key, state, key.matched, eventPath); ok {
		return p, true
	}
	if key.matched.Path == key.requested.Path {
		return "", false
	}
	return changedPathForMatchedPath(key, state, key.requested, eventPath)
}

// changedPathForMatchedPath applies the watch matching rules in one path
// namespace and maps any emitted path back into the subscriber's requested
// namespace. It mirrors codex's changed_path_for_matched_path.
func changedPathForMatchedPath(key subscriberWatchKey, state *subscriberWatchState, matched WatchPath, eventPath string) (string, bool) {
	requested := key.requested

	if eventPath == matched.Path {
		state.lastExists = pathExists(matched.Path)
		return requested.Path, true
	}

	if startsWith(matched.Path, eventPath) {
		// eventPath is an ancestor of the matched path.
		nowExists := pathExists(matched.Path)
		if state.fallback {
			shouldNotify := nowExists || state.lastExists
			state.lastExists = nowExists
			if shouldNotify {
				return requested.Path, true
			}
			return "", false
		}
		if state.actual.Path != matched.Path {
			shouldNotify := nowExists || state.lastExists
			state.lastExists = nowExists
			if shouldNotify {
				return requested.Path, true
			}
			return "", false
		}
		state.lastExists = nowExists
		return eventPath, true
	}

	if !startsWith(eventPath, matched.Path) {
		return "", false
	}
	if !(matched.Recursive || parentEquals(eventPath, matched.Path)) {
		return "", false
	}
	state.lastExists = pathExists(matched.Path)
	return mapToRequested(matched.Path, eventPath, requested.Path), true
}

// mapToRequested rewrites eventPath, which lives under matchedBase, into the
// requested namespace. When eventPath is not under matchedBase the original
// eventPath is returned (mirroring Rust's unwrap_or_else fallback).
func mapToRequested(matchedBase, eventPath, requestedBase string) string {
	rel, err := filepath.Rel(matchedBase, eventPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return eventPath
	}
	if rel == "." {
		return requestedBase
	}
	return filepath.Join(requestedBase, rel)
}

// startsWith reports whether child is path-prefixed by base on component
// boundaries (so "/a/bc" does not start with "/a/b"). Equal paths return true,
// mirroring Rust's Path::starts_with.
func startsWith(child, base string) bool {
	child = filepath.Clean(child)
	base = filepath.Clean(base)
	if child == base {
		return true
	}
	rel, err := filepath.Rel(base, child)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	// filepath.Rel may produce a relative path even for unrelated roots on the
	// same volume; guard by re-joining.
	return filepath.Join(base, rel) == child
}

// parentEquals reports whether the parent directory of child equals base,
// mirroring Rust's event_path.parent() == Some(matched.path).
func parentEquals(child, base string) bool {
	return filepath.Dir(filepath.Clean(child)) == filepath.Clean(base)
}
