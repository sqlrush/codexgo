package sandbox

import "strings"

// lexicalPathKey normalizes a path for deduplication: backslashes become forward
// slashes, a trailing slash is dropped, and the result is lower-cased. Windows
// paths are case-insensitive, so this key collapses equivalent spellings.
// Mirrors lexical_path_key in deny_read_acl.rs.
func lexicalPathKey(path string) string {
	replaced := strings.ReplaceAll(path, "\\", "/")
	trimmed := strings.TrimRight(replaced, "/")
	return strings.ToLower(trimmed)
}

// planDenyReadACLPaths returns the deduplicated list of paths that should receive
// a deny-read ACE, preserving order. Each input path is kept (so a missing path
// can be materialized later), and when it already exists its canonical target is
// also planned so a reparse point cannot be read through the resolved location.
// Mirrors plan_deny_read_acl_paths in deny_read_acl.rs; canonicalization is
// injected so the logic is unit-testable without touching the filesystem.
func planDenyReadACLPaths(paths []string, exists func(string) bool, canonicalize func(string) string) []string {
	planned := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))

	push := func(p string) {
		key := lexicalPathKey(p)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		planned = append(planned, p)
	}

	for _, path := range paths {
		push(path)
		if exists(path) {
			push(canonicalize(path))
		}
	}
	return planned
}
