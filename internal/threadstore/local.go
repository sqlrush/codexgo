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
// Deviation: the read/list/search paths in the upstream crate scan rollout JSONL
// and the state DB through helper APIs that are not part of this package's
// allowed dependency set; those operations return an [ErrorKindUnsupported]
// error here. The live-writer surface, the in-memory store, and the full type
// system are faithful.
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

// LoadHistory is part of the read path, which is not supported in this port.
func (s *LocalThreadStore) LoadHistory(_ context.Context, _ LoadThreadHistoryParams) (StoredThreadHistory, error) {
	return StoredThreadHistory{}, unsupportedError("load_history")
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

// ListThreads is part of the read path, which is not supported in this port.
func (s *LocalThreadStore) ListThreads(_ context.Context, _ ListThreadsParams) (ThreadPage, error) {
	return ThreadPage{}, unsupportedError("list_threads")
}

// SearchThreads is part of the read path, which is not supported in this port.
func (s *LocalThreadStore) SearchThreads(_ context.Context, _ SearchThreadsParams) (ThreadSearchPage, error) {
	return ThreadSearchPage{}, unsupportedError("thread/search")
}

// UpdateThreadMetadata is part of the metadata-sync path, which is not supported
// in this port.
func (s *LocalThreadStore) UpdateThreadMetadata(_ context.Context, _ UpdateThreadMetadataParams) (StoredThread, error) {
	return StoredThread{}, unsupportedError("update_thread_metadata")
}

// ArchiveThread is part of the read/metadata path, which is not supported in this
// port.
func (s *LocalThreadStore) ArchiveThread(_ context.Context, _ ArchiveThreadParams) error {
	return unsupportedError("archive_thread")
}

// UnarchiveThread is part of the read/metadata path, which is not supported in
// this port.
func (s *LocalThreadStore) UnarchiveThread(_ context.Context, _ ArchiveThreadParams) (StoredThread, error) {
	return StoredThread{}, unsupportedError("unarchive_thread")
}
