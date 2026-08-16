package protocol

// GitSha is a git commit SHA. It mirrors the Rust `codex_protocol::protocol::GitSha`
// newtype, which is `#[serde(transparent)]` over a `String`; on the wire it is a
// bare JSON string.
//
// Rust defines this type in the protocol crate and re-exports it from
// `codex-git-utils`; the Go port previously kept private copies in `gitutils`
// and `threadstore`. Both now alias this definition so that packages which only
// need the wire types (rollout, threadstore) do not pull in the git backend
// (airush-core spec 50 D0.9 seam S3).
type GitSha string

// NewGitSha constructs a GitSha from a raw SHA string. Mirrors Rust `GitSha::new`.
func NewGitSha(sha string) GitSha { return GitSha(sha) }

// String returns the underlying SHA string.
func (s GitSha) String() string { return string(s) }

// GitInfo describes the git repository state for a working directory.
//
// Mirrors the Rust `GitInfo` struct. Each field is omitted from JSON when nil
// (matching `#[serde(skip_serializing_if = "Option::is_none")]`).
type GitInfo struct {
	// CommitHash is the current commit hash (SHA), if available.
	CommitHash *GitSha `json:"commit_hash,omitempty"`
	// Branch is the current branch name, if on a branch (nil when detached).
	Branch *string `json:"branch,omitempty"`
	// RepositoryURL is the URL of the `origin` remote, if available.
	RepositoryURL *string `json:"repository_url,omitempty"`
}
