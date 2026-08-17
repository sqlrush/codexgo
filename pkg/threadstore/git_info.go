package threadstore

import "github.com/sqlrush/codexgo/pkg/protocol"

// GitSha is a git commit SHA. The canonical definition lives in the protocol
// package (mirroring Rust `codex_protocol::protocol::GitSha`); the alias keeps
// the historical `threadstore.GitSha` spelling working.
type GitSha = protocol.GitSha

// NewGitSha constructs a GitSha from a raw SHA string.
func NewGitSha(sha string) GitSha { return protocol.NewGitSha(sha) }

// GitInfo describes the git repository state captured for a thread. See
// protocol.GitInfo for the canonical definition and wire format.
type GitInfo = protocol.GitInfo

// GitInfoFromParts builds a GitInfo from individual parts, returning nil when all
// parts are absent, mirroring the Rust `git_info_from_parts` helper.
func GitInfoFromParts(sha, branch, originURL *string) *GitInfo {
	if sha == nil && branch == nil && originURL == nil {
		return nil
	}
	var commit *GitSha
	if sha != nil {
		c := GitSha(*sha)
		commit = &c
	}
	return &GitInfo{
		CommitHash:    commit,
		Branch:        branch,
		RepositoryURL: originURL,
	}
}

// gitInfoFromPatch reconstructs a GitInfo from a [GitInfoPatch], returning nil
// when no concrete value is set, mirroring the Rust `git_info_from_patch` helper
// used by the in-memory store.
func gitInfoFromPatch(patch *GitInfoPatch) *GitInfo {
	if patch == nil {
		return nil
	}
	sha := flatten(patch.SHA)
	branch := flatten(patch.Branch)
	originURL := flatten(patch.OriginURL)
	if sha == nil && branch == nil && originURL == nil {
		return nil
	}
	return GitInfoFromParts(sha, branch, originURL)
}
