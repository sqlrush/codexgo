package local

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/threadstore"
)

// needsRolloutCompatibilityUpdate reports whether the patch requires rewriting
// rollout session-meta lines for legacy compatibility, mirroring the Rust
// `needs_rollout_compatibility_update`. A name change always qualifies (the
// name-index path); otherwise only memory-mode / git-info changes that are NOT
// part of an observed-metadata sync qualify (observed syncs are reconciled from
// the rollout transcript and must not append spurious session-meta lines).
func needsRolloutCompatibilityUpdate(patch threadstore.ThreadMetadataPatch) bool {
	if patch.Name.IsSome() {
		return true
	}
	if patch.MemoryMode == nil && patch.GitInfo == nil {
		return false
	}
	return !hasObservedMetadataFacts(patch)
}

// hasObservedMetadataFacts reports whether the patch carries any transcript-
// observed metadata facts, mirroring the Rust `has_observed_metadata_facts`.
// When present, the update is an observed-metadata sync and the rollout
// session-meta lines are reconciled from the transcript instead of appended.
func hasObservedMetadataFacts(patch threadstore.ThreadMetadataPatch) bool {
	return patch.RolloutPath != nil ||
		patch.Preview != nil ||
		patch.Title != nil ||
		patch.ModelProvider != nil ||
		patch.Model != nil ||
		patch.ReasoningEffort != nil ||
		patch.CreatedAt != nil ||
		patch.Source != nil ||
		patch.ThreadSource.IsSome() ||
		patch.AgentNickname.IsSome() ||
		patch.AgentRole.IsSome() ||
		patch.AgentPath.IsSome() ||
		patch.Cwd != nil ||
		patch.CliVersion != nil ||
		patch.ApprovalMode != nil ||
		patch.PermissionProfile != nil ||
		patch.TokenUsage != nil ||
		patch.FirstUserMessage != nil
}

// applyRolloutCompatibilityUpdate appends the rollout session-meta line(s) that
// mirror a metadata patch, after the SQLite row has been updated. It mirrors the
// rollout-compat section of the Rust `update_thread_metadata`: a memory-mode
// change rewrites the session-meta line with the new memory mode (and no git
// marker), and a git-info change rewrites it with the resolved git info plus the
// current memory mode. The two are independent and both may run for a combined
// patch. The resolved rollout path must already exist.
func (s *LocalThreadStore) applyRolloutCompatibilityUpdate(ctx context.Context, threadID protocol.ThreadID, rolloutPath string, patch threadstore.ThreadMetadataPatch) error {
	if patch.MemoryMode != nil {
		if err := applyThreadMemoryModeToRollout(rolloutPath, threadID, *patch.MemoryMode); err != nil {
			return err
		}
	}
	if patch.GitInfo != nil {
		if err := s.applyThreadGitInfoToRollout(ctx, threadID, rolloutPath); err != nil {
			return err
		}
	}
	return nil
}

// applyThreadMemoryModeToRollout reads the head session-meta line, validates the
// thread id, sets the memory mode (clearing any git marker so the replay code
// preserves the latest prior git marker), and appends the updated session-meta
// line. Mirrors the Rust `apply_thread_memory_mode`.
func applyThreadMemoryModeToRollout(rolloutPath string, threadID protocol.ThreadID, mode protocol.ThreadMemoryMode) error {
	line, err := readRolloutSessionMeta(rolloutPath, threadID, "set thread memory mode")
	if err != nil {
		return err
	}

	// Memory-mode updates must not modify git metadata; the rollout replay code
	// preserves the latest prior git marker when this field is absent.
	line.Git = nil
	value := memoryModeAsString(mode)
	line.Meta.MemoryMode = &value

	if err := rollout.AppendRolloutItemToPath(rolloutPath, rollout.NewSessionMetaItem(line)); err != nil {
		return threadstore.NewInternalError(err, "failed to set thread memory mode: %v", err)
	}
	return nil
}

// applyThreadGitInfoToRollout reads the head session-meta line, validates the
// thread id, sets the resolved git info (read back from the just-updated SQLite
// row so partial git patches preserve unspecified fields) plus the current
// memory mode, and appends the updated session-meta line. Mirrors the Rust
// `apply_thread_git_info_to_rollout`.
func (s *LocalThreadStore) applyThreadGitInfoToRollout(ctx context.Context, threadID protocol.ThreadID, rolloutPath string) error {
	if s.stateDB == nil {
		return threadstore.NewInternalError(fmt.Errorf("sqlite state db unavailable"), "sqlite state db unavailable for thread %s", threadID)
	}
	metadata, err := s.stateDB.GetThread(ctx, threadID)
	if err != nil {
		return threadstore.NewInternalError(err, "failed to read git metadata for thread %s: %v", threadID, err)
	}

	line, metaErr := readRolloutSessionMeta(rolloutPath, threadID, "set thread git metadata")
	if metaErr != nil {
		return metaErr
	}

	var (
		sha       *string
		branch    *string
		originURL *string
		memMode   *string
	)
	if metadata != nil {
		sha = cloneStr(metadata.GitSha)
		branch = cloneStr(metadata.GitBranch)
		originURL = cloneStr(metadata.GitOriginURL)
		if mode, modeErr := s.stateDB.GetThreadMemoryMode(ctx, threadID); modeErr == nil {
			memMode = cloneStr(mode)
		}
	}

	line.Git = rolloutGitInfoFromParts(sha, branch, originURL)
	line.Meta.MemoryMode = memMode

	if err := rollout.AppendRolloutItemToPath(rolloutPath, rollout.NewSessionMetaItem(line)); err != nil {
		return threadstore.NewInternalError(err, "failed to set thread git metadata: %v", err)
	}
	return nil
}

// readRolloutSessionMeta reads the head session-meta line and validates that its
// id matches threadID, returning an internal error with the supplied action in
// the message on a mismatch or read failure (mirroring the Rust error strings).
func readRolloutSessionMeta(rolloutPath string, threadID protocol.ThreadID, action string) (rollout.SessionMetaLine, error) {
	line, err := rollout.ReadSessionMetaLine(rolloutPath)
	if err != nil {
		return rollout.SessionMetaLine{}, threadstore.NewInternalError(err, "failed to %s: %v", action, err)
	}
	if line.Meta.ID != threadID {
		mismatch := fmt.Errorf("rollout session metadata id mismatch: expected %s, found %s", threadID, line.Meta.ID)
		return rollout.SessionMetaLine{}, threadstore.NewInternalError(mismatch, "failed to %s: %v", action, mismatch)
	}
	return line, nil
}

// rolloutGitInfoFromParts builds the rollout git marker from individual parts,
// returning an empty (all-nil) threadstore.GitInfo when every part is absent so the appended
// session-meta line carries `"git":{}` (a git clear), mirroring the Rust
// `apply_thread_git_info_to_rollout` which always sets `session_meta.git =
// Some(threadstore.GitInfo { .. })`.
func rolloutGitInfoFromParts(sha, branch, originURL *string) *protocol.GitInfo {
	var commit *protocol.GitSha
	if sha != nil {
		c := protocol.NewGitSha(*sha)
		commit = &c
	}
	return &protocol.GitInfo{
		CommitHash:    commit,
		Branch:        branch,
		RepositoryURL: originURL,
	}
}
