package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/state"
	"github.com/sqlrush/codexgo/internal/threadstore"
)

// loadHistory resolves the rollout path for the thread and returns its persisted
// rollout items, mirroring the Rust `load_history`.
func (s *LocalThreadStore) loadHistory(ctx context.Context, params threadstore.LoadThreadHistoryParams) (threadstore.StoredThreadHistory, error) {
	path, err := s.resolveRolloutPath(ctx, params.ThreadID, params.IncludeArchived)
	if err != nil {
		return threadstore.StoredThreadHistory{}, err
	}
	if path == "" {
		return threadstore.StoredThreadHistory{}, threadstore.NewInvalidRequestError("no rollout found for thread id %s", params.ThreadID)
	}
	items, err := loadHistoryItems(path)
	if err != nil {
		return threadstore.StoredThreadHistory{}, err
	}
	return threadstore.StoredThreadHistory{ThreadID: params.ThreadID, Items: items}, nil
}

// archiveThread moves the thread's rollout file from the active sessions tree to
// the archived-sessions tree and, when a state DB is present, flips the archived
// flag/timestamp, mirroring the Rust `archive_thread`.
func (s *LocalThreadStore) archiveThread(ctx context.Context, params threadstore.ArchiveThreadParams) error {
	threadID := params.ThreadID
	activePath, err := rollout.FindThreadPathByIDStr(s.config.CodexHome, threadID.String())
	if err != nil {
		return threadstore.NewInvalidRequestError("failed to locate thread id %s: %v", threadID, err)
	}
	if activePath == "" {
		return threadstore.NewInvalidRequestError("no rollout found for thread id %s", threadID)
	}

	archiveDir := filepath.Join(s.config.CodexHome, rollout.ArchivedSessionsSubdir)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return threadstore.NewInternalError(err, "failed to archive thread")
	}
	archivedPath := filepath.Join(archiveDir, filepath.Base(activePath))
	if err := os.Rename(activePath, archivedPath); err != nil {
		return threadstore.NewInternalError(err, "failed to archive thread")
	}

	if s.stateDB != nil {
		at := time.Now().UTC()
		s.applyArchiveState(ctx, threadID, archivedPath, &at)
	}
	return nil
}

// unarchiveThread reverses an archive: it moves the rollout file back into the
// dated active sessions tree, clears the archived state in the DB (when present),
// and returns the restored threadstore.StoredThread, mirroring the Rust `unarchive_thread`.
func (s *LocalThreadStore) unarchiveThread(ctx context.Context, params threadstore.ArchiveThreadParams) (threadstore.StoredThread, error) {
	threadID := params.ThreadID
	archivedPath, err := rollout.FindArchivedThreadPathByIDStr(s.config.CodexHome, threadID.String())
	if err != nil {
		return threadstore.StoredThread{}, threadstore.NewInvalidRequestError("failed to locate archived thread id %s: %v", threadID, err)
	}
	if archivedPath == "" {
		return threadstore.StoredThread{}, threadstore.NewInvalidRequestError("no archived rollout found for thread id %s", threadID)
	}

	fileName := filepath.Base(archivedPath)
	year, month, day, ok := rollout.RolloutDateParts(fileName)
	if !ok {
		return threadstore.StoredThread{}, threadstore.NewInvalidRequestError("rollout path %q missing filename timestamp", archivedPath)
	}

	destDir := filepath.Join(s.config.CodexHome, rollout.SessionsSubdir, year, month, day)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return threadstore.StoredThread{}, threadstore.NewInternalError(err, "failed to unarchive thread")
	}
	restoredPath := filepath.Join(destDir, fileName)
	if err := os.Rename(archivedPath, restoredPath); err != nil {
		return threadstore.StoredThread{}, threadstore.NewInternalError(err, "failed to unarchive thread")
	}
	if err := touchModifiedTime(restoredPath); err != nil {
		return threadstore.StoredThread{}, threadstore.NewInternalError(err, "failed to update unarchived thread timestamp")
	}

	if s.stateDB != nil {
		s.applyArchiveState(ctx, threadID, restoredPath, nil)
	}

	return s.readThread(ctx, threadstore.ReadThreadParams{ThreadID: threadID, IncludeArchived: false})
}

// applyArchiveState rewrites the state-DB row for threadID with the supplied
// rollout path and archived timestamp (nil clears the archived flag). It is
// best-effort: a missing row or write failure is ignored, matching the Rust
// `mark_archived`/`mark_unarchived` best-effort semantics.
func (s *LocalThreadStore) applyArchiveState(ctx context.Context, threadID protocol.ThreadID, rolloutPath string, archivedAt *time.Time) {
	metadata, err := s.stateDB.GetThread(ctx, threadID)
	if err != nil || metadata == nil {
		return
	}
	updated := *metadata
	updated.RolloutPath = rolloutPath
	if archivedAt != nil {
		at := *archivedAt
		updated.ArchivedAt = &at
	} else {
		updated.ArchivedAt = nil
	}
	_ = s.stateDB.UpsertThread(ctx, &updated)
}

// touchModifiedTime updates a file's modification time to now so unarchived
// threads sort as recently updated, mirroring the Rust `touch_modified_time`.
func touchModifiedTime(path string) error {
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		return errors.New("failed to update modification time: " + err.Error())
	}
	return nil
}

// updateThreadMetadata applies the metadata patch to the thread's state-DB row
// and, for legacy-compatibility patches (memory mode / git info), appends the
// matching rollout session-meta line so unloaded threads replay the new values.
// It returns the updated threadstore.StoredThread via readThread.
//
// Deviation: the upstream crate also rewrites the rollout thread-name index for a
// name patch. That index is not yet ported, so a name change only updates the
// SQLite title; the memory-mode and git-info rollout session-meta lines (the
// STATUS spec 19 residual) are written here.
// When no state DB is configured, metadata cannot be persisted, so the operation
// fails with an invalid-request error rather than reporting unsupported.
func (s *LocalThreadStore) updateThreadMetadata(ctx context.Context, params threadstore.UpdateThreadMetadataParams) (threadstore.StoredThread, error) {
	threadID := params.ThreadID
	if params.Patch.IsEmpty() {
		return s.readThread(ctx, threadstore.ReadThreadParams{
			ThreadID:        threadID,
			IncludeArchived: params.IncludeArchived,
		})
	}

	if s.stateDB == nil {
		return threadstore.StoredThread{}, threadstore.NewInvalidRequestError(
			"local thread store requires a state db to update metadata for thread %s", threadID)
	}

	existing, err := s.stateDB.GetThread(ctx, threadID)
	if err != nil {
		return threadstore.StoredThread{}, threadstore.NewInternalError(err, "failed to read thread metadata for %s", threadID)
	}
	if existing == nil {
		// First metadata sync for a thread the state DB has not seen: build the
		// row from the patch and the resolved rollout path, mirroring the Rust
		// apply_metadata_update (`existing.unwrap_or_else(ThreadMetadataBuilder…)`).
		seeded, seedErr := s.seedMetadataFromPatch(ctx, threadID, params.Patch, params.IncludeArchived)
		if seedErr != nil {
			return threadstore.StoredThread{}, seedErr
		}
		existing = &seeded
	}

	updated := applyPatchToMetadata(*existing, params.Patch)
	if err := s.stateDB.UpsertThread(ctx, &updated); err != nil {
		return threadstore.StoredThread{}, threadstore.NewInternalError(err, "failed to update thread metadata for %s", threadID)
	}

	if params.Patch.MemoryMode != nil {
		mode := memoryModeAsString(*params.Patch.MemoryMode)
		if _, err := s.stateDB.SetThreadMemoryMode(ctx, threadID, mode); err != nil {
			return threadstore.StoredThread{}, threadstore.NewInternalError(err, "failed to update memory mode for %s", threadID)
		}
	}

	// Append the rollout session-meta line(s) for memory-mode / git-info patches
	// so unloaded threads replay the new values (mirrors the rollout-compat
	// section of the Rust update_thread_metadata).
	if needsRolloutCompatibilityUpdate(params.Patch) &&
		(params.Patch.MemoryMode != nil || params.Patch.GitInfo != nil) {
		rolloutPath, pathErr := s.resolveRolloutPath(ctx, threadID, params.IncludeArchived)
		if pathErr != nil {
			return threadstore.StoredThread{}, pathErr
		}
		if rolloutPath == "" {
			return threadstore.StoredThread{}, threadstore.NewInvalidRequestError("thread not found: %s", threadID)
		}
		if err := s.applyRolloutCompatibilityUpdate(ctx, threadID, rolloutPath, params.Patch); err != nil {
			return threadstore.StoredThread{}, err
		}
	}

	return s.readThread(ctx, threadstore.ReadThreadParams{
		ThreadID:        threadID,
		IncludeArchived: params.IncludeArchived,
	})
}

// applyPatchToMetadata returns a new ThreadMetadata with the patch applied,
// mirroring the field-by-field application in the Rust `apply_metadata_update`.
// It never mutates the input metadata.
func applyPatchToMetadata(metadata state.ThreadMetadata, patch threadstore.ThreadMetadataPatch) state.ThreadMetadata {
	updated := metadata

	if patch.RolloutPath != nil {
		updated.RolloutPath = *patch.RolloutPath
	}
	if patch.Preview != nil {
		updated.Preview = cloneStr(patch.Preview)
	}
	if patch.Name.IsSome() {
		updated.Title = flattenString(patch.Name)
	}
	if patch.Title != nil {
		updated.Title = *patch.Title
	}
	if patch.ModelProvider != nil {
		updated.ModelProvider = *patch.ModelProvider
	}
	if patch.Model != nil {
		updated.Model = cloneStr(patch.Model)
	}
	if patch.ReasoningEffort.IsSome() {
		updated.ReasoningEffort = cloneReasoningEffort(threadstore.Flatten(patch.ReasoningEffort))
	}
	if patch.CreatedAt != nil {
		updated.CreatedAt = patch.CreatedAt.UTC()
	}
	if patch.UpdatedAt != nil {
		updated.UpdatedAt = patch.UpdatedAt.UTC()
	}
	if patch.Source != nil {
		updated.Source = patch.Source.String()
	}
	if patch.ThreadSource.IsSome() {
		updated.ThreadSource = flattenThreadSource(patch.ThreadSource)
	}
	if patch.AgentNickname.IsSome() {
		updated.AgentNickname = flattenStringPtr(patch.AgentNickname)
	}
	if patch.AgentRole.IsSome() {
		updated.AgentRole = flattenStringPtr(patch.AgentRole)
	}
	if patch.AgentPath.IsSome() {
		updated.AgentPath = flattenStringPtr(patch.AgentPath)
	}
	if patch.Cwd != nil {
		updated.Cwd = *patch.Cwd
	}
	if patch.CliVersion != nil {
		updated.CliVersion = *patch.CliVersion
	}
	if patch.ApprovalMode != nil {
		updated.ApprovalMode = string(patch.ApprovalMode.Kind)
	}
	if patch.TokenUsage != nil {
		total := patch.TokenUsage.TotalTokens
		if total < 0 {
			total = 0
		}
		updated.TokensUsed = total
	}
	if patch.FirstUserMessage != nil {
		updated.FirstUserMessage = cloneStr(patch.FirstUserMessage)
	}
	if patch.GitInfo != nil {
		updated.GitSha = resolveClearableGit(patch.GitInfo.SHA, updated.GitSha)
		updated.GitBranch = resolveClearableGit(patch.GitInfo.Branch, updated.GitBranch)
		updated.GitOriginURL = resolveClearableGit(patch.GitInfo.OriginURL, updated.GitOriginURL)
	}

	return updated
}

// resolveClearableGit applies a clearable git patch field: a set value replaces,
// a clear request nils the value, and an absent field leaves the existing value.
func resolveClearableGit(field threadstore.ClearableField[string], existing *string) *string {
	if !field.IsSome() {
		return existing
	}
	return cloneStr(field.Value)
}

// flattenString returns the inner value of a clearable string field, or "" when
// the field is a clear request.
func flattenString(field threadstore.ClearableField[string]) string {
	if v := threadstore.Flatten(field); v != nil {
		return *v
	}
	return ""
}

// flattenStringPtr returns the inner pointer of a clearable string field.
func flattenStringPtr(field threadstore.ClearableField[string]) *string {
	return cloneStr(threadstore.Flatten(field))
}

// flattenThreadSource returns the inner pointer of a clearable thread source.
func flattenThreadSource(field threadstore.ClearableField[rollout.ThreadSource]) *rollout.ThreadSource {
	v := threadstore.Flatten(field)
	if v == nil {
		return nil
	}
	source := *v
	return &source
}

// memoryModeAsString renders a thread memory mode for the state DB column,
// mirroring the Rust `memory_mode_as_str`.
func memoryModeAsString(mode protocol.ThreadMemoryMode) string {
	if mode == protocol.ThreadMemoryModeDisabled {
		return "disabled"
	}
	return "enabled"
}

// seedMetadataFromPatch builds the initial state-DB row for a thread that has
// a rollout but no metadata row yet: created_at from the patch (or now), the
// patch's source/provider/agent identity/cwd/cli version, and the rollout path
// resolved from the live writer or the sessions tree (archived when the
// rollout lives under the archived subtree). Mirrors the builder branch of the
// Rust apply_metadata_update; a thread with no rollout at all is not found.
func (s *LocalThreadStore) seedMetadataFromPatch(ctx context.Context, threadID protocol.ThreadID, patch threadstore.ThreadMetadataPatch, includeArchived bool) (state.ThreadMetadata, error) {
	rolloutPath := ""
	if patch.RolloutPath != nil {
		rolloutPath = *patch.RolloutPath
	} else if recorder, err := s.liveRecorder(threadID); err == nil {
		rolloutPath = recorder.RolloutPath()
	}
	if rolloutPath == "" {
		resolved, err := s.resolveRolloutPath(ctx, threadID, includeArchived)
		if err != nil {
			return state.ThreadMetadata{}, err
		}
		if resolved == "" {
			return state.ThreadMetadata{}, threadstore.NewInvalidRequestError("thread not found: %s", threadID)
		}
		rolloutPath = resolved
	}

	createdAt := time.Now().UTC()
	if patch.CreatedAt != nil {
		createdAt = patch.CreatedAt.UTC()
	} else if patch.UpdatedAt != nil {
		createdAt = patch.UpdatedAt.UTC()
	}
	source := rollout.SessionSource{Kind: rollout.SessionSourceKindUnknown}
	if patch.Source != nil {
		source = *patch.Source
	}
	builder := state.NewThreadMetadataBuilder(threadID, rolloutPath, createdAt, source)
	builder.ModelProvider = cloneStr(patch.ModelProvider)
	builder.ThreadSource = threadstore.Flatten(patch.ThreadSource)
	builder.AgentNickname = threadstore.Flatten(patch.AgentNickname)
	builder.AgentRole = threadstore.Flatten(patch.AgentRole)
	builder.AgentPath = threadstore.Flatten(patch.AgentPath)
	if patch.Cwd != nil {
		builder.Cwd = *patch.Cwd
	}
	builder.CliVersion = cloneStr(patch.CliVersion)
	metadata := builder.Build(s.config.DefaultModelProviderID)
	if rolloutPathIsArchived(s.config.CodexHome, rolloutPath) {
		archivedAt := metadata.UpdatedAt
		metadata.ArchivedAt = &archivedAt
	}
	return metadata, nil
}
