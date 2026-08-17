package threadstore

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// InMemoryThreadStoreCalls records the call counts observed by an
// [InMemoryThreadStore], mirroring the Rust struct.
type InMemoryThreadStoreCalls struct {
	CreateThread            int
	ResumeThread            int
	AppendItems             int
	PersistThread           int
	FlushThread             int
	ShutdownThread          int
	DiscardThread           int
	LoadHistory             int
	ReadThread              int
	ReadThreadByRolloutPath int
	ListThreads             int
	UpdateThreadMetadata    int
	ArchiveThread           int
	UnarchiveThread         int
	LoadLatestModelContext  int
	DeleteThread            int
}

// InMemoryThreadStore is an in-memory [ThreadStore] for tests and debug configs,
// mirroring the Rust `InMemoryThreadStore`. Sections, occurrence search and
// turn/item pagination take the trait defaults (Unsupported) via
// [UnimplementedStore]; move-to-section is unsupported here as well.
type InMemoryThreadStore struct {
	UnimplementedStore

	mu    sync.Mutex
	state inMemoryState
}

type inMemoryState struct {
	calls           InMemoryThreadStoreCalls
	createdThreads  map[string]CreateThreadParams
	histories       map[string][]rollout.RolloutItem
	metadataUpdates map[string]*ThreadMetadataPatch
	names           map[string]*string
	rolloutPaths    map[string]protocol.ThreadID
}

var _ ThreadStore = (*InMemoryThreadStore)(nil)

// inMemoryRegistry holds the process-global shared stores keyed by id, mirroring
// the Rust static `IN_MEMORY_THREAD_STORES`.
var inMemoryRegistry = struct {
	mu     sync.Mutex
	stores map[string]*InMemoryThreadStore
}{stores: make(map[string]*InMemoryThreadStore)}

// NewInMemoryThreadStore returns a fresh, unregistered in-memory store.
func NewInMemoryThreadStore() *InMemoryThreadStore {
	return &InMemoryThreadStore{state: newInMemoryState()}
}

func newInMemoryState() inMemoryState {
	return inMemoryState{
		createdThreads:  make(map[string]CreateThreadParams),
		histories:       make(map[string][]rollout.RolloutItem),
		metadataUpdates: make(map[string]*ThreadMetadataPatch),
		names:           make(map[string]*string),
		rolloutPaths:    make(map[string]protocol.ThreadID),
	}
}

// InMemoryThreadStoreForID returns the shared store associated with id, creating
// it if needed, mirroring the Rust `InMemoryThreadStore::for_id`.
func InMemoryThreadStoreForID(id string) *InMemoryThreadStore {
	inMemoryRegistry.mu.Lock()
	defer inMemoryRegistry.mu.Unlock()
	store, ok := inMemoryRegistry.stores[id]
	if !ok {
		store = NewInMemoryThreadStore()
		inMemoryRegistry.stores[id] = store
	}
	return store
}

// RemoveInMemoryThreadStoreID removes a shared in-memory store for id, returning
// it when present, mirroring the Rust `InMemoryThreadStore::remove_id`.
func RemoveInMemoryThreadStoreID(id string) *InMemoryThreadStore {
	inMemoryRegistry.mu.Lock()
	defer inMemoryRegistry.mu.Unlock()
	store := inMemoryRegistry.stores[id]
	delete(inMemoryRegistry.stores, id)
	return store
}

// Calls returns a copy of the call counts observed by this store.
func (s *InMemoryThreadStore) Calls() InMemoryThreadStoreCalls {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.calls
}

// CreateThread records a created thread and initializes its history.
func (s *InMemoryThreadStore) CreateThread(_ context.Context, params CreateThreadParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.CreateThread++
	key := params.ThreadID.String()
	if _, ok := s.state.histories[key]; !ok {
		s.state.histories[key] = nil
	}
	s.state.createdThreads[key] = params
	return nil
}

// ResumeThread records a resume and the optional rollout path mapping.
func (s *InMemoryThreadStore) ResumeThread(_ context.Context, params ResumeThreadParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.ResumeThread++
	key := params.ThreadID.String()
	if _, ok := s.state.histories[key]; !ok {
		s.state.histories[key] = nil
	}
	if params.RolloutPath != nil {
		s.state.rolloutPaths[*params.RolloutPath] = params.ThreadID
	}
	return nil
}

// AppendItems appends items to the in-memory history.
func (s *InMemoryThreadStore) AppendItems(_ context.Context, params AppendThreadItemsParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.AppendItems++
	key := params.ThreadID.String()
	s.state.histories[key] = append(s.state.histories[key], params.Items...)
	return nil
}

// PersistThread records a persist call.
func (s *InMemoryThreadStore) PersistThread(_ context.Context, _ protocol.ThreadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.PersistThread++
	return nil
}

// FlushThread records a flush call.
func (s *InMemoryThreadStore) FlushThread(_ context.Context, _ protocol.ThreadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.FlushThread++
	return nil
}

// ShutdownThread records a shutdown call.
func (s *InMemoryThreadStore) ShutdownThread(_ context.Context, _ protocol.ThreadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.ShutdownThread++
	return nil
}

// DiscardThread records a discard call.
func (s *InMemoryThreadStore) DiscardThread(_ context.Context, _ protocol.ThreadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.DiscardThread++
	return nil
}

// LoadHistory returns the stored history for the thread.
func (s *InMemoryThreadStore) LoadHistory(_ context.Context, params LoadThreadHistoryParams) (StoredThreadHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.LoadHistory++
	key := params.ThreadID.String()
	items, ok := s.state.histories[key]
	if !ok {
		return StoredThreadHistory{}, NewThreadNotFoundError(params.ThreadID)
	}
	return StoredThreadHistory{ThreadID: params.ThreadID, Items: cloneItems(items)}, nil
}

// ReadThread reads a thread summary from in-memory state.
func (s *InMemoryThreadStore) ReadThread(_ context.Context, params ReadThreadParams) (StoredThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.ReadThread++
	return s.storedThreadFromState(params.ThreadID, params.IncludeHistory)
}

// ReadThreadByRolloutPath resolves the thread id from a known rollout path.
func (s *InMemoryThreadStore) ReadThreadByRolloutPath(_ context.Context, params ReadThreadByRolloutPathParams) (StoredThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.ReadThreadByRolloutPath++
	threadID, ok := s.state.rolloutPaths[params.RolloutPath]
	if !ok {
		return StoredThread{}, NewInvalidRequestError("in-memory thread store does not know rollout path %s", params.RolloutPath)
	}
	return s.storedThreadFromState(threadID, params.IncludeHistory)
}

// ListThreads returns all created threads sorted by thread id.
func (s *InMemoryThreadStore) ListThreads(_ context.Context, _ ListThreadsParams) (ThreadPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.ListThreads++

	keys := make([]string, 0, len(s.state.createdThreads))
	for key := range s.state.createdThreads {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]StoredThread, 0, len(keys))
	for _, key := range keys {
		thread, err := s.storedThreadFromState(protocol.NewThreadID(key), false)
		if err != nil {
			return ThreadPage{}, err
		}
		items = append(items, thread)
	}
	return ThreadPage{Items: items, NextCursor: nil}, nil
}

// LoadLatestModelContext returns the full stored history: the in-memory store
// has no targeted read, which the contract allows.
func (s *InMemoryThreadStore) LoadLatestModelContext(_ context.Context, params LoadThreadHistoryParams) (StoredModelContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.LoadLatestModelContext++
	key := params.ThreadID.String()
	items, ok := s.state.histories[key]
	if !ok {
		return StoredModelContext{}, NewThreadNotFoundError(params.ThreadID)
	}
	return StoredModelContext{ThreadID: params.ThreadID, Items: cloneItems(items)}, nil
}

// UpdateThreadMetadata merges the patch into the stored metadata.
func (s *InMemoryThreadStore) UpdateThreadMetadata(_ context.Context, params UpdateThreadMetadataParams) (StoredThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.UpdateThreadMetadata++
	key := params.ThreadID.String()
	if params.Patch.Name.IsSome() {
		s.state.names[key] = params.Patch.Name.Value
	}
	existing := s.state.metadataUpdates[key]
	if existing == nil {
		existing = &ThreadMetadataPatch{}
		s.state.metadataUpdates[key] = existing
	}
	existing.Merge(params.Patch)
	return s.storedThreadFromState(params.ThreadID, false)
}

// ArchiveThread records an archive call.
func (s *InMemoryThreadStore) ArchiveThread(_ context.Context, _ ArchiveThreadParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.ArchiveThread++
	return nil
}

// ArchiveThreads archives in order with the Rust default semantics.
func (s *InMemoryThreadStore) ArchiveThreads(ctx context.Context, params ArchiveThreadsParams) ([]protocol.ThreadID, error) {
	return ArchiveThreadsSequentially(ctx, s, params, nil)
}

// UnarchiveThread records an unarchive call and returns the current summary.
func (s *InMemoryThreadStore) UnarchiveThread(_ context.Context, params ArchiveThreadParams) (StoredThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.UnarchiveThread++
	return s.storedThreadFromState(params.ThreadID, false)
}

// DeleteThread forgets everything recorded for the thread, mirroring the Rust
// in-memory `delete_thread`; an unknown thread reports ThreadNotFound.
func (s *InMemoryThreadStore) DeleteThread(_ context.Context, params DeleteThreadParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.calls.DeleteThread++
	key := params.ThreadID.String()
	_, existed := s.state.histories[key]
	delete(s.state.histories, key)
	delete(s.state.createdThreads, key)
	delete(s.state.names, key)
	delete(s.state.metadataUpdates, key)
	for path, mapped := range s.state.rolloutPaths {
		if mapped.String() == key {
			delete(s.state.rolloutPaths, path)
		}
	}
	if !existed {
		return NewThreadNotFoundError(params.ThreadID)
	}
	return nil
}

// DeleteThreads deletes in order with the Rust default semantics.
func (s *InMemoryThreadStore) DeleteThreads(ctx context.Context, params DeleteThreadsParams) error {
	return DeleteThreadsSequentially(ctx, s, params)
}

// storedThreadFromState builds a StoredThread from the recorded state, mirroring
// the Rust `stored_thread_from_state`. The caller must hold s.mu.
func (s *InMemoryThreadStore) storedThreadFromState(threadID protocol.ThreadID, includeHistory bool) (StoredThread, error) {
	key := threadID.String()
	created, ok := s.state.createdThreads[key]
	if !ok {
		return StoredThread{}, NewThreadNotFoundError(threadID)
	}
	historyItems := cloneItems(s.state.histories[key])
	var history *StoredThreadHistory
	if includeHistory {
		history = &StoredThreadHistory{ThreadID: threadID, Items: historyItems}
	}
	name := s.state.names[key]
	metadata := s.state.metadataUpdates[key]

	var rolloutPath *string
	if metadata != nil && metadata.RolloutPath != nil {
		rolloutPath = metadata.RolloutPath
	} else {
		for path, mapped := range s.state.rolloutPaths {
			if mapped.String() == key {
				p := path
				rolloutPath = &p
				break
			}
		}
	}

	now := time.Now().UTC()
	thread := StoredThread{
		ThreadID:          threadID,
		RolloutPath:       rolloutPath,
		ForkedFromID:      created.ForkedFromID,
		ParentThreadID:    created.ParentThreadID,
		Preview:           metaString(metadata, func(m *ThreadMetadataPatch) *string { return m.Preview }),
		Name:              name,
		ModelProvider:     metaStringOr(metadata, func(m *ThreadMetadataPatch) *string { return m.ModelProvider }, "test"),
		Model:             metaPtr(metadata, func(m *ThreadMetadataPatch) *string { return m.Model }),
		ReasoningEffort:   metaReasoning(metadata),
		CreatedAt:         metaTimeOr(metadata, func(m *ThreadMetadataPatch) *time.Time { return m.CreatedAt }, now),
		UpdatedAt:         metaTimeOr(metadata, func(m *ThreadMetadataPatch) *time.Time { return m.UpdatedAt }, now),
		RecencyAt:         metaRecency(metadata, now),
		ArchivedAt:        nil,
		Cwd:               metaString(metadata, func(m *ThreadMetadataPatch) *string { return m.Cwd }),
		CliVersion:        metaStringOr(metadata, func(m *ThreadMetadataPatch) *string { return m.CliVersion }, "test"),
		Source:            metaSource(metadata, created.Source),
		HistoryMode:       historyModeOrLegacy(created.HistoryMode),
		ThreadSource:      metaThreadSource(metadata, created.ThreadSource),
		AgentNickname:     flattenMeta(metadata, func(m *ThreadMetadataPatch) ClearableField[string] { return m.AgentNickname }),
		AgentRole:         flattenMeta(metadata, func(m *ThreadMetadataPatch) ClearableField[string] { return m.AgentRole }),
		AgentPath:         flattenMeta(metadata, func(m *ThreadMetadataPatch) ClearableField[string] { return m.AgentPath }),
		GitInfo:           gitInfoFromMeta(metadata),
		ApprovalMode:      metaApproval(metadata),
		PermissionProfile: metaPermissionProfile(metadata),
		TokenUsage:        metaTokenUsage(metadata),
		FirstUserMessage:  metaPtr(metadata, func(m *ThreadMetadataPatch) *string { return m.FirstUserMessage }),
		History:           history,
	}
	return thread, nil
}

func cloneItems(items []rollout.RolloutItem) []rollout.RolloutItem {
	if len(items) == 0 {
		return nil
	}
	return append([]rollout.RolloutItem(nil), items...)
}

func metaString(m *ThreadMetadataPatch, get func(*ThreadMetadataPatch) *string) string {
	if m == nil {
		return ""
	}
	if v := get(m); v != nil {
		return *v
	}
	return ""
}

func metaStringOr(m *ThreadMetadataPatch, get func(*ThreadMetadataPatch) *string, fallback string) string {
	if m != nil {
		if v := get(m); v != nil {
			return *v
		}
	}
	return fallback
}

func metaPtr(m *ThreadMetadataPatch, get func(*ThreadMetadataPatch) *string) *string {
	if m == nil {
		return nil
	}
	return get(m)
}

func metaReasoning(m *ThreadMetadataPatch) *protocol.ReasoningEffort {
	if m == nil {
		return nil
	}
	return flatten(m.ReasoningEffort)
}

func metaTimeOr(m *ThreadMetadataPatch, get func(*ThreadMetadataPatch) *time.Time, fallback time.Time) time.Time {
	if m != nil {
		if v := get(m); v != nil {
			return *v
		}
	}
	return fallback
}

func metaSource(m *ThreadMetadataPatch, fallback rollout.SessionSource) rollout.SessionSource {
	if m != nil && m.Source != nil {
		return *m.Source
	}
	return fallback
}

func metaThreadSource(m *ThreadMetadataPatch, fallback *rollout.ThreadSource) *rollout.ThreadSource {
	if m != nil && m.ThreadSource.IsSome() {
		return m.ThreadSource.Value
	}
	return fallback
}

func flattenMeta(m *ThreadMetadataPatch, get func(*ThreadMetadataPatch) ClearableField[string]) *string {
	if m == nil {
		return nil
	}
	return flatten(get(m))
}

func gitInfoFromMeta(m *ThreadMetadataPatch) *GitInfo {
	if m == nil {
		return nil
	}
	return gitInfoFromPatch(m.GitInfo)
}

func metaApproval(m *ThreadMetadataPatch) protocol.AskForApproval {
	if m != nil && m.ApprovalMode != nil {
		return *m.ApprovalMode
	}
	return neverApproval()
}

func metaPermissionProfile(m *ThreadMetadataPatch) protocol.PermissionProfile {
	if m != nil && m.PermissionProfile != nil {
		return *m.PermissionProfile
	}
	return ReadOnlyPermissionProfile()
}

func metaTokenUsage(m *ThreadMetadataPatch) *protocol.TokenUsage {
	if m == nil {
		return nil
	}
	return m.TokenUsage
}

// metaRecency mirrors the Rust `recency_at` derivation: advance_recency_at
// wins, then updated_at, then the fallback.
func metaRecency(m *ThreadMetadataPatch, fallback time.Time) time.Time {
	if m != nil {
		if m.AdvanceRecencyAt != nil {
			return *m.AdvanceRecencyAt
		}
		if m.UpdatedAt != nil {
			return *m.UpdatedAt
		}
	}
	return fallback
}

// historyModeOrLegacy maps the zero value to Legacy.
func historyModeOrLegacy(mode protocol.ThreadHistoryMode) protocol.ThreadHistoryMode {
	if mode == "" {
		return protocol.ThreadHistoryModeLegacy
	}
	return mode
}
