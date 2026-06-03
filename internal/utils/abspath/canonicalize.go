package abspath

import (
	"fmt"
	"os"
	"path/filepath"
)

// canonicalize resolves a path to an existing, symlink-free absolute path. It is
// the Go analog of Rust's `dunce::canonicalize`: `filepath.EvalSymlinks`
// resolves symlinks (and requires the path to exist), and `filepath.Abs`
// guarantees an absolute result. On Windows, Go does not emit verbatim ("\\?\")
// prefixes, matching dunce's friendly-path behavior.
func canonicalize(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// Canonicalize mirrors Rust's `AbsolutePathBuf::canonicalize`: it canonicalizes
// the path (resolving symlinks; the path must exist) and returns a new
// [AbsolutePathBuf]. The receiver is not modified.
func (a AbsolutePathBuf) Canonicalize() (AbsolutePathBuf, error) {
	c, err := canonicalize(a.path)
	if err != nil {
		return AbsolutePathBuf{}, fmt.Errorf("abspath: canonicalize %q: %w", a.path, err)
	}
	return AbsolutePathBuf{path: c}, nil
}

// CanonicalizePreservingSymlinks mirrors Rust's
// `canonicalize_preserving_symlinks`.
//
// It canonicalizes the path when possible, but preserves the logical absolute
// path whenever canonicalization would rewrite it through a nested symlink.
// Top-level system aliases (e.g. macOS /var -> /private/var) stay canonicalized.
// If the path cannot be canonicalized, the logical absolute path is returned.
func CanonicalizePreservingSymlinks(path string) (string, error) {
	logicalBuf, err := FromAbsolutePath(path)
	if err != nil {
		return "", err
	}
	logical := logicalBuf.path
	preserve := shouldPreserveLogicalPath(logical)

	canonical, err := canonicalize(path)
	if err != nil {
		return logical, nil
	}
	if preserve && canonical != logical {
		return logical, nil
	}
	return canonical, nil
}

// CanonicalizeExistingPreservingSymlinks mirrors Rust's
// `canonicalize_existing_preserving_symlinks`.
//
// Like [CanonicalizePreservingSymlinks] it preserves the logical path through
// nested symlinks, but canonicalization failures are propagated so callers can
// reject invalid working directories early.
func CanonicalizeExistingPreservingSymlinks(path string) (string, error) {
	logicalBuf, err := FromAbsolutePath(path)
	if err != nil {
		return "", err
	}
	logical := logicalBuf.path

	canonical, err := canonicalize(path)
	if err != nil {
		return "", fmt.Errorf("abspath: canonicalize %q: %w", path, err)
	}
	if shouldPreserveLogicalPath(logical) && canonical != logical {
		return logical, nil
	}
	return canonical, nil
}

// shouldPreserveLogicalPath mirrors Rust's `should_preserve_logical_path`: it
// reports whether any ancestor of the logical path is itself a symlink that is
// nested (i.e. not a top-level alias). The Rust check `ancestor.parent().and_
// then(Path::parent).is_some()` excludes the two shallowest levels.
func shouldPreserveLogicalPath(logical string) bool {
	cur := logical
	for {
		if isNestedAncestor(cur) {
			info, err := os.Lstat(cur)
			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				return true
			}
		}
		parent, ok := parentOf(cur)
		if !ok {
			return false
		}
		cur = parent
	}
}

// isNestedAncestor reports whether ancestor has a grandparent, mirroring
// `ancestor.parent().and_then(Path::parent).is_some()`.
func isNestedAncestor(ancestor string) bool {
	parent, ok := parentOf(ancestor)
	if !ok {
		return false
	}
	_, ok = parentOf(parent)
	return ok
}
