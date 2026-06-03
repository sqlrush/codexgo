package pathutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// SymlinkWritePaths describes the result of resolving a symlink chain for a path
// that may be written to.
//
// ReadPath, when set, is the final non-symlink target reached by following the
// chain (or the first missing component, which is a valid write target).
// ReadPath is empty (and HasReadPath is false) when the chain could not be
// resolved safely. WritePath is always set: it is the resolved target when
// resolution succeeded, or the original absolute root path otherwise.
//
// HasReadPath distinguishes "no read path" from an empty string, mirroring the
// Rust Option<PathBuf>.
type SymlinkWritePaths struct {
	ReadPath    string
	HasReadPath bool
	WritePath   string
}

// ResolveSymlinkWritePaths resolves the final filesystem target for path while
// retaining a safe write path.
//
// It follows symlink chains (including relative symlink targets) until it reaches
// a non-symlink path. If the chain cycles, or any metadata or link resolution
// fails, it returns HasReadPath=false and uses the original absolute path as the
// WritePath. There is no fixed max-resolution count; cycles are detected via a
// visited set.
//
// The returned error is always nil; it exists to mirror the Rust io::Result
// signature and to leave room for future fallible behavior.
func ResolveSymlinkWritePaths(path string) (SymlinkWritePaths, error) {
	root := path
	if abs, ok := fromAbsolutePath(path); ok {
		root = abs
	}

	current := root
	visited := make(map[string]struct{})

	for {
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// A missing path is a valid write target: writing creates it.
				return SymlinkWritePaths{
					ReadPath:    current,
					HasReadPath: true,
					WritePath:   current,
				}, nil
			}
			return fallbackToRoot(root), nil
		}

		if info.Mode()&fs.ModeSymlink == 0 {
			// Reached a real, non-symlink file or directory.
			return SymlinkWritePaths{
				ReadPath:    current,
				HasReadPath: true,
				WritePath:   current,
			}, nil
		}

		// Detect cycles: if we have already visited this path, the chain loops.
		if _, seen := visited[current]; seen {
			return fallbackToRoot(root), nil
		}
		visited[current] = struct{}{}

		target, err := os.Readlink(current)
		if err != nil {
			return fallbackToRoot(root), nil
		}

		next, ok := nextLinkTarget(target, current)
		if !ok {
			return fallbackToRoot(root), nil
		}
		current = next
	}
}

// nextLinkTarget computes the next path in a symlink chain. Absolute targets are
// normalized directly; relative targets are resolved against the parent of the
// current link. The boolean is false when the current path has no parent, which
// matches the Rust None-parent fall-back.
func nextLinkTarget(target string, current string) (string, bool) {
	if isAbsolutePath(target) {
		if abs, ok := fromAbsolutePath(target); ok {
			return abs, true
		}
		return "", false
	}
	parent := filepath.Dir(current)
	// filepath.Dir collapses "/" and "." to themselves; treat a path that is its
	// own parent as having no parent, mirroring Path::parent returning None.
	if parent == current {
		return "", false
	}
	return resolvePathAgainstBase(target, parent), true
}

// fallbackToRoot builds the unresolved result: no read path, write to root.
func fallbackToRoot(root string) SymlinkWritePaths {
	return SymlinkWritePaths{
		ReadPath:    "",
		HasReadPath: false,
		WritePath:   root,
	}
}
