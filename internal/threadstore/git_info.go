package threadstore

// GitSha is a git commit SHA. It mirrors the Rust `GitSha` newtype, which is
// `#[serde(transparent)]` over a String; on the wire it is a bare JSON string.
//
// The allowed dependency set for this package does not include the git-utils
// package, so the type is reproduced here to keep the wire format identical.
type GitSha string

// NewGitSha constructs a GitSha from a raw SHA string.
func NewGitSha(sha string) GitSha { return GitSha(sha) }

// String returns the underlying SHA string.
func (s GitSha) String() string { return string(s) }

// GitInfo describes the git repository state captured for a thread.
//
// Mirrors the Rust `GitInfo` struct. Each field is omitted from JSON when nil
// (matching `#[serde(skip_serializing_if = "Option::is_none")]`).
type GitInfo struct {
	// CommitHash is the current commit hash (SHA), if available.
	CommitHash *GitSha `json:"commit_hash,omitempty"`
	// Branch is the current branch name, if on a branch.
	Branch *string `json:"branch,omitempty"`
	// RepositoryURL is the URL of the origin remote, if available.
	RepositoryURL *string `json:"repository_url,omitempty"`
}

// gitInfoFromParts builds a GitInfo from individual parts, returning nil when all
// parts are absent, mirroring the Rust `git_info_from_parts` helper.
func gitInfoFromParts(sha, branch, originURL *string) *GitInfo {
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
	return gitInfoFromParts(sha, branch, originURL)
}
