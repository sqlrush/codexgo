package memories

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// readSortedDirPaths returns the lexically sorted child paths of dirPath,
// mirroring read_sorted_dir_paths (a missing directory yields an empty slice).
func readSortedDirPaths(dirPath string) ([]string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, errIO(err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, filepath.Join(dirPath, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// rejectSymlink returns an InvalidPath error when info describes a symlink,
// mirroring reject_symlink. info must come from a no-follow (Lstat) call.
func rejectSymlink(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return errInvalidPath(path, "must not be a symlink")
	}
	return nil
}

// isHiddenComponent reports whether a single path component starts with a dot,
// mirroring is_hidden_component (root/parent/prefix components are never hidden).
func isHiddenComponent(component string) bool {
	return strings.HasPrefix(component, ".")
}

// isHiddenPath reports whether the final path component starts with a dot,
// mirroring is_hidden_path.
func isHiddenPath(path string) bool {
	name := filepath.Base(path)
	return strings.HasPrefix(name, ".")
}

// displayRelativePath renders path relative to root using forward slashes,
// dropping empty components, mirroring display_relative_path.
func displayRelativePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	if rel == "." {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	kept := parts[:0]
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "/")
}
