package local

import (
	"context"
	"sync"

	"github.com/sqlrush/codexgo/internal/gitutils"
	"github.com/sqlrush/codexgo/internal/gitutils/gitroot"
	"github.com/sqlrush/codexgo/internal/state"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
	"github.com/sqlrush/codexgo/pkg/threadstore"
)

// LocalThreadStoreConfig is the process-scoped configuration for local thread
// storage, mirroring the Rust `LocalThreadStoreConfig`.
//
// New-thread rollout metadata such as cwd, provider, and memory mode is supplied
// when live persistence is opened. Originator and CliVersion are required by the
// rollout recorder's session metadata; the Rust crate reads them from the login
// client and build environment, so callers must supply them here.
type LocalThreadStoreConfig struct {
	// CodexHome is the Codex home directory (where rollout JSONL lives).
	CodexHome string
	// SQLiteHome is the directory that holds the state SQLite database.
	SQLiteHome string
	// DefaultModelProviderID is used only when older local metadata lacks one.
	DefaultModelProviderID string
	// Originator identifies the client writing the rollout.
	Originator string
	// CliVersion is the CLI version recorded in session metadata.
	CliVersion string
}

// LocalThreadStore is the local filesystem/SQLite-backed implementation of
// [threadstore.ThreadStore], mirroring the Rust `LocalThreadStore`.
//
// This port implements the live-writer lifecycle (create/resume/append/persist/
// flush/shutdown/discard) over the rollout [rollout.Rolloutrecorder]. Forking is
// supported via [threadstore.CreateThreadParams.ForkedFromID], which is recorded in the
// session metadata's forked_from_id field.
//
// The read/list/search/archive surface is also implemented: it prefers the state
// DB for ordering and paging and falls back to scanning the on-disk sessions
// tree when no DB is configured or the DB is empty.
//
// SearchThreads scans the rollout transcripts (see local_search.go), a faithful
// port of thread-store/src/local/search_threads.rs + rollout/src/search.rs: a
// raw-JSONL case-insensitive literal path match followed by a conversation-text
// snippet gate. codexgo always uses the in-process scan (codex's `rg`-binary
// fallback), which has identical match semantics without the external dependency.
//
// Deviations from the upstream crate:
//   - UpdateThreadMetadata applies the patch to the SQLite row (the source of
//     truth for listing/reads) but does not rewrite rollout session-meta lines;
//     it requires a state DB and reports [threadstore.ErrorKindInvalidRequest] when none is
//     configured.
type LocalThreadStore struct {
	// UnimplementedStore supplies the 0.147 defaults for the operations the
	// local store does not implement yet (sections, occurrence search, turn/item
	// pagination, prepare_fork, delete). Interface first, implementation staged
	// (spec 50 D0.1); the CLI paths do not call them.
	threadstore.UnimplementedStore

	config LocalThreadStoreConfig

	mu            sync.Mutex
	liveRecorders map[string]*rollout.RolloutRecorder
	stateDB       *state.StateRuntime
}

var _ threadstore.ThreadStore = (*LocalThreadStore)(nil)

// NewLocalThreadStore creates a local store. stateDB may be nil for tests and
// SQLite-less operation, mirroring the Rust `LocalThreadStore::new`.
func NewLocalThreadStore(config LocalThreadStoreConfig, stateDB *state.StateRuntime) *LocalThreadStore {
	return &LocalThreadStore{
		config:        config,
		liveRecorders: make(map[string]*rollout.RolloutRecorder),
		stateDB:       stateDB,
	}
}

// Config returns the store configuration.
func (s *LocalThreadStore) Config() LocalThreadStoreConfig { return s.config }

// StateDB returns the state runtime backing local rollout writers, if any.
func (s *LocalThreadStore) StateDB() *state.StateRuntime { return s.stateDB }

// liveRecorder returns the live recorder for threadID, or a not-found error.
func (s *LocalThreadStore) liveRecorder(threadID protocol.ThreadID) (*rollout.RolloutRecorder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recorder, ok := s.liveRecorders[threadID.String()]
	if !ok {
		return nil, threadstore.NewThreadNotFoundError(threadID)
	}
	return recorder, nil
}

// ensureLiveRecorderAbsent errors when threadID already has a live writer.
func (s *LocalThreadStore) ensureLiveRecorderAbsent(threadID protocol.ThreadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.liveRecorders[threadID.String()]; ok {
		return threadstore.NewInvalidRequestError("thread %s already has a live local writer", threadID)
	}
	return nil
}

// insertLiveRecorder stores recorder for threadID, erroring on duplicates.
func (s *LocalThreadStore) insertLiveRecorder(threadID protocol.ThreadID, recorder *rollout.RolloutRecorder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadID.String()
	if _, ok := s.liveRecorders[key]; ok {
		return threadstore.NewInvalidRequestError("thread %s already has a live local writer", threadID)
	}
	s.liveRecorders[key] = recorder
	return nil
}

// removeLiveRecorder removes and returns the recorder for threadID.
func (s *LocalThreadStore) removeLiveRecorder(threadID protocol.ThreadID) (*rollout.RolloutRecorder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadID.String()
	recorder, ok := s.liveRecorders[key]
	if ok {
		delete(s.liveRecorders, key)
	}
	return recorder, ok
}

// LiveRolloutPath returns the live local rollout path for threadID, mirroring the
// Rust `live_rollout_path`.
func (s *LocalThreadStore) LiveRolloutPath(threadID protocol.ThreadID) (string, error) {
	recorder, err := s.liveRecorder(threadID)
	if err != nil {
		return "", err
	}
	return recorder.RolloutPath(), nil
}

// rolloutConfigFor builds a rollout config for the given metadata.
func (s *LocalThreadStore) rolloutConfigFor(metadata threadstore.ThreadPersistenceMetadata) (rollout.RolloutConfig, error) {
	if metadata.Cwd == nil {
		return rollout.RolloutConfig{}, threadstore.NewInvalidRequestError("local thread store requires a cwd")
	}
	return rollout.RolloutConfig{
		CodexHomeDir:       s.config.CodexHome,
		CwdDir:             *metadata.Cwd,
		ModelProvider:      metadata.ModelProvider,
		GenerateMemoriesOn: metadata.MemoryMode == protocol.ThreadMemoryModeEnabled,
	}, nil
}

// CreateThread opens a new live rollout writer, mirroring the Rust
// `create_thread` live-writer flow. The fork source flows through as
// forked_from_id.
func (s *LocalThreadStore) CreateThread(ctx context.Context, params threadstore.CreateThreadParams) error {
	if err := s.ensureLiveRecorderAbsent(params.ThreadID); err != nil {
		return err
	}
	config, err := s.rolloutConfigFor(params.Metadata)
	if err != nil {
		return err
	}
	recorder, err := rollout.NewRecorderForCreate(ctx, config, rollout.CreateParams{
		ConversationID:   params.ThreadID,
		ForkedFromID:     params.ForkedFromID,
		Source:           params.Source,
		ThreadSource:     params.ThreadSource,
		BaseInstructions: params.BaseInstructions,
		DynamicTools:     params.DynamicTools,
		Originator:       s.config.Originator,
		CliVersion:       s.config.CliVersion,
		GitInfo:          collectRolloutGitInfo,
	})
	if err != nil {
		return threadstore.NewInternalError(err, "failed to initialize local thread recorder")
	}
	return s.insertLiveRecorder(params.ThreadID, recorder)
}

// ResumeThread reopens a live rollout writer over an existing rollout file,
// mirroring the Rust `resume_thread` live-writer flow.
func (s *LocalThreadStore) ResumeThread(ctx context.Context, params threadstore.ResumeThreadParams) error {
	if err := s.ensureLiveRecorderAbsent(params.ThreadID); err != nil {
		return err
	}
	if params.Metadata.Cwd == nil {
		return threadstore.NewInvalidRequestError("local thread store requires a cwd")
	}
	if params.RolloutPath == nil {
		// Resolve the rollout path from the live writer / state DB / sessions scan.
		path, err := s.resolveRolloutPath(ctx, params.ThreadID, true)
		if err != nil {
			return err
		}
		if path == "" {
			return threadstore.NewInvalidRequestError("no rollout found for thread id %s", params.ThreadID)
		}
		params.RolloutPath = &path
	}
	recorder, err := rollout.NewRecorderForResume(ctx, *params.RolloutPath)
	if err != nil {
		return threadstore.NewInternalError(err, "failed to resume local thread recorder")
	}
	return s.insertLiveRecorder(params.ThreadID, recorder)
}

// AppendItems records canonical items and flushes so SQLite never gets ahead of
// JSONL, mirroring the Rust `append_items` live-writer flow.
func (s *LocalThreadStore) AppendItems(ctx context.Context, params threadstore.AppendThreadItemsParams) error {
	recorder, err := s.liveRecorder(params.ThreadID)
	if err != nil {
		return err
	}
	if err := recorder.RecordCanonicalItems(ctx, params.Items); err != nil {
		return threadstore.NewInternalError(err, "failed to record canonical items")
	}
	if err := recorder.Flush(ctx); err != nil {
		return threadstore.NewInternalError(err, "failed to flush local thread recorder")
	}
	return nil
}

// PersistThread materializes the rollout file and writes buffered items.
func (s *LocalThreadStore) PersistThread(ctx context.Context, threadID protocol.ThreadID) error {
	recorder, err := s.liveRecorder(threadID)
	if err != nil {
		return err
	}
	if err := recorder.Persist(ctx); err != nil {
		return threadstore.NewInternalError(err, "failed to persist local thread recorder")
	}
	return nil
}

// FlushThread flushes queued items and waits until they are durable.
func (s *LocalThreadStore) FlushThread(ctx context.Context, threadID protocol.ThreadID) error {
	recorder, err := s.liveRecorder(threadID)
	if err != nil {
		return err
	}
	if err := recorder.Flush(ctx); err != nil {
		return threadstore.NewInternalError(err, "failed to flush local thread recorder")
	}
	return nil
}

// ShutdownThread drains pending items and removes the live writer, mirroring the
// Rust `shutdown_thread`.
func (s *LocalThreadStore) ShutdownThread(ctx context.Context, threadID protocol.ThreadID) error {
	recorder, err := s.liveRecorder(threadID)
	if err != nil {
		return err
	}
	if err := recorder.Shutdown(ctx); err != nil {
		return threadstore.NewInternalError(err, "failed to shut down local thread recorder")
	}
	s.removeLiveRecorder(threadID)
	return nil
}

// DiscardThread drops the live writer without forcing pending items to become
// durable, mirroring the Rust `discard_thread`.
func (s *LocalThreadStore) DiscardThread(_ context.Context, threadID protocol.ThreadID) error {
	if _, ok := s.removeLiveRecorder(threadID); !ok {
		return threadstore.NewThreadNotFoundError(threadID)
	}
	return nil
}

// LoadHistory loads persisted rollout history for the thread by resolving its
// rollout path and replaying every persisted item.
func (s *LocalThreadStore) LoadHistory(ctx context.Context, params threadstore.LoadThreadHistoryParams) (threadstore.StoredThreadHistory, error) {
	return s.loadHistory(ctx, params)
}

// LoadLatestModelContext returns the full persisted history: the local store
// has no targeted suffix read yet (upstream 0.147 local/model_context.rs), which
// the contract permits.
func (s *LocalThreadStore) LoadLatestModelContext(ctx context.Context, params threadstore.LoadThreadHistoryParams) (threadstore.StoredModelContext, error) {
	history, err := s.loadHistory(ctx, params)
	if err != nil {
		return threadstore.StoredModelContext{}, err
	}
	return threadstore.StoredModelContext{ThreadID: history.ThreadID, Items: history.Items}, nil
}

// ReadThread loads a stored thread summary (and optional history) by id,
// preferring state-DB metadata and falling back to a sessions-tree scan.
func (s *LocalThreadStore) ReadThread(ctx context.Context, params threadstore.ReadThreadParams) (threadstore.StoredThread, error) {
	return s.readThread(ctx, params)
}

// ReadThreadByRolloutPath loads a stored thread summary by rollout path.
func (s *LocalThreadStore) ReadThreadByRolloutPath(ctx context.Context, params threadstore.ReadThreadByRolloutPathParams) (threadstore.StoredThread, error) {
	return s.readThreadByRolloutPath(ctx, params)
}

// ListThreads lists stored thread summaries, preferring the state DB and falling
// back to a sessions-tree scan.
func (s *LocalThreadStore) ListThreads(ctx context.Context, params threadstore.ListThreadsParams) (threadstore.ThreadPage, error) {
	return s.listThreads(ctx, params)
}

// SearchThreads lists then filters stored threads by the supplied query.
func (s *LocalThreadStore) SearchThreads(ctx context.Context, params threadstore.SearchThreadsParams) (threadstore.ThreadSearchPage, error) {
	return s.searchThreads(ctx, params)
}

// UpdateThreadMetadata applies a metadata patch to the state-DB row and returns
// the refreshed thread summary.
func (s *LocalThreadStore) UpdateThreadMetadata(ctx context.Context, params threadstore.UpdateThreadMetadataParams) (threadstore.StoredThread, error) {
	return s.updateThreadMetadata(ctx, params)
}

// ArchiveThread archives a thread by moving its rollout file into the archived
// sessions tree and updating the state DB when present.
func (s *LocalThreadStore) ArchiveThread(ctx context.Context, params threadstore.ArchiveThreadParams) error {
	return s.archiveThread(ctx, params)
}

// ArchiveThreads archives in order with the Rust default semantics.
func (s *LocalThreadStore) ArchiveThreads(ctx context.Context, params threadstore.ArchiveThreadsParams) ([]protocol.ThreadID, error) {
	return threadstore.ArchiveThreadsSequentially(ctx, s, params, nil)
}

// UnarchiveThread reverses an archive and returns the restored thread summary.
func (s *LocalThreadStore) UnarchiveThread(ctx context.Context, params threadstore.ArchiveThreadParams) (threadstore.StoredThread, error) {
	return s.unarchiveThread(ctx, params)
}

// DeleteThread is not implemented for the local rollout-file store yet
// (upstream 0.147 local/delete_thread.rs removes the rollout file and state
// rows); it reports Unsupported so callers fail closed rather than leave
// partial state.
func (s *LocalThreadStore) DeleteThread(context.Context, threadstore.DeleteThreadParams) error {
	return threadstore.NewUnsupportedError("delete_thread")
}

// DeleteThreads deletes in order with the Rust default semantics (every member
// reports Unsupported today, so the first call surfaces it).
func (s *LocalThreadStore) DeleteThreads(ctx context.Context, params threadstore.DeleteThreadsParams) error {
	return threadstore.DeleteThreadsSequentially(ctx, s, params)
}

// collectRolloutGitInfo gathers git info for cwd when cwd is inside a
// repository, matching the Rust `write_session_meta` behaviour. It is injected
// into the rollout recorder so the rollout package itself stays free of the git
// backend dependency.
func collectRolloutGitInfo(ctx context.Context, cwd string) *protocol.GitInfo {
	if _, ok := gitroot.GetGitRepoRoot(cwd); !ok {
		return nil
	}
	return gitutils.CollectGitInfo(ctx, cwd)
}
