package threadstore

import (
	"context"
	"sync"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/state"
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
// [ThreadStore], mirroring the Rust `LocalThreadStore`.
//
// This port implements the live-writer lifecycle (create/resume/append/persist/
// flush/shutdown/discard) over the rollout [rollout.Rolloutrecorder]. Forking is
// supported via [CreateThreadParams.ForkedFromID], which is recorded in the
// session metadata's forked_from_id field.
//
// The read/list/search/archive surface is also implemented: it prefers the state
// DB for ordering and paging and falls back to scanning the on-disk sessions
// tree when no DB is configured or the DB is empty.
//
// Deviations from the upstream crate:
//   - SearchThreads matches the visible title/preview/cwd/first-user-message via
//     substring containment rather than a ripgrep transcript scan, which is
//     outside this package's allowed dependency set.
//   - UpdateThreadMetadata applies the patch to the SQLite row (the source of
//     truth for listing/reads) but does not rewrite rollout session-meta lines;
//     it requires a state DB and reports [ErrorKindInvalidRequest] when none is
//     configured.
type LocalThreadStore struct {
	config LocalThreadStoreConfig

	mu            sync.Mutex
	liveRecorders map[string]*rollout.RolloutRecorder
	stateDB       *state.StateRuntime
}

var _ ThreadStore = (*LocalThreadStore)(nil)

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
		return nil, threadNotFoundError(threadID)
	}
	return recorder, nil
}

// ensureLiveRecorderAbsent errors when threadID already has a live writer.
func (s *LocalThreadStore) ensureLiveRecorderAbsent(threadID protocol.ThreadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.liveRecorders[threadID.String()]; ok {
		return invalidRequestError("thread %s already has a live local writer", threadID)
	}
	return nil
}

// insertLiveRecorder stores recorder for threadID, erroring on duplicates.
func (s *LocalThreadStore) insertLiveRecorder(threadID protocol.ThreadID, recorder *rollout.RolloutRecorder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadID.String()
	if _, ok := s.liveRecorders[key]; ok {
		return invalidRequestError("thread %s already has a live local writer", threadID)
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
func (s *LocalThreadStore) rolloutConfigFor(metadata ThreadPersistenceMetadata) (rollout.RolloutConfig, error) {
	if metadata.Cwd == nil {
		return rollout.RolloutConfig{}, invalidRequestError("local thread store requires a cwd")
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
func (s *LocalThreadStore) CreateThread(ctx context.Context, params CreateThreadParams) error {
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
	})
	if err != nil {
		return internalError(err, "failed to initialize local thread recorder")
	}
	return s.insertLiveRecorder(params.ThreadID, recorder)
}

// ResumeThread reopens a live rollout writer over an existing rollout file,
// mirroring the Rust `resume_thread` live-writer flow.
func (s *LocalThreadStore) ResumeThread(ctx context.Context, params ResumeThreadParams) error {
	if err := s.ensureLiveRecorderAbsent(params.ThreadID); err != nil {
		return err
	}
	if params.Metadata.Cwd == nil {
		return invalidRequestError("local thread store requires a cwd")
	}
	if params.RolloutPath == nil {
		// Resolve the rollout path from the live writer / state DB / sessions scan.
		path, err := s.resolveRolloutPath(ctx, params.ThreadID, true)
		if err != nil {
			return err
		}
		if path == "" {
			return invalidRequestError("no rollout found for thread id %s", params.ThreadID)
		}
		params.RolloutPath = &path
	}
	recorder, err := rollout.NewRecorderForResume(ctx, *params.RolloutPath)
	if err != nil {
		return internalError(err, "failed to resume local thread recorder")
	}
	return s.insertLiveRecorder(params.ThreadID, recorder)
}

// AppendItems records canonical items and flushes so SQLite never gets ahead of
// JSONL, mirroring the Rust `append_items` live-writer flow.
func (s *LocalThreadStore) AppendItems(ctx context.Context, params AppendThreadItemsParams) error {
	recorder, err := s.liveRecorder(params.ThreadID)
	if err != nil {
		return err
	}
	if err := recorder.RecordCanonicalItems(ctx, params.Items); err != nil {
		return internalError(err, "failed to record canonical items")
	}
	if err := recorder.Flush(ctx); err != nil {
		return internalError(err, "failed to flush local thread recorder")
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
		return internalError(err, "failed to persist local thread recorder")
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
		return internalError(err, "failed to flush local thread recorder")
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
		return internalError(err, "failed to shut down local thread recorder")
	}
	s.removeLiveRecorder(threadID)
	return nil
}

// DiscardThread drops the live writer without forcing pending items to become
// durable, mirroring the Rust `discard_thread`.
func (s *LocalThreadStore) DiscardThread(_ context.Context, threadID protocol.ThreadID) error {
	if _, ok := s.removeLiveRecorder(threadID); !ok {
		return threadNotFoundError(threadID)
	}
	return nil
}

// LoadHistory loads persisted rollout history for the thread by resolving its
// rollout path and replaying every persisted item.
func (s *LocalThreadStore) LoadHistory(ctx context.Context, params LoadThreadHistoryParams) (StoredThreadHistory, error) {
	return s.loadHistory(ctx, params)
}

// ReadThread loads a stored thread summary (and optional history) by id,
// preferring state-DB metadata and falling back to a sessions-tree scan.
func (s *LocalThreadStore) ReadThread(ctx context.Context, params ReadThreadParams) (StoredThread, error) {
	return s.readThread(ctx, params)
}

// ReadThreadByRolloutPath loads a stored thread summary by rollout path.
func (s *LocalThreadStore) ReadThreadByRolloutPath(ctx context.Context, params ReadThreadByRolloutPathParams) (StoredThread, error) {
	return s.readThreadByRolloutPath(ctx, params)
}

// ListThreads lists stored thread summaries, preferring the state DB and falling
// back to a sessions-tree scan.
func (s *LocalThreadStore) ListThreads(ctx context.Context, params ListThreadsParams) (ThreadPage, error) {
	return s.listThreads(ctx, params)
}

// SearchThreads lists then filters stored threads by the supplied query.
func (s *LocalThreadStore) SearchThreads(ctx context.Context, params SearchThreadsParams) (ThreadSearchPage, error) {
	return s.searchThreads(ctx, params)
}

// UpdateThreadMetadata applies a metadata patch to the state-DB row and returns
// the refreshed thread summary.
func (s *LocalThreadStore) UpdateThreadMetadata(ctx context.Context, params UpdateThreadMetadataParams) (StoredThread, error) {
	return s.updateThreadMetadata(ctx, params)
}

// ArchiveThread archives a thread by moving its rollout file into the archived
// sessions tree and updating the state DB when present.
func (s *LocalThreadStore) ArchiveThread(ctx context.Context, params ArchiveThreadParams) error {
	return s.archiveThread(ctx, params)
}

// UnarchiveThread reverses an archive and returns the restored thread summary.
func (s *LocalThreadStore) UnarchiveThread(ctx context.Context, params ArchiveThreadParams) (StoredThread, error) {
	return s.unarchiveThread(ctx, params)
}
