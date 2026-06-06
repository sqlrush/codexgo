// Package homedir resolves the Codex configuration directory (CODEXGO_HOME).
//
// This file contains a minimal, self-contained port of the parts of the
// upstream `codex_utils_absolute_path::AbsolutePathBuf` type that
// `find_codex_home` relies upon. The upstream crate guarantees that a path is
// absolute and normalized (lexically; not necessarily canonicalized). For the
// home-dir use case the only behavior we must reproduce is the lexical
// normalization performed by `AbsolutePathBuf::from_absolute_path` on a path
// that is already absolute (CODEXGO_HOME is canonicalized before this call, and
// the default `~/.codex` is built from an absolute home directory).
//
// When the full `internal/utils/absolutepath` package is ported it should
// replace this local helper; until then this keeps the homedir package
// dependency-free.
package homedir

import (
	"path/filepath"
	"strings"
)

// normalizeAbsolute reproduces the lexical normalization that
// `AbsolutePathBuf::from_absolute_path` applies to an already-absolute path.
//
// Upstream walks the path components and:
//   - drops "." (current-dir) components,
//   - resolves ".." (parent-dir) components by popping the previous component,
//   - keeps the root and normal components.
//
// It never touches the filesystem, so symlinks are not resolved here. The input
// must already be absolute; callers in this package guarantee that.
//
// The returned string is a freshly built value; the caller-provided argument is
// never mutated.
func normalizeAbsolute(path string) string {
	// filepath.Clean implements exactly the lexical algorithm described above
	// for the host platform: it collapses redundant separators, removes "."
	// elements, and resolves ".." against the preceding non-".." element while
	// keeping a leading root in place. This matches Rust's component-based
	// normalize_path for absolute inputs.
	cleaned := filepath.Clean(path)

	// filepath.Clean turns an empty string into ".". Upstream's normalize_path
	// also yields "." for an empty normalized result, so the two agree. Guard
	// the empty case explicitly so behavior is obvious to readers.
	if strings.TrimSpace(path) == "" {
		return "."
	}
	return cleaned
}
