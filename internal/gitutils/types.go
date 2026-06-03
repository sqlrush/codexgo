// Package gitutils is a faithful, drop-in-compatible Go port of the Rust
// `codex-git-utils` crate. It collects git repository information (commit hash,
// branch, remote URL), discovers the repository root, reports whether the
// working tree has changes, computes the diff against the closest remote SHA,
// lists recent commits, and applies unified diffs to a repository with
// staged-path tracking.
//
// Read-only inspection (info collection, repo-root discovery, has-changes,
// recent commits) is implemented with go-git. Patch application mirrors the
// Rust crate which shells out to the system `git` binary for `git apply
// --3way`; go-git has no faithful equivalent of 3-way `git apply`, so that
// behaviour is preserved by invoking `git`.
package gitutils

// GitSha is a git commit SHA. It mirrors the Rust `codex_protocol::protocol::GitSha`
// newtype, which is `#[serde(transparent)]` over a `String`; on the wire it is a
// bare JSON string.
//
// The Go protocol package does not (yet) define this type, so it is defined here
// to match the Rust re-export `pub use codex_protocol::protocol::GitSha`.
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

// GitDiffToRemote pairs the closest remote SHA reachable from HEAD with the
// unified diff from that SHA to the current working tree.
//
// Mirrors the Rust `GitDiffToRemote` struct.
type GitDiffToRemote struct {
	Sha  GitSha `json:"sha"`
	Diff string `json:"diff"`
}

// CommitLogEntry is a minimal commit summary used for pickers: subject,
// timestamp, and SHA.
//
// Mirrors the Rust `CommitLogEntry` struct.
type CommitLogEntry struct {
	Sha string `json:"sha"`
	// Timestamp is the Unix timestamp (seconds since epoch) of the committer time.
	Timestamp int64 `json:"timestamp"`
	// Subject is the single-line subject of the commit message.
	Subject string `json:"subject"`
}
