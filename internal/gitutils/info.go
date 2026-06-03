package gitutils

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// openRepo opens the git repository containing cwd, walking up parent
// directories to find the `.git` entry (mirroring git's discovery behaviour).
// It returns false-friendly errors so callers can degrade to None like the Rust
// crate does.
func openRepo(cwd string) (*git.Repository, error) {
	repo, err := git.PlainOpenWithOptions(cwd, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("open git repository at %q: %w", cwd, err)
	}
	return repo, nil
}

// CollectGitInfo collects git repository information (commit hash, branch, and
// origin remote URL) for cwd. It returns nil when cwd is not inside a git
// repository.
//
// Mirrors the Rust `collect_git_info`. Each field is best-effort: a failure to
// resolve any individual field leaves it unset rather than failing the whole
// call. The context is honoured for cancellation.
func CollectGitInfo(ctx context.Context, cwd string) *GitInfo {
	if err := ctx.Err(); err != nil {
		return nil
	}
	repo, err := openRepo(cwd)
	if err != nil {
		return nil
	}

	info := &GitInfo{}

	// Commit hash: HEAD's resolved SHA.
	if head, err := repo.Head(); err == nil {
		sha := NewGitSha(head.Hash().String())
		info.CommitHash = &sha

		// Branch name: only when HEAD points at a branch (not detached).
		if head.Name().IsBranch() {
			branch := head.Name().Short()
			if branch != "" && branch != "HEAD" {
				info.Branch = &branch
			}
		}
	}

	// Repository URL: first fetch URL of the `origin` remote, if present.
	if url, ok := originURL(repo); ok {
		info.RepositoryURL = &url
	}

	return info
}

// originURL returns the first URL of the `origin` remote.
func originURL(repo *git.Repository) (string, bool) {
	remote, err := repo.Remote("origin")
	if err != nil {
		return "", false
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", false
	}
	url := strings.TrimSpace(urls[0])
	if url == "" {
		return "", false
	}
	return url, true
}

// GetGitRemoteURLs returns the fetch remotes for cwd as a map of name to URL,
// or nil when cwd is not in a git repository or no remotes are configured.
//
// Mirrors the Rust `get_git_remote_urls`.
func GetGitRemoteURLs(ctx context.Context, cwd string) map[string]string {
	if err := ctx.Err(); err != nil {
		return nil
	}
	repo, err := openRepo(cwd)
	if err != nil {
		return nil
	}
	return remoteURLs(repo)
}

func remoteURLs(repo *git.Repository) map[string]string {
	remotes, err := repo.Remotes()
	if err != nil {
		return nil
	}
	result := make(map[string]string)
	for _, remote := range remotes {
		cfg := remote.Config()
		if len(cfg.URLs) == 0 {
			continue
		}
		url := strings.TrimSpace(cfg.URLs[0])
		if url != "" {
			result[cfg.Name] = url
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// GetHeadCommitHash returns the current HEAD commit hash for cwd, or false when
// HEAD cannot be resolved (e.g. an empty repository or non-repository path).
//
// Mirrors the Rust `get_head_commit_hash`.
func GetHeadCommitHash(ctx context.Context, cwd string) (GitSha, bool) {
	if err := ctx.Err(); err != nil {
		return "", false
	}
	repo, err := openRepo(cwd)
	if err != nil {
		return "", false
	}
	head, err := repo.Head()
	if err != nil {
		return "", false
	}
	sha := head.Hash().String()
	if sha == "" {
		return "", false
	}
	return NewGitSha(sha), true
}

// GetHasChanges reports whether cwd's working tree has any changes (staged,
// unstaged, or untracked). It returns false (and ok=false) when cwd is not a
// git repository or the status cannot be computed.
//
// Mirrors the Rust `get_has_changes`, which checks `git status --porcelain`.
func GetHasChanges(ctx context.Context, cwd string) (changed bool, ok bool) {
	if err := ctx.Err(); err != nil {
		return false, false
	}
	repo, err := openRepo(cwd)
	if err != nil {
		return false, false
	}
	wt, err := repo.Worktree()
	if err != nil {
		return false, false
	}
	status, err := wt.Status()
	if err != nil {
		return false, false
	}
	return !status.IsClean(), true
}

// CurrentBranchName returns the name of the currently checked-out branch, or
// false when HEAD is detached or cwd is not a git repository.
//
// Mirrors the Rust `current_branch_name`, which runs `git branch --show-current`.
func CurrentBranchName(ctx context.Context, cwd string) (string, bool) {
	if err := ctx.Err(); err != nil {
		return "", false
	}
	repo, err := openRepo(cwd)
	if err != nil {
		return "", false
	}
	head, err := repo.Head()
	if err != nil {
		return "", false
	}
	if !head.Name().IsBranch() {
		return "", false
	}
	name := head.Name().Short()
	if name == "" {
		return "", false
	}
	return name, true
}

// LocalGitBranches returns the local branches for cwd, sorted, with the default
// branch (`main` or `master`, whichever exists) moved to the front when present.
//
// Mirrors the Rust `local_git_branches`.
func LocalGitBranches(ctx context.Context, cwd string) []string {
	if err := ctx.Err(); err != nil {
		return nil
	}
	repo, err := openRepo(cwd)
	if err != nil {
		return nil
	}
	iter, err := repo.Branches()
	if err != nil {
		return nil
	}

	branches := make([]string, 0)
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		name := strings.TrimSpace(ref.Name().Short())
		if name != "" {
			branches = append(branches, name)
		}
		return nil
	})

	sort.Strings(branches)

	if base, ok := defaultBranchLocal(repo); ok {
		for i, name := range branches {
			if name == base {
				branches = append(branches[:i], branches[i+1:]...)
				branches = append([]string{base}, branches...)
				break
			}
		}
	}

	return branches
}

// DefaultBranchName attempts to determine the repository's default branch name.
//
// Mirrors the Rust `default_branch_name`. Preference order: the symbolic ref at
// `refs/remotes/<remote>/HEAD` (origin first), then a local fallback to `main`
// or `master` when present.
func DefaultBranchName(ctx context.Context, cwd string) (string, bool) {
	if err := ctx.Err(); err != nil {
		return "", false
	}
	repo, err := openRepo(cwd)
	if err != nil {
		return "", false
	}
	return defaultBranch(repo)
}

// defaultBranch resolves the default branch via remote HEAD symbolic refs, then
// falls back to local `main`/`master`.
func defaultBranch(repo *git.Repository) (string, bool) {
	for _, remote := range orderedRemotes(repo) {
		refName := plumbing.ReferenceName(fmt.Sprintf("refs/remotes/%s/HEAD", remote))
		ref, err := repo.Reference(refName, false)
		if err != nil {
			continue
		}
		// A symbolic ref resolves to e.g. refs/remotes/origin/main.
		target := ref.Target().String()
		if idx := strings.LastIndexByte(target, '/'); idx >= 0 {
			name := target[idx+1:]
			if name != "" {
				return name, true
			}
		}
	}
	return defaultBranchLocal(repo)
}

// defaultBranchLocal returns the first of `main`/`master` that exists locally.
//
// Mirrors the Rust `get_default_branch_local`.
func defaultBranchLocal(repo *git.Repository) (string, bool) {
	for _, candidate := range []string{"main", "master"} {
		refName := plumbing.NewBranchReferenceName(candidate)
		if _, err := repo.Reference(refName, true); err == nil {
			return candidate, true
		}
	}
	return "", false
}

// orderedRemotes returns the remote names with `origin` prioritized first.
//
// Mirrors the Rust `get_git_remotes` ordering.
func orderedRemotes(repo *git.Repository) []string {
	remotes, err := repo.Remotes()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(remotes))
	for _, remote := range remotes {
		names = append(names, remote.Config().Name)
	}
	for i, name := range names {
		if name == "origin" {
			names = append(names[:i], names[i+1:]...)
			names = append([]string{"origin"}, names...)
			break
		}
	}
	return names
}

// RecentCommits returns the last `limit` commits reachable from HEAD. Each entry
// contains the SHA, committer timestamp (seconds), and subject line. A limit of
// 0 means no limit. It returns an empty slice when cwd is not a git repository
// or on error.
//
// Mirrors the Rust `recent_commits`.
func RecentCommits(ctx context.Context, cwd string, limit int) []CommitLogEntry {
	if err := ctx.Err(); err != nil {
		return []CommitLogEntry{}
	}
	repo, err := openRepo(cwd)
	if err != nil {
		return []CommitLogEntry{}
	}

	iter, err := repo.Log(&git.LogOptions{Order: git.LogOrderCommitterTime})
	if err != nil {
		return []CommitLogEntry{}
	}
	defer iter.Close()

	entries := make([]CommitLogEntry, 0)
	err = iter.ForEach(func(c *object.Commit) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entries = append(entries, CommitLogEntry{
			Sha:       c.Hash.String(),
			Timestamp: c.Committer.When.Unix(),
			Subject:   commitSubject(c.Message),
		})
		if limit > 0 && len(entries) >= limit {
			return errStopIter
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopIter) {
		return []CommitLogEntry{}
	}

	return entries
}

// errStopIter is a sentinel used to break out of go-git iterators early.
var errStopIter = errors.New("gitutils: stop iteration")

// commitSubject extracts the single-line subject from a commit message,
// matching git's `%s` pretty format (first line, trimmed).
func commitSubject(message string) string {
	subject := message
	if idx := strings.IndexByte(message, '\n'); idx >= 0 {
		subject = message[:idx]
	}
	return strings.TrimSpace(subject)
}
