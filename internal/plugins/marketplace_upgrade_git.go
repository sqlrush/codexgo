package plugins

// Real git client for the configured-marketplace upgrade, porting the git
// helpers of the Rust `core-plugins/src/marketplace_upgrade/git.rs`.

import (
	"fmt"
	"strings"
	"time"
)

// realMarketplaceGitClient drives the real `git` binary. It implements
// [MarketplaceGitClient].
type realMarketplaceGitClient struct{}

var _ MarketplaceGitClient = realMarketplaceGitClient{}

// RemoteRevision resolves the revision source+refName points to, mirroring
// Rust's `git_remote_revision`. A full 40-hex ref is returned as-is; otherwise
// the ref (defaulting to HEAD) is resolved with `git ls-remote`.
func (realMarketplaceGitClient) RemoteRevision(source string, refName *string, timeout time.Duration) (string, error) {
	if refName != nil && isFullGitSHA(*refName) {
		return *refName, nil
	}
	ref := "HEAD"
	if refName != nil {
		ref = *refName
	}
	out, err := runGitWithTimeout(timeout, "git", "ls-remote", source, ref)
	if err != nil {
		return "", fmt.Errorf("git ls-remote marketplace source: %w", err)
	}
	line := firstLine(string(out))
	if line == "" {
		return "", fmt.Errorf("git ls-remote returned empty output for marketplace source")
	}
	revision, _, found := strings.Cut(line, "\t")
	if !found {
		return "", fmt.Errorf("unexpected git ls-remote output for marketplace source: %s", line)
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "", fmt.Errorf("git ls-remote returned empty revision for marketplace source")
	}
	return revision, nil
}

// CloneSource clones source into destination, mirroring Rust's
// `clone_git_source`. Without sparse paths it clones fully (optionally checking
// out refName); with sparse paths it does a blob-less no-checkout clone, sets
// the sparse cone, and checks out the ref. It returns the worktree HEAD revision.
func (realMarketplaceGitClient) CloneSource(source string, refName *string, sparsePaths []string, destination string, timeout time.Duration) (string, error) {
	if len(sparsePaths) == 0 {
		if _, err := runGitWithTimeout(timeout, "git", "clone", source, destination); err != nil {
			return "", fmt.Errorf("git clone marketplace source: %w", err)
		}
		if refName != nil {
			if _, err := runGitWithTimeout(timeout, "git", "-C", destination, "checkout", *refName); err != nil {
				return "", fmt.Errorf("git checkout marketplace ref: %w", err)
			}
		}
		return gitWorktreeRevision(destination, timeout)
	}

	if _, err := runGitWithTimeout(timeout, "git", "clone", "--filter=blob:none", "--no-checkout", source, destination); err != nil {
		return "", fmt.Errorf("git clone marketplace source: %w", err)
	}
	args := append([]string{"-C", destination, "sparse-checkout", "set"}, sparsePaths...)
	if _, err := runGitWithTimeout(timeout, "git", args...); err != nil {
		return "", fmt.Errorf("git sparse-checkout set: %w", err)
	}
	checkoutRef := "HEAD"
	if refName != nil {
		checkoutRef = *refName
	}
	if _, err := runGitWithTimeout(timeout, "git", "-C", destination, "checkout", checkoutRef); err != nil {
		return "", fmt.Errorf("git checkout marketplace ref: %w", err)
	}
	return gitWorktreeRevision(destination, timeout)
}

// gitWorktreeRevision returns the HEAD revision of the checked-out worktree,
// mirroring Rust's `git_worktree_revision`.
func gitWorktreeRevision(destination string, timeout time.Duration) (string, error) {
	out, err := runGitWithTimeout(timeout, "git", "-C", destination, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD marketplace worktree: %w", err)
	}
	revision := strings.TrimSpace(string(out))
	if revision == "" {
		return "", fmt.Errorf("git rev-parse HEAD returned empty revision for marketplace worktree")
	}
	return revision, nil
}

// isFullGitSHA reports whether s is a 40-character lowercase-or-uppercase hex
// git object id, mirroring Rust's `is_full_git_sha`.
func isFullGitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
