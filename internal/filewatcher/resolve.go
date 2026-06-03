package filewatcher

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// dedupeWatchedPaths sorts and removes duplicate watch requests, mirroring
// codex's dedupe_watched_paths (ordered by path then recursiveness). The input
// slice is not mutated; a new slice is returned.
func dedupeWatchedPaths(watchedPaths []WatchPath) []WatchPath {
	out := make([]WatchPath, len(watchedPaths))
	copy(out, watchedPaths)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		// false sorts before true, matching Rust's bool Ord.
		return !out[i].Recursive && out[j].Recursive
	})

	deduped := out[:0]
	for i, wp := range out {
		if i > 0 && wp == out[i-1] {
			continue
		}
		deduped = append(deduped, wp)
	}
	return deduped
}

// actualWatchPath returns the actual OS watch path, the canonical match path,
// and whether the watch is a missing-target fallback, mirroring codex's
// actual_watch_path.
//
// Missing targets are watched non-recursively through the nearest existing
// directory ancestor. As path components appear, the actual watch is moved
// closer to the requested path so broad recursive ancestor watches are never
// needed.
func actualWatchPath(requested WatchPath) (actual WatchPath, matched WatchPath, fallback bool) {
	if pathExists(requested.Path) {
		matchedPath := canonicalize(requested.Path)
		return requested,
			WatchPath{Path: matchedPath, Recursive: requested.Recursive},
			false
	}

	ancestor := parent(requested.Path)
	for ancestor != "" {
		if isDir(ancestor) {
			actualPath := canonicalize(ancestor)
			matchedPath := joinSuffix(actualPath, ancestor, requested.Path)
			return WatchPath{Path: ancestor, Recursive: false},
				WatchPath{Path: matchedPath, Recursive: requested.Recursive},
				true
		}
		next := parent(ancestor)
		if next == ancestor {
			break
		}
		ancestor = next
	}

	return requested, requested, false
}

// joinSuffix recreates Rust's strip_prefix(base).map(|s| canonicalBase.join(s)).
// When requested is not under base, the requested path is returned unchanged.
func joinSuffix(canonicalBase, base, requested string) string {
	rel, err := filepath.Rel(base, requested)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return requested
	}
	if rel == "." {
		return canonicalBase
	}
	return filepath.Join(canonicalBase, rel)
}

// canonicalize resolves symlinks like Rust's Path::canonicalize, falling back to
// the input on error. This makes macOS-style /var -> /private/var aliases match
// backend-reported paths.
func canonicalize(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	return abs
}

// parent returns the cleaned parent directory of path, or "" if path has no
// parent (it is the filesystem root or empty), mirroring Path::parent.
func parent(path string) string {
	clean := filepath.Clean(path)
	dir := filepath.Dir(clean)
	if dir == clean {
		// Reached the root (Dir of "/" is "/"); no further parent.
		return ""
	}
	return dir
}

// pathExists reports whether path exists (following symlinks).
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
