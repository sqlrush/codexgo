package filesystem

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// copyPath performs a single copy operation, dispatching on the source's file
// type. It mirrors the body of the Rust `DirectFileSystem::copy` blocking task.
func copyPath(ctx context.Context, source, destination string, opts CopyOptions) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	mode := info.Mode()

	if mode.IsDir() {
		if !opts.Recursive {
			return newInvalidInput("fs/copy requires recursive: true when sourcePath is a directory")
		}
		descendant, err := destinationIsSameOrDescendantOfSource(source, destination)
		if err != nil {
			return err
		}
		if descendant {
			return newInvalidInput("fs/copy cannot copy a directory to itself or one of its descendants")
		}
		return copyDirRecursive(ctx, source, destination)
	}

	if mode&fs.ModeSymlink != 0 {
		return copySymlink(source, destination)
	}

	if mode.IsRegular() {
		return copyFileContents(source, destination)
	}

	return newInvalidInput("fs/copy only supports regular files, directories, and symlinks")
}

// copyDirRecursive copies a directory tree, preserving symlinks as links.
//
// Rust: `copy_dir_recursive`.
func copyDirRecursive(ctx context.Context, source, target string) error {
	if err := os.MkdirAll(target, 0o777); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return wrapTaskFailure(err)
		}
		sourcePath := filepath.Join(source, entry.Name())
		targetPath := filepath.Join(target, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			if err := copyDirRecursive(ctx, sourcePath, targetPath); err != nil {
				return err
			}
		case mode.IsRegular():
			if err := copyFileContents(sourcePath, targetPath); err != nil {
				return err
			}
		case mode&fs.ModeSymlink != 0:
			if err := copySymlink(sourcePath, targetPath); err != nil {
				return err
			}
		}
		// Other file types (sockets, devices, FIFOs) are silently skipped,
		// matching the Rust recursion which only handles dir/file/symlink.
	}
	return nil
}

// copyFileContents copies a regular file's contents, mirroring std::fs::copy
// (truncating any existing destination).
func copyFileContents(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Match std::fs::copy, which also copies the permission bits.
	return os.Chmod(destination, info.Mode().Perm())
}

// copySymlink recreates a symlink at target pointing to the same location as the
// symlink at source.
//
// Rust: `copy_symlink`. On Windows the Rust code chooses a directory or file
// symlink based on the source link's target type; Go's os.Symlink determines
// this automatically, so the platform branch is unnecessary here.
func copySymlink(source, target string) error {
	linkTarget, err := os.Readlink(source)
	if err != nil {
		return err
	}
	return os.Symlink(linkTarget, target)
}

// destinationIsSameOrDescendantOfSource reports whether destination is the same
// as, or nested inside, source after resolving symlinks.
//
// Rust: `destination_is_same_or_descendant_of_source`.
func destinationIsSameOrDescendantOfSource(source, destination string) (bool, error) {
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		return false, err
	}
	resolvedDest, err := resolveExistingPath(destination)
	if err != nil {
		return false, err
	}
	return pathStartsWith(resolvedDest, resolvedSource), nil
}

// resolveExistingPath canonicalizes the longest existing prefix of path and then
// re-appends the non-existent suffix, so a destination that does not yet exist
// can still be compared against the source subtree without being fooled by
// symlinked parents.
//
// Rust: `resolve_existing_path`.
func resolveExistingPath(path string) (string, error) {
	var unresolvedSuffix []string
	existing := path
	for !pathExists(existing) {
		name := filepath.Base(existing)
		// filepath.Base returns "." for empty and the separator for root; both
		// indicate there is no further file-name component to peel off.
		if name == "." || name == string(filepath.Separator) || name == existing {
			break
		}
		unresolvedSuffix = append(unresolvedSuffix, name)
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	for i := len(unresolvedSuffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, unresolvedSuffix[i])
	}
	return resolved, nil
}

// pathExists reports whether path exists (following symlinks via Stat would
// reject dangling links; Rust uses Path::exists which follows links and returns
// false for broken links, so Stat matches that semantics).
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// pathStartsWith reports whether child is base or a descendant of base, using
// path-component boundaries to avoid matching sibling prefixes (e.g. "/a/bc" is
// not under "/a/b").
//
// Rust: `Path::starts_with`, which compares whole components.
func pathStartsWith(child, base string) bool {
	if child == base {
		return true
	}
	rel, err := filepath.Rel(base, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
