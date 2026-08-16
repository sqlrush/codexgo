// Package gitroot locates git repository roots by walking the filesystem for a
// `.git` entry, including the linked-worktree resolution used for project trust.
// It needs neither the git binary nor a git library, so packages that only need
// "where is the repo root" (config trust, rollout local recorder) import it
// without pulling in the go-git backend that the rest of gitutils uses.
// Mirrors the root-discovery functions of the Rust codex-git-utils crate.
package gitroot

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
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

// ResolveRootGitProjectForTrust resolves the repository root used for project
// trust decisions, mirroring the Rust `resolve_root_git_project_for_trust`
// (git-utils/src/info.rs).
//
// For a regular checkout (where `<repo_root>/.git` is a directory) it returns
// the repo root. For a linked worktree (where `<repo_root>/.git` is a file
// containing `gitdir: <path>`), it resolves the pointer to the worktree's git
// directory, verifies the `.../worktrees/<name>` shape, and returns the main
// checkout root (the grandparent of the `worktrees` directory). It returns
// ok=false when cwd is not inside a git repo or the pointer shape is
// unexpected. The check does not require the `git` binary.
func ResolveRootGitProjectForTrust(cwd abspath.AbsolutePathBuf) (abspath.AbsolutePathBuf, bool) {
	repoRootStr, ok := GetGitRepoRoot(cwd.String())
	if !ok {
		return abspath.AbsolutePathBuf{}, false
	}
	repoRoot, err := abspath.FromAbsolutePathChecked(repoRootStr)
	if err != nil {
		return abspath.AbsolutePathBuf{}, false
	}

	dotGit := repoRoot.Join(".git")
	if info, statErr := os.Stat(dotGit.String()); statErr == nil && info.IsDir() {
		return repoRoot, true
	}

	contents, readErr := os.ReadFile(dotGit.String())
	if readErr != nil {
		return abspath.AbsolutePathBuf{}, false
	}
	rel, found := strings.CutPrefix(strings.TrimSpace(string(contents)), "gitdir:")
	if !found {
		return abspath.AbsolutePathBuf{}, false
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return abspath.AbsolutePathBuf{}, false
	}

	gitDirPath := abspath.ResolvePathAgainstBase(rel, repoRoot.String())
	worktreesDir, ok := gitDirPath.Parent()
	if !ok {
		return abspath.AbsolutePathBuf{}, false
	}
	if filepath.Base(worktreesDir.String()) != "worktrees" {
		return abspath.AbsolutePathBuf{}, false
	}
	commonDir, ok := worktreesDir.Parent()
	if !ok {
		return abspath.AbsolutePathBuf{}, false
	}
	mainRoot, ok := commonDir.Parent()
	if !ok {
		return abspath.AbsolutePathBuf{}, false
	}
	return mainRoot, true
}

// FindGitCheckoutRoot returns the directory of the nearest `.git` entry at or
// above cwd, mirroring the Rust `find_git_checkout_root` used by project trust.
// Unlike [ResolveRootGitProjectForTrust] it does not follow worktree pointers:
// it returns the directory that physically contains `.git` (file or directory).
// It returns ok=false when no `.git` entry is found.
func FindGitCheckoutRoot(cwd abspath.AbsolutePathBuf) (abspath.AbsolutePathBuf, bool) {
	base := cwd
	if !isDir(cwd.String()) {
		parent, ok := cwd.Parent()
		if !ok {
			return abspath.AbsolutePathBuf{}, false
		}
		base = parent
	}
	for _, dir := range base.Ancestors() {
		if _, err := os.Stat(dir.Join(".git").String()); err == nil {
			return dir, true
		}
	}
	return abspath.AbsolutePathBuf{}, false
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
