package gitutils

import (
	"os"
	"path/filepath"
)

// GetGitRepoRoot returns the repository root for baseDir by walking up the
// directory hierarchy looking for a `.git` entry (file or directory), or false
// if none is found.
//
// Mirrors the Rust `get_git_repo_root`. The check does not require the `git`
// binary. Note that, like the Rust version, it does not detect work-trees whose
// checkout lives outside the main repository directory.
func GetGitRepoRoot(baseDir string) (string, bool) {
	base := baseDir
	if !isDir(baseDir) {
		parent := filepath.Dir(baseDir)
		if parent == baseDir {
			return "", false
		}
		base = parent
	}
	root, _, ok := findAncestorGitEntry(base)
	return root, ok
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// findAncestorGitEntry walks up from baseDir, returning the first directory that
// contains a `.git` entry along with the `.git` path itself.
//
// Mirrors the Rust `find_ancestor_git_entry`.
func findAncestorGitEntry(baseDir string) (repoRoot string, dotGit string, found bool) {
	dir := baseDir
	for {
		candidate := filepath.Join(dir, ".git")
		if _, err := os.Lstat(candidate); err == nil {
			return dir, candidate, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root.
			break
		}
		dir = parent
	}
	return "", "", false
}
