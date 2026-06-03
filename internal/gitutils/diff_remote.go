package gitutils

import (
	"context"
	"strconv"
	"strings"
)

// GitDiffToRemoteResult returns the closest git SHA to HEAD that also exists on
// a remote, together with the unified diff from that SHA to the current working
// tree (including untracked files). It returns nil when cwd is not in a git
// repository or no suitable remote SHA can be found.
//
// Mirrors the Rust `git_diff_to_remote`. The diff text is produced by the system
// `git` binary to guarantee byte-for-byte parity with codex.
func GitDiffToRemoteResult(ctx context.Context, cwd string) *GitDiffToRemote {
	if _, ok := GetGitRepoRoot(cwd); !ok {
		return nil
	}

	remotes, ok := gitRemotes(ctx, cwd)
	if !ok {
		return nil
	}
	branches := branchAncestry(ctx, cwd)
	baseSha, ok := findClosestSha(ctx, cwd, branches, remotes)
	if !ok {
		return nil
	}
	diff, ok := diffAgainstSha(ctx, cwd, baseSha)
	if !ok {
		return nil
	}

	return &GitDiffToRemote{Sha: baseSha, Diff: diff}
}

// gitRemotes returns the remote names with `origin` prioritized first, using the
// git binary so the ordering and parsing match the Rust crate exactly.
//
// Mirrors the Rust `get_git_remotes`.
func gitRemotes(ctx context.Context, cwd string) ([]string, bool) {
	out, ok := runGitWithTimeout(ctx, cwd, "remote")
	if !ok || !out.success {
		return nil, false
	}
	remotes := nonEmptyLines(string(out.stdout))
	for i, r := range remotes {
		if r == "origin" {
			remotes = append(remotes[:i], remotes[i+1:]...)
			remotes = append([]string{"origin"}, remotes...)
			break
		}
	}
	return remotes, true
}

// branchAncestry builds an ancestry of branches starting at the current branch
// and ending at the repository's default branch, then expands it with any remote
// branches that already contain HEAD.
//
// Mirrors the Rust `branch_ancestry`.
func branchAncestry(ctx context.Context, cwd string) []string {
	currentBranch := ""
	if out, ok := runGitWithTimeout(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD"); ok && out.success {
		trimmed := strings.TrimSpace(string(out.stdout))
		if trimmed != "HEAD" {
			currentBranch = trimmed
		}
	}

	defaultBranch, _ := defaultBranchViaGit(ctx, cwd)

	ancestry := make([]string, 0)
	seen := make(map[string]struct{})
	if currentBranch != "" {
		seen[currentBranch] = struct{}{}
		ancestry = append(ancestry, currentBranch)
	}
	if defaultBranch != "" {
		if _, dup := seen[defaultBranch]; !dup {
			seen[defaultBranch] = struct{}{}
			ancestry = append(ancestry, defaultBranch)
		}
	}

	remotes, _ := gitRemotes(ctx, cwd)
	for _, remote := range remotes {
		out, ok := runGitWithTimeout(ctx, cwd,
			"for-each-ref", "--format=%(refname:short)", "--contains=HEAD",
			"refs/remotes/"+remote)
		if !ok || !out.success {
			continue
		}
		prefix := remote + "/"
		for _, line := range strings.Split(string(out.stdout), "\n") {
			short := strings.TrimSpace(line)
			stripped, found := strings.CutPrefix(short, prefix)
			if !found || stripped == "" {
				continue
			}
			if _, dup := seen[stripped]; dup {
				continue
			}
			seen[stripped] = struct{}{}
			ancestry = append(ancestry, stripped)
		}
	}

	return ancestry
}

// defaultBranchViaGit resolves the default branch using the git binary,
// mirroring the Rust `get_default_branch` (remote symbolic-ref, then
// `git remote show`, then local main/master).
func defaultBranchViaGit(ctx context.Context, cwd string) (string, bool) {
	remotes, _ := gitRemotes(ctx, cwd)
	for _, remote := range remotes {
		if out, ok := runGitWithTimeout(ctx, cwd,
			"symbolic-ref", "--quiet", "refs/remotes/"+remote+"/HEAD"); ok && out.success {
			trimmed := strings.TrimSpace(string(out.stdout))
			if idx := strings.LastIndexByte(trimmed, '/'); idx >= 0 {
				return trimmed[idx+1:], true
			}
		}

		if out, ok := runGitWithTimeout(ctx, cwd, "remote", "show", remote); ok && out.success {
			for _, line := range strings.Split(string(out.stdout), "\n") {
				line = strings.TrimSpace(line)
				if rest, found := strings.CutPrefix(line, "HEAD branch:"); found {
					name := strings.TrimSpace(rest)
					if name != "" {
						return name, true
					}
				}
			}
		}
	}

	return defaultBranchLocalViaGit(ctx, cwd)
}

// defaultBranchLocalViaGit returns the first of main/master verified to exist
// via the git binary. Mirrors the Rust `get_default_branch_local`.
func defaultBranchLocalViaGit(ctx context.Context, cwd string) (string, bool) {
	for _, candidate := range []string{"main", "master"} {
		if out, ok := runGitWithTimeout(ctx, cwd,
			"rev-parse", "--verify", "--quiet", "refs/heads/"+candidate); ok && out.success {
			return candidate, true
		}
	}
	return "", false
}

// branchRemoteAndDistance returns the remote SHA for branch (if it exists on any
// remote) and how many commits HEAD is ahead of branch. It returns ok=false when
// the distance could not be computed.
//
// Mirrors the Rust `branch_remote_and_distance`.
func branchRemoteAndDistance(ctx context.Context, cwd, branch string, remotes []string) (remoteSha GitSha, hasRemote bool, distance int, ok bool) {
	var foundRemoteRef string
	for _, remote := range remotes {
		remoteRef := "refs/remotes/" + remote + "/" + branch
		verify, started := runGitWithTimeout(ctx, cwd, "rev-parse", "--verify", "--quiet", remoteRef)
		if !started {
			// Process-level failure: treat the entire branch as unusable.
			return "", false, 0, false
		}
		if !verify.success {
			continue
		}
		remoteSha = NewGitSha(strings.TrimSpace(string(verify.stdout)))
		hasRemote = true
		foundRemoteRef = remoteRef
		break
	}

	// Distance: commits HEAD is ahead of the branch. Prefer the local branch
	// name; fall back to the remote ref if the local count fails.
	count, started := runGitWithTimeout(ctx, cwd, "rev-list", "--count", branch+"..HEAD")
	switch {
	case started && count.success:
		// use count
	case foundRemoteRef != "":
		rc, rcStarted := runGitWithTimeout(ctx, cwd, "rev-list", "--count", foundRemoteRef+"..HEAD")
		if !rcStarted {
			return "", false, 0, false
		}
		count = rc
	default:
		return "", false, 0, false
	}

	if !count.success {
		return "", false, 0, false
	}
	distance, err := strconv.Atoi(strings.TrimSpace(string(count.stdout)))
	if err != nil {
		return "", false, 0, false
	}
	return remoteSha, hasRemote, distance, true
}

// findClosestSha finds the SHA, among the given branches, that exists on a
// remote and is closest to HEAD.
//
// Mirrors the Rust `find_closest_sha`.
func findClosestSha(ctx context.Context, cwd string, branches, remotes []string) (GitSha, bool) {
	var closest GitSha
	bestDistance := -1
	for _, branch := range branches {
		remoteSha, hasRemote, distance, ok := branchRemoteAndDistance(ctx, cwd, branch, remotes)
		if !ok || !hasRemote {
			continue
		}
		if bestDistance < 0 || distance < bestDistance {
			closest = remoteSha
			bestDistance = distance
		}
	}
	if bestDistance < 0 {
		return "", false
	}
	return closest, true
}

// diffAgainstSha returns the working-tree diff against sha, appending per-file
// diffs for untracked files.
//
// Mirrors the Rust `diff_against_sha`. Exit codes 0 (no diff) and 1 (diff
// present) are both treated as success.
func diffAgainstSha(ctx context.Context, cwd string, sha GitSha) (string, bool) {
	out, started := runGitWithTimeout(ctx, cwd,
		"diff", "--no-textconv", "--no-ext-diff", sha.String())
	if !started {
		return "", false
	}
	if !diffExitOK(out.exitCode) {
		return "", false
	}
	diff := string(out.stdout)

	untrackedOut, started := runGitWithTimeout(ctx, cwd,
		"ls-files", "--others", "--exclude-standard")
	if started && untrackedOut.success {
		untracked := nonEmptyLines(string(untrackedOut.stdout))
		nullDev := nullDevice()
		for _, file := range untracked {
			extra, extraStarted := runGitWithTimeout(ctx, cwd,
				"diff", "--no-textconv", "--no-ext-diff", "--binary", "--no-index",
				"--", nullDev, file)
			if !extraStarted {
				continue
			}
			if diffExitOK(extra.exitCode) {
				diff += string(extra.stdout)
			}
		}
	}

	return diff, true
}

// diffExitOK reports whether a `git diff` exit code indicates success: 0 means
// no diff, 1 means a diff is present.
func diffExitOK(code int) bool { return code == 0 || code == 1 }

// nonEmptyLines splits s on newlines and drops empty lines.
func nonEmptyLines(s string) []string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
