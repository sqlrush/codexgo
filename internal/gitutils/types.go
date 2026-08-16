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

import "github.com/sqlrush/codexgo/internal/protocol"

// GitSha is a git commit SHA. The canonical definition lives in the protocol
// package (mirroring the Rust re-export `pub use codex_protocol::protocol::GitSha`);
// this alias keeps the historical `gitutils.GitSha` spelling working.
type GitSha = protocol.GitSha

// NewGitSha constructs a GitSha from a raw SHA string. Mirrors Rust `GitSha::new`.
func NewGitSha(sha string) GitSha { return protocol.NewGitSha(sha) }

// GitInfo describes the git repository state for a working directory. See
// protocol.GitInfo for the canonical definition and wire format.
type GitInfo = protocol.GitInfo

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
