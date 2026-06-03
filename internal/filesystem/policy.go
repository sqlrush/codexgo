package filesystem

import (
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file ports the subset of `FileSystemSandboxPolicy` predicate logic from
// the Rust protocol crate (`protocol/src/permissions.rs`) that
// [FileSystemSandboxContext.ShouldRunInSandbox] needs.
//
// DEVIATION: in Codex these are methods/free-functions on the protocol crate
// (`has_full_disk_write_access`, `has_root_access`, `has_write_narrowing_entries`,
// `has_same_target_write_override`, `file_system_paths_share_target`,
// `special_paths_share_target`, `special_path_matches_absolute_path`). The Go
// `internal/protocol` package does not yet expose them. To avoid redefining
// protocol *types* (which the project rules forbid) while still reproducing the
// sandbox-routing decision exactly, the predicate *logic* is reimplemented here
// over the already-exported protocol data types. The behavior is byte-for-byte
// faithful to the Rust source.

// hasFullDiskWriteAccess reports whether filesystem writes are unrestricted for
// the policy.
//
// Rust: `FileSystemSandboxPolicy::has_full_disk_write_access`.
func hasFullDiskWriteAccess(policy protocol.FileSystemSandboxPolicy) bool {
	switch policy.Kind {
	case protocol.FileSystemSandboxKindUnrestricted,
		protocol.FileSystemSandboxKindExternalSandbox:
		return true
	default: // Restricted
		return hasRootAccess(policy, protocol.FileSystemAccessMode.CanWrite) &&
			!hasWriteNarrowingEntries(policy)
	}
}

// hasRootAccess reports whether a restricted policy has a `:root` special entry
// whose access satisfies predicate.
//
// Rust: `FileSystemSandboxPolicy::has_root_access`.
func hasRootAccess(policy protocol.FileSystemSandboxPolicy, predicate func(protocol.FileSystemAccessMode) bool) bool {
	if policy.Kind != protocol.FileSystemSandboxKindRestricted {
		return false
	}
	for _, entry := range policy.Entries {
		if entry.Path.Type == protocol.FileSystemPathTypeSpecial &&
			entry.Path.Special != nil &&
			entry.Path.Special.Kind == protocol.FileSystemSpecialPathKindRoot &&
			predicate(entry.Access) {
			return true
		}
	}
	return false
}

// hasWriteNarrowingEntries reports whether a restricted policy contains any
// entry that really reduces a broader `:root = write` grant.
//
// Rust: `FileSystemSandboxPolicy::has_write_narrowing_entries`.
func hasWriteNarrowingEntries(policy protocol.FileSystemSandboxPolicy) bool {
	if policy.Kind != protocol.FileSystemSandboxKindRestricted {
		return false
	}
	for i := range policy.Entries {
		entry := policy.Entries[i]
		if entry.Access.CanWrite() {
			continue
		}
		switch entry.Path.Type {
		case protocol.FileSystemPathTypePath:
			if !hasSameTargetWriteOverride(policy, entry) {
				return true
			}
		case protocol.FileSystemPathTypeGlobPattern:
			return true
		case protocol.FileSystemPathTypeSpecial:
			if narrowsForSpecial(policy, entry) {
				return true
			}
		}
	}
	return false
}

// narrowsForSpecial evaluates the Special branch of has_write_narrowing_entries.
func narrowsForSpecial(policy protocol.FileSystemSandboxPolicy, entry protocol.FileSystemSandboxEntry) bool {
	special := entry.Path.Special
	if special == nil {
		return false
	}
	switch special.Kind {
	case protocol.FileSystemSpecialPathKindRoot:
		return entry.Access == protocol.FileSystemAccessModeDeny
	case protocol.FileSystemSpecialPathKindMinimal,
		protocol.FileSystemSpecialPathKindUnknown:
		return false
	default:
		return !hasSameTargetWriteOverride(policy, entry)
	}
}

// hasSameTargetWriteOverride reports whether a higher-priority write entry
// targets the same location as entry.
//
// Rust: `FileSystemSandboxPolicy::has_same_target_write_override`.
func hasSameTargetWriteOverride(policy protocol.FileSystemSandboxPolicy, entry protocol.FileSystemSandboxEntry) bool {
	for i := range policy.Entries {
		candidate := policy.Entries[i]
		if candidate.Access.CanWrite() &&
			accessModeGreater(candidate.Access, entry.Access) &&
			fileSystemPathsShareTarget(candidate.Path, entry.Path) {
			return true
		}
	}
	return false
}

// accessModeGreater reports whether left ranks strictly above right in the Rust
// `FileSystemAccessMode` ordering (Deny < Read < Write). Rust derives `PartialOrd`
// on the enum, so ordering follows declaration order.
func accessModeGreater(left, right protocol.FileSystemAccessMode) bool {
	return accessModeRank(left) > accessModeRank(right)
}

// accessModeRank assigns the declaration-order rank used by Rust's derived Ord.
func accessModeRank(mode protocol.FileSystemAccessMode) int {
	switch mode {
	case protocol.FileSystemAccessModeDeny:
		return 0
	case protocol.FileSystemAccessModeRead:
		return 1
	case protocol.FileSystemAccessModeWrite:
		return 2
	default:
		return 0
	}
}

// fileSystemPathsShareTarget reports whether two paths refer to the same target.
//
// Rust: free function `file_system_paths_share_target`.
func fileSystemPathsShareTarget(left, right protocol.FileSystemPath) bool {
	switch {
	case left.Type == protocol.FileSystemPathTypePath && right.Type == protocol.FileSystemPathTypePath:
		return left.Path == right.Path
	case left.Type == protocol.FileSystemPathTypeSpecial && right.Type == protocol.FileSystemPathTypeSpecial:
		return specialPathsShareTarget(left.Special, right.Special)
	case left.Type == protocol.FileSystemPathTypePath && right.Type == protocol.FileSystemPathTypeSpecial:
		return specialPathMatchesAbsolutePath(right.Special, left.Path)
	case left.Type == protocol.FileSystemPathTypeSpecial && right.Type == protocol.FileSystemPathTypePath:
		return specialPathMatchesAbsolutePath(left.Special, right.Path)
	default:
		// Any GlobPattern combination other than two equal patterns is false.
		if left.Type == protocol.FileSystemPathTypeGlobPattern && right.Type == protocol.FileSystemPathTypeGlobPattern {
			return left.Pattern == right.Pattern
		}
		return false
	}
}

// specialPathsShareTarget reports whether two special paths refer to the same
// target.
//
// Rust: free function `special_paths_share_target`.
func specialPathsShareTarget(left, right *protocol.FileSystemSpecialPath) bool {
	if left == nil || right == nil {
		return false
	}
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case protocol.FileSystemSpecialPathKindRoot,
		protocol.FileSystemSpecialPathKindMinimal,
		protocol.FileSystemSpecialPathKindTmpdir,
		protocol.FileSystemSpecialPathKindSlashTmp:
		return true
	case protocol.FileSystemSpecialPathKindProjectRoots:
		return optStringEqual(left.Subpath, right.Subpath)
	case protocol.FileSystemSpecialPathKindUnknown:
		return left.Path == right.Path && optStringEqual(left.Subpath, right.Subpath)
	default:
		return false
	}
}

// specialPathMatchesAbsolutePath reports whether a special path matches an
// absolute path.
//
// Rust: free function `special_path_matches_absolute_path`.
func specialPathMatchesAbsolutePath(value *protocol.FileSystemSpecialPath, path protocol.AbsolutePath) bool {
	if value == nil {
		return false
	}
	switch value.Kind {
	case protocol.FileSystemSpecialPathKindRoot:
		// Rust: `path.as_path().parent().is_none()` (true only for a filesystem
		// root). filepath.Dir(root) == root, so detect that fixed point.
		return filepath.Dir(string(path)) == string(path)
	case protocol.FileSystemSpecialPathKindSlashTmp:
		return string(path) == "/tmp"
	default:
		return false
	}
}

// optStringEqual compares two optional strings for equality, matching Rust
// Option equality semantics.
func optStringEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
