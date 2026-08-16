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

// readSQLiteMetadata loads the state-DB row for threadID, returning nil when the
// store has no DB or the row is absent.
func (s *LocalThreadStore) readSQLiteMetadata(ctx context.Context, threadID protocol.ThreadID) *state.ThreadMetadata {
	if s.stateDB == nil {
		return nil
	}
	meta, err := s.stateDB.GetThread(ctx, threadID)
	if err != nil {
		return nil
	}
	return meta
}

// readThread loads a thread summary and optional history, mirroring the Rust
// `read_thread`. It prefers state-DB metadata that resolves to a valid rollout
// path, then falls back to scanning the sessions tree for the rollout file.
func (s *LocalThreadStore) readThread(ctx context.Context, params threadstore.ReadThreadParams) (threadstore.StoredThread, error) {
	threadID := params.ThreadID

	if thread, ok, err := s.readThreadFromSQLite(ctx, params); err != nil {
		return threadstore.StoredThread{}, err
	} else if ok {
		return thread, nil
	}

	path, err := s.resolveRolloutPath(ctx, threadID, params.IncludeArchived)
	if err != nil {
		return threadstore.StoredThread{}, err
	}
	if path == "" {
		return threadstore.StoredThread{}, threadstore.NewInvalidRequestError("no rollout found for thread id %s", threadID)
	}

	thread, err := s.readThreadFromRolloutPath(ctx, path)
	if err != nil {
		return threadstore.StoredThread{}, err
	}
	if !params.IncludeArchived && thread.ArchivedAt != nil {
		return threadstore.StoredThread{}, threadstore.NewInvalidRequestError("thread %s is archived", thread.ThreadID)
	}
	if err := s.attachHistoryIfRequested(&thread, params.IncludeHistory); err != nil {
		return threadstore.StoredThread{}, err
	}
	return thread, nil
}

// readThreadFromSQLite attempts to build the summary from state-DB metadata. It
// returns ok=false when no usable metadata is present, deferring to the rollout
// scan.
func (s *LocalThreadStore) readThreadFromSQLite(ctx context.Context, params threadstore.ReadThreadParams) (threadstore.StoredThread, bool, error) {
	metadata := s.readSQLiteMetadata(ctx, params.ThreadID)
	if metadata == nil {
		return threadstore.StoredThread{}, false, nil
	}
	archivedByPath := rolloutPathIsArchived(s.config.CodexHome, metadata.RolloutPath)
	if !params.IncludeArchived && (metadata.ArchivedAt != nil || archivedByPath) {
		return threadstore.StoredThread{}, false, nil
	}
	// SQLite metadata can outlive a moved/recreated rollout path. When history is
	// requested verify the path still resolves to this thread before trusting it.
	if params.IncludeHistory && !s.rolloutPathLoadsThread(metadata.RolloutPath, params.ThreadID) {
		return threadstore.StoredThread{}, false, nil
	}

	thread := s.storedThreadFromSQLiteMetadata(metadata)

	// When history is not requested and the rollout file still exists with a
	// non-empty preview, prefer the richer rollout-derived summary while keeping
	// the SQLite-sourced name and git info.
	if !params.IncludeHistory && thread.RolloutPath != nil {
		if rolloutThread, err := s.readThreadFromRolloutPath(ctx, *thread.RolloutPath); err == nil &&
			rolloutThread.ThreadID == params.ThreadID &&
			(params.IncludeArchived || rolloutThread.ArchivedAt == nil) &&
			rolloutThread.Preview != "" {
			if thread.Name != nil {
				rolloutThread.Name = thread.Name
			}
			rolloutThread.GitInfo = thread.GitInfo
			thread = rolloutThread
		}
	}

	if err := s.attachHistoryIfRequested(&thread, params.IncludeHistory); err != nil {
		return threadstore.StoredThread{}, false, err
	}
	return thread, true, nil
}

// rolloutPathLoadsThread reports whether the rollout at path exists and replays
// to threadID, mirroring the Rust `sqlite_rollout_path_can_load_history`.
func (s *LocalThreadStore) rolloutPathLoadsThread(path string, threadID protocol.ThreadID) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	_, loadedID, _, err := rollout.LoadRolloutItems(path)
	if err != nil || loadedID == nil {
		return false
	}
	return *loadedID == threadID
}

// readThreadByRolloutPath reads a thread summary by rollout path, mirroring the
// Rust `read_thread_by_rollout_path`. Relative paths are resolved against the
// Codex home.
func (s *LocalThreadStore) readThreadByRolloutPath(ctx context.Context, params threadstore.ReadThreadByRolloutPathParams) (threadstore.StoredThread, error) {
	path, err := s.resolveRequestedRolloutPath(params.RolloutPath)
	if err != nil {
		return threadstore.StoredThread{}, err
	}
	thread, err := s.readThreadFromRolloutPath(ctx, path)
	if err != nil {
		return threadstore.StoredThread{}, err
	}
	if !params.IncludeArchived && thread.ArchivedAt != nil {
		return threadstore.StoredThread{}, threadstore.NewInvalidRequestError("thread %s is archived", thread.ThreadID)
	}
	if metadata := s.readSQLiteMetadata(ctx, thread.ThreadID); metadata != nil {
		thread.GitInfo = mergeGitInfo(metadata, thread.GitInfo)
	}
	if err := s.attachHistoryIfRequested(&thread, params.IncludeHistory); err != nil {
		return threadstore.StoredThread{}, err
	}
	return thread, nil
}

// resolveRequestedRolloutPath canonicalizes a caller-supplied rollout path,
// joining relative paths to the Codex home, mirroring the Rust
// `resolve_requested_rollout_path`.
func (s *LocalThreadStore) resolveRequestedRolloutPath(rolloutPath string) (string, error) {
	path := rolloutPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.config.CodexHome, path)
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", threadstore.NewInvalidRequestError("failed to resolve rollout path %q: %v", path, err)
	}
	if _, err := os.Stat(resolved); err != nil {
		return "", threadstore.NewInvalidRequestError("failed to resolve rollout path %q: %v", resolved, err)
	}
	return resolved, nil
}

// attachHistoryIfRequested loads and attaches the persisted rollout history when
// includeHistory is set, mirroring the Rust `attach_history_if_requested`.
func (s *LocalThreadStore) attachHistoryIfRequested(thread *threadstore.StoredThread, includeHistory bool) error {
	if !includeHistory {
		return nil
	}
	if thread.RolloutPath == nil {
		return threadstore.NewInternalError(errors.New("missing rollout path"), "failed to load thread history for thread %s", thread.ThreadID)
	}
	items, err := loadHistoryItems(*thread.RolloutPath)
	if err != nil {
		return err
	}
	thread.History = &threadstore.StoredThreadHistory{ThreadID: thread.ThreadID, Items: items}
	return nil
}

// loadHistoryItems loads every persisted rollout item from path.
func loadHistoryItems(path string) ([]rollout.RolloutItem, error) {
	items, _, _, err := rollout.LoadRolloutItems(path)
	if err != nil {
		return nil, threadstore.NewInternalError(err, "failed to load thread history %s", path)
	}
	return items, nil
}

// resolveRolloutPath locates the rollout file for threadID, preferring a live
// writer path, then the active sessions tree, then (when archived is allowed)
// the archived tree, mirroring the Rust `resolve_rollout_path`.
func (s *LocalThreadStore) resolveRolloutPath(ctx context.Context, threadID protocol.ThreadID, includeArchived bool) (string, error) {
	if recorder, err := s.liveRecorder(threadID); err == nil {
		path := recorder.RolloutPath()
		if _, statErr := os.Stat(path); statErr == nil &&
			(includeArchived || !rolloutPathIsArchived(s.config.CodexHome, path)) {
			return path, nil
		}
	}

	if metadata := s.readSQLiteMetadata(ctx, threadID); metadata != nil && metadata.RolloutPath != "" {
		if s.rolloutPathLoadsThread(metadata.RolloutPath, threadID) &&
			(includeArchived || !rolloutPathIsArchived(s.config.CodexHome, metadata.RolloutPath)) {
			return metadata.RolloutPath, nil
		}
	}

	active, err := rollout.FindThreadPathByIDStr(s.config.CodexHome, threadID.String())
	if err != nil {
		return "", threadstore.NewInvalidRequestError("failed to locate thread id %s: %v", threadID, err)
	}
	if active != "" {
		return active, nil
	}
	if !includeArchived {
		return "", nil
	}
	archived, err := rollout.FindArchivedThreadPathByIDStr(s.config.CodexHome, threadID.String())
	if err != nil {
		return "", threadstore.NewInvalidRequestError("failed to locate archived thread id %s: %v", threadID, err)
	}
	return archived, nil
}

// readThreadFromRolloutPath builds a threadstore.StoredThread by replaying the rollout file
// at path, mirroring the Rust `read_thread_from_rollout_path`.
func (s *LocalThreadStore) readThreadFromRolloutPath(_ context.Context, path string) (threadstore.StoredThread, error) {
	items, threadID, _, err := rollout.LoadRolloutItems(path)
	if err != nil {
		return s.storedThreadFromSessionMeta(path)
	}
	if threadID == nil {
		if id, ok := threadIDFromRolloutPath(path); ok {
			threadID = &id
		}
	}
	if threadID == nil {
		return threadstore.StoredThread{}, threadstore.NewInternalError(errors.New("missing thread id"), "failed to read thread id from %s", path)
	}

	archived := rolloutPathIsArchived(s.config.CodexHome, path)
	thread := s.storedThreadFromRolloutItems(*threadID, path, items, archived)
	return thread, nil
}

// storedThreadFromRolloutItems reconstructs a threadstore.StoredThread from replayed rollout
// items by delegating field extraction to the shared state metadata extractor,
// mirroring the Rust `stored_thread_from_rollout_item` + session-meta merge.
func (s *LocalThreadStore) storedThreadFromRolloutItems(
	threadID protocol.ThreadID,
	path string,
	items []rollout.RolloutItem,
	archived bool,
) threadstore.StoredThread {
	meta := s.metadataFromRolloutItems(threadID, path, items)

	createdAt := meta.CreatedAt
	updatedAt := fileModTime(path, createdAt)
	if updatedAt.Before(createdAt) {
		updatedAt = createdAt
	}
	var archivedAt *time.Time
	if archived {
		at := updatedAt
		archivedAt = &at
	}

	preview := ""
	if meta.Preview != nil {
		preview = *meta.Preview
	} else if meta.FirstUserMessage != nil {
		preview = *meta.FirstUserMessage
	}

	modelProvider := meta.ModelProvider
	if modelProvider == "" {
		modelProvider = s.config.DefaultModelProviderID
	}

	thread := threadstore.StoredThread{
		ThreadID:          threadID,
		RolloutPath:       &path,
		ForkedFromID:      forkedFromID(items),
		Preview:           preview,
		Name:              nil,
		ModelProvider:     modelProvider,
		Model:             meta.Model,
		ReasoningEffort:   meta.ReasoningEffort,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		ArchivedAt:        archivedAt,
		Cwd:               meta.Cwd,
		CliVersion:        meta.CliVersion,
		Source:            parseSessionSource(meta.Source),
		ThreadSource:      meta.ThreadSource,
		AgentNickname:     meta.AgentNickname,
		AgentRole:         meta.AgentRole,
		AgentPath:         meta.AgentPath,
		GitInfo:           threadstore.GitInfoFromParts(meta.GitSha, meta.GitBranch, meta.GitOriginURL),
		ApprovalMode:      parseApprovalMode(meta.ApprovalMode),
		PermissionProfile: threadstore.ReadOnlyPermissionProfile(),
		TokenUsage:        nil,
		FirstUserMessage:  meta.FirstUserMessage,
		History:           nil,
	}
	s.applyStoredThreadName(&thread)
	return thread
}

// metadataFromRolloutItems derives canonical thread metadata from rollout items
// by reusing the state extractor, which already implements codex preview,
// first-user-message, model, cwd, and git derivation.
func (s *LocalThreadStore) metadataFromRolloutItems(
	threadID protocol.ThreadID,
	path string,
	items []rollout.RolloutItem,
) state.ThreadMetadata {
	createdAt := createdAtFromItems(items)
	builder := state.NewThreadMetadataBuilder(threadID, path, createdAt, parseSessionSource(""))
	meta := builder.Build(s.config.DefaultModelProviderID)
	meta.ModelProvider = ""
	for i := range items {
		state.ApplyRolloutItem(&meta, &items[i], s.config.DefaultModelProviderID)
	}
	return meta
}

// applyStoredThreadName attaches a distinct legacy/SQLite title to the thread
// name when one is available from the state DB.
func (s *LocalThreadStore) applyStoredThreadName(thread *threadstore.StoredThread) {
	if s.stateDB == nil {
		return
	}
	metadata, err := s.stateDB.GetThread(context.Background(), thread.ThreadID)
	if err != nil || metadata == nil {
		return
	}
	if title := distinctThreadMetadataTitle(metadata); title != nil {
		setThreadNameFromTitle(thread, *title)
	}
}

// storedThreadFromSessionMeta builds a summary from only the session-meta line,
// used when the rollout has no replayable items, mirroring the Rust
// `stored_thread_from_session_meta`.
func (s *LocalThreadStore) storedThreadFromSessionMeta(path string) (threadstore.StoredThread, error) {
	metaLine, err := rollout.ReadSessionMetaLine(path)
	if err != nil {
		return threadstore.StoredThread{}, threadstore.NewInternalError(err, "failed to read thread %s", path)
	}
	archived := rolloutPathIsArchived(s.config.CodexHome, path)
	meta := metaLine.Meta

	createdAt, ok := parseRFC3339(meta.Timestamp)
	if !ok {
		createdAt = time.Now().UTC()
	}
	updatedAt := fileModTime(path, createdAt)
	if updatedAt.Before(createdAt) {
		updatedAt = createdAt
	}
	var archivedAt *time.Time
	if archived {
		at := updatedAt
		archivedAt = &at
	}

	modelProvider := s.config.DefaultModelProviderID
	if meta.ModelProvider != nil && *meta.ModelProvider != "" {
		modelProvider = *meta.ModelProvider
	}

	thread := threadstore.StoredThread{
		ThreadID:          meta.ID,
		RolloutPath:       &path,
		ForkedFromID:      meta.ForkedFromID,
		Preview:           "",
		Name:              nil,
		ModelProvider:     modelProvider,
		Model:             nil,
		ReasoningEffort:   nil,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		ArchivedAt:        archivedAt,
		Cwd:               meta.Cwd,
		CliVersion:        meta.CliVersion,
		Source:            meta.Source,
		ThreadSource:      meta.ThreadSource,
		AgentNickname:     meta.AgentNickname,
		AgentRole:         meta.AgentRole,
		AgentPath:         meta.AgentPath,
		GitInfo:           gitInfoFromSessionMeta(metaLine),
		ApprovalMode:      threadstore.OnRequestApproval(),
		PermissionProfile: threadstore.ReadOnlyPermissionProfile(),
		TokenUsage:        nil,
		FirstUserMessage:  nil,
		History:           nil,
	}
	return thread, nil
}

// storedThreadFromSQLiteMetadata builds a summary purely from state-DB metadata,
// mirroring the Rust `stored_thread_from_sqlite_metadata`.
func (s *LocalThreadStore) storedThreadFromSQLiteMetadata(metadata *state.ThreadMetadata) threadstore.StoredThread {
	var name *string
	if title := distinctThreadMetadataTitle(metadata); title != nil {
		name = title
	}

	var forkedFromID *protocol.ThreadID
	if metaLine, err := rollout.ReadSessionMetaLine(metadata.RolloutPath); err == nil {
		forkedFromID = metaLine.Meta.ForkedFromID
	}

	preview := ""
	if metadata.Preview != nil {
		preview = *metadata.Preview
	} else if metadata.FirstUserMessage != nil {
		preview = *metadata.FirstUserMessage
	}

	modelProvider := metadata.ModelProvider
	if modelProvider == "" {
		modelProvider = s.config.DefaultModelProviderID
	}

	rolloutPath := metadata.RolloutPath
	return threadstore.StoredThread{
		ThreadID:          metadata.ID,
		RolloutPath:       &rolloutPath,
		ForkedFromID:      forkedFromID,
		Preview:           preview,
		Name:              name,
		ModelProvider:     modelProvider,
		Model:             cloneStr(metadata.Model),
		ReasoningEffort:   metadata.ReasoningEffort,
		CreatedAt:         metadata.CreatedAt,
		UpdatedAt:         metadata.UpdatedAt,
		ArchivedAt:        metadata.ArchivedAt,
		Cwd:               metadata.Cwd,
		CliVersion:        metadata.CliVersion,
		Source:            parseSessionSource(metadata.Source),
		ThreadSource:      metadata.ThreadSource,
		AgentNickname:     cloneStr(metadata.AgentNickname),
		AgentRole:         cloneStr(metadata.AgentRole),
		AgentPath:         cloneStr(metadata.AgentPath),
		GitInfo:           threadstore.GitInfoFromParts(metadata.GitSha, metadata.GitBranch, metadata.GitOriginURL),
		ApprovalMode:      parseApprovalMode(metadata.ApprovalMode),
		PermissionProfile: threadstore.ReadOnlyPermissionProfile(),
		TokenUsage:        nil,
		FirstUserMessage:  cloneStr(metadata.FirstUserMessage),
		History:           nil,
	}
}

// forkedFromID returns the forked_from_id recorded in the first session-meta
// line of items, if any.
func forkedFromID(items []rollout.RolloutItem) *protocol.ThreadID {
	for i := range items {
		item := items[i]
		if item.Kind == rollout.RolloutItemKindSessionMeta && item.SessionMeta != nil {
			return item.SessionMeta.Meta.ForkedFromID
		}
	}
	return nil
}

// createdAtFromItems returns the created-at timestamp recorded in the first
// session-meta line of items, defaulting to now.
func createdAtFromItems(items []rollout.RolloutItem) time.Time {
	for i := range items {
		item := items[i]
		if item.Kind == rollout.RolloutItemKindSessionMeta && item.SessionMeta != nil {
			if ts, ok := parseRFC3339(item.SessionMeta.Meta.Timestamp); ok {
				return ts
			}
		}
	}
	return time.Now().UTC()
}

// mergeGitInfo prefers state-DB git fields, falling back to the existing rollout
// git info, mirroring the Rust git-info merge in read_thread_by_rollout_path.
func mergeGitInfo(metadata *state.ThreadMetadata, existing *threadstore.GitInfo) *threadstore.GitInfo {
	var sha, branch, origin *string
	if existing != nil {
		if existing.CommitHash != nil {
			v := existing.CommitHash.String()
			sha = &v
		}
		branch = cloneStr(existing.Branch)
		origin = cloneStr(existing.RepositoryURL)
	}
	if metadata.GitSha != nil {
		sha = cloneStr(metadata.GitSha)
	}
	if metadata.GitBranch != nil {
		branch = cloneStr(metadata.GitBranch)
	}
	if metadata.GitOriginURL != nil {
		origin = cloneStr(metadata.GitOriginURL)
	}
	return threadstore.GitInfoFromParts(sha, branch, origin)
}

// cloneStr returns a copy of a string pointer.
func cloneStr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// gitInfoFromSessionMeta extracts git info from a session-meta line. An
// all-nil marker (`"git":{}`, a git clear) yields nil, mirroring the Rust
// `git_info_from_session_meta` which treats an empty threadstore.GitInfo as absent.
func gitInfoFromSessionMeta(metaLine rollout.SessionMetaLine) *threadstore.GitInfo {
	if metaLine.Git == nil {
		return nil
	}
	info := *metaLine.Git
	if info.CommitHash == nil && info.Branch == nil && info.RepositoryURL == nil {
		return nil
	}
	return &info
}
