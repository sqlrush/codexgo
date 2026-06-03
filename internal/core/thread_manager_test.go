package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/threadstore"
)

// ----------------------------------------------------------------------------
// Test doubles
// ----------------------------------------------------------------------------

// recordingRecorder is a RolloutRecorder that captures every recorded item and
// counts flushes, so tests can assert on the session_meta startup writes.
type recordingRecorder struct {
	mu       sync.Mutex
	recorded []rollout.RolloutItem
	flushes  int
	recErr   error
}

func (r *recordingRecorder) Record(_ context.Context, items []rollout.RolloutItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recErr != nil {
		return r.recErr
	}
	r.recorded = append(r.recorded, items...)
	return nil
}

func (r *recordingRecorder) Flush(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushes++
	return nil
}

func (r *recordingRecorder) items() []rollout.RolloutItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]rollout.RolloutItem(nil), r.recorded...)
}

// stubStore is a ThreadStore that only implements the read paths the
// ThreadManager exercises (ReadThreadByRolloutPath). Every other method returns
// an unsupported error so accidental use is loud.
type stubStore struct {
	byRolloutPath map[string]threadstore.StoredThread
	readErr       error
	readByPath    int
}

func (s *stubStore) ReadThreadByRolloutPath(_ context.Context, params threadstore.ReadThreadByRolloutPathParams) (threadstore.StoredThread, error) {
	s.readByPath++
	if s.readErr != nil {
		return threadstore.StoredThread{}, s.readErr
	}
	st, ok := s.byRolloutPath[params.RolloutPath]
	if !ok {
		return threadstore.StoredThread{}, &threadstore.Error{
			Kind:    threadstore.ErrorKindInvalidRequest,
			Message: fmt.Sprintf("no rollout found for %s", params.RolloutPath),
		}
	}
	return st, nil
}

func (s *stubStore) unsupported(op string) error {
	return &threadstore.Error{Kind: threadstore.ErrorKindUnsupported, Operation: op}
}

func (s *stubStore) CreateThread(context.Context, threadstore.CreateThreadParams) error {
	return s.unsupported("create")
}
func (s *stubStore) ResumeThread(context.Context, threadstore.ResumeThreadParams) error {
	return s.unsupported("resume")
}
func (s *stubStore) AppendItems(context.Context, threadstore.AppendThreadItemsParams) error {
	return s.unsupported("append")
}
func (s *stubStore) PersistThread(context.Context, protocol.ThreadID) error {
	return s.unsupported("persist")
}
func (s *stubStore) FlushThread(context.Context, protocol.ThreadID) error {
	return s.unsupported("flush")
}
func (s *stubStore) ShutdownThread(context.Context, protocol.ThreadID) error {
	return s.unsupported("shutdown")
}
func (s *stubStore) DiscardThread(context.Context, protocol.ThreadID) error {
	return s.unsupported("discard")
}
func (s *stubStore) LoadHistory(context.Context, threadstore.LoadThreadHistoryParams) (threadstore.StoredThreadHistory, error) {
	return threadstore.StoredThreadHistory{}, s.unsupported("load")
}
func (s *stubStore) ReadThread(context.Context, threadstore.ReadThreadParams) (threadstore.StoredThread, error) {
	return threadstore.StoredThread{}, s.unsupported("read")
}
func (s *stubStore) ListThreads(context.Context, threadstore.ListThreadsParams) (threadstore.ThreadPage, error) {
	return threadstore.ThreadPage{}, s.unsupported("list")
}
func (s *stubStore) SearchThreads(context.Context, threadstore.SearchThreadsParams) (threadstore.ThreadSearchPage, error) {
	return threadstore.ThreadSearchPage{}, s.unsupported("search")
}
func (s *stubStore) UpdateThreadMetadata(context.Context, threadstore.UpdateThreadMetadataParams) (threadstore.StoredThread, error) {
	return threadstore.StoredThread{}, s.unsupported("update")
}
func (s *stubStore) ArchiveThread(context.Context, threadstore.ArchiveThreadParams) error {
	return s.unsupported("archive")
}
func (s *stubStore) UnarchiveThread(context.Context, threadstore.ArchiveThreadParams) (threadstore.StoredThread, error) {
	return threadstore.StoredThread{}, s.unsupported("unarchive")
}

var _ threadstore.ThreadStore = (*stubStore)(nil)

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// testConfig returns a minimally-valid SessionConfiguration.
func testConfig() SessionConfiguration {
	return SessionConfiguration{
		ProviderID: "openai",
		CollaborationMode: protocol.CollaborationMode{
			Settings: protocol.Settings{Model: "gpt-test"},
		},
		Cwd:              "/work",
		CodexHome:        "/home/.codex",
		BaseInstructions: "be helpful",
	}
}

// newServices builds a SessionServices with the required mocks plus the supplied
// recorder (may be nil to exercise the no-recorder path).
func newServices(t *testing.T, recorder RolloutRecorder) SessionServices {
	t.Helper()
	router, err := NewDefaultToolRouter()
	if err != nil {
		t.Fatalf("NewDefaultToolRouter: %v", err)
	}
	return SessionServices{
		ModelClient:     NewMockModelClient("gpt-test", nil),
		ToolRouter:      router,
		RolloutRecorder: recorder,
	}
}

// idGen returns a deterministic ThreadIDFactory minting tm-0, tm-1, ...
func idGen(prefix string) (ThreadIDFactory, *int) {
	n := 0
	return func() protocol.ThreadID {
		id := protocol.NewThreadID(fmt.Sprintf("%s-%d", prefix, n))
		n++
		return id
	}, &n
}

// newManager builds a ThreadManager wired to store + a per-spawn recorder
// returned by the factory. The factory records each built service so tests can
// inspect the recorder later.
func newManager(t *testing.T, store threadstore.ThreadStore, recorder RolloutRecorder) *ThreadManager {
	t.Helper()
	gen, _ := idGen("tm")
	mgr, err := NewThreadManager(ThreadManagerConfig{
		Store:          store,
		NewThreadID:    gen,
		Originator:     "codex_go_test",
		CliVersion:     "0.0.0-test",
		SessionSource:  rollout.NewExecSource(),
		InstallationID: "install-123",
		Now:            func() time.Time { return time.Unix(0, 0).UTC() },
		ServicesFactory: func(_ context.Context, _ protocol.ThreadID, _ SessionConfiguration) (SessionServices, error) {
			return newServices(t, recorder), nil
		},
	})
	if err != nil {
		t.Fatalf("NewThreadManager: %v", err)
	}
	return mgr
}

// userMsg builds a user-message rollout response item.
func userMsg(text string) rollout.RolloutItem {
	return rollout.NewResponseItem(protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "user",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: text}},
	})
}

// assistantMsg builds an assistant-message rollout response item.
func assistantMsg(text string) rollout.RolloutItem {
	return rollout.NewResponseItem(protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "assistant",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
	})
}

// turnComplete builds a TurnComplete event rollout item (a turn boundary).
func turnComplete() rollout.RolloutItem {
	return rollout.NewEventMsgItem(protocol.EventMsg{
		Type:         protocol.EventMsgKindTurnComplete,
		TurnComplete: &protocol.TurnCompleteEvent{},
	})
}

// resumedHistory builds an InitialHistory(Resumed) for a thread id with items.
func resumedHistory(id protocol.ThreadID, rolloutPath *string, items ...rollout.RolloutItem) rollout.InitialHistory {
	return rollout.InitialHistory{
		Kind: rollout.InitialHistoryKindResumed,
		Resumed: &rollout.ResumedHistory{
			ConversationID: id,
			History:        items,
			RolloutPath:    rolloutPath,
		},
	}
}

// ----------------------------------------------------------------------------
// Construction / validation
// ----------------------------------------------------------------------------

func TestNewThreadManagerValidation(t *testing.T) {
	gen, _ := idGen("x")
	factory := func(context.Context, protocol.ThreadID, SessionConfiguration) (SessionServices, error) {
		return SessionServices{}, nil
	}
	tests := []struct {
		name    string
		cfg     ThreadManagerConfig
		wantErr bool
	}{
		{
			name:    "missing store",
			cfg:     ThreadManagerConfig{ServicesFactory: factory, NewThreadID: gen},
			wantErr: true,
		},
		{
			name:    "missing factory",
			cfg:     ThreadManagerConfig{Store: &stubStore{}, NewThreadID: gen},
			wantErr: true,
		},
		{
			name:    "missing id factory",
			cfg:     ThreadManagerConfig{Store: &stubStore{}, ServicesFactory: factory},
			wantErr: true,
		},
		{
			name:    "ok with defaults",
			cfg:     ThreadManagerConfig{Store: &stubStore{}, ServicesFactory: factory, NewThreadID: gen},
			wantErr: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := NewThreadManager(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mgr.SessionSource().Kind == "" {
				t.Fatalf("expected default session source to be applied")
			}
		})
	}
}

// ----------------------------------------------------------------------------
// StartThread (new)
// ----------------------------------------------------------------------------

func TestStartThreadNew(t *testing.T) {
	rec := &recordingRecorder{}
	mgr := newManager(t, &stubStore{}, rec)
	defer mgr.ShutdownAllThreadsBounded(context.Background(), time.Second)

	nt, err := mgr.StartThread(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	if nt.ThreadID.String() != "tm-0" {
		t.Fatalf("expected fresh thread id tm-0, got %s", nt.ThreadID)
	}
	if nt.Thread == nil {
		t.Fatalf("expected non-nil thread")
	}
	if nt.SessionConfigured.ThreadID != nt.ThreadID {
		t.Fatalf("SessionConfigured thread id mismatch: %s vs %s", nt.SessionConfigured.ThreadID, nt.ThreadID)
	}
	if nt.SessionConfigured.Model != "gpt-test" {
		t.Fatalf("expected model gpt-test, got %q", nt.SessionConfigured.Model)
	}

	// A fresh thread must write exactly one session_meta line at startup.
	items := rec.items()
	if len(items) != 1 || items[0].Kind != rollout.RolloutItemKindSessionMeta {
		t.Fatalf("expected single session_meta record, got %#v", items)
	}
	meta := items[0].SessionMeta.Meta
	if meta.ID != nt.ThreadID {
		t.Fatalf("session_meta id mismatch: %s vs %s", meta.ID, nt.ThreadID)
	}
	if meta.Originator != "codex_go_test" || meta.CliVersion != "0.0.0-test" {
		t.Fatalf("session_meta originator/version not threaded: %+v", meta)
	}
	if meta.Source.Kind != rollout.SessionSourceKindExec {
		t.Fatalf("expected exec source, got %s", meta.Source.Kind)
	}
	if meta.BaseInstructions == nil || meta.BaseInstructions.Text != "be helpful" {
		t.Fatalf("expected base instructions threaded, got %+v", meta.BaseInstructions)
	}
	if meta.ForkedFromID != nil {
		t.Fatalf("new thread must not carry forked_from_id")
	}

	// The thread should be tracked and retrievable.
	got, err := mgr.GetThread(nt.ThreadID)
	if err != nil {
		t.Fatalf("GetThread after start: %v", err)
	}
	if got != nt.Thread {
		t.Fatalf("GetThread returned a different thread instance")
	}
}

func TestStartThreadNoRecorder(t *testing.T) {
	// With no recorder, the rollout path comes from history (nil for New) and no
	// records are attempted.
	mgr := newManager(t, &stubStore{}, nil)
	defer mgr.ShutdownAllThreadsBounded(context.Background(), time.Second)

	nt, err := mgr.StartThread(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	if nt.SessionConfigured.RolloutPath != nil {
		t.Fatalf("expected nil rollout path for new thread, got %v", *nt.SessionConfigured.RolloutPath)
	}
}

// ----------------------------------------------------------------------------
// Resume
// ----------------------------------------------------------------------------

func TestResumeThreadWithHistory(t *testing.T) {
	rec := &recordingRecorder{}
	mgr := newManager(t, &stubStore{}, rec)
	defer mgr.ShutdownAllThreadsBounded(context.Background(), time.Second)

	resumedID := protocol.NewThreadID("resumed-99")
	path := "/rollouts/resumed-99.jsonl"
	hist := resumedHistory(resumedID, &path, userMsg("hi"), assistantMsg("hello"), turnComplete())

	nt, err := mgr.ResumeThreadWithHistory(context.Background(), testConfig(), hist)
	if err != nil {
		t.Fatalf("ResumeThreadWithHistory: %v", err)
	}
	// A resumed thread reuses its recorded conversation id (not a fresh mint).
	if nt.ThreadID != resumedID {
		t.Fatalf("expected resumed id %s, got %s", resumedID, nt.ThreadID)
	}
	// Resume must not rewrite session_meta; only a flush is expected.
	if got := rec.items(); len(got) != 0 {
		t.Fatalf("resume must not record new items, got %#v", got)
	}
	if rec.flushes == 0 {
		t.Fatalf("resume should flush the resumed rollout")
	}
	// The reported rollout path should come from the resumed history.
	if nt.SessionConfigured.RolloutPath == nil || *nt.SessionConfigured.RolloutPath != path {
		t.Fatalf("expected rollout path %s, got %v", path, nt.SessionConfigured.RolloutPath)
	}
}

func TestResumeRunningThreadShortCircuits(t *testing.T) {
	mgr := newManager(t, &stubStore{}, nil)
	defer mgr.ShutdownAllThreadsBounded(context.Background(), time.Second)

	resumedID := protocol.NewThreadID("dup-1")
	hist := resumedHistory(resumedID, nil, userMsg("hi"))

	first, err := mgr.ResumeThreadWithHistory(context.Background(), testConfig(), hist)
	if err != nil {
		t.Fatalf("first resume: %v", err)
	}
	second, err := mgr.ResumeThreadWithHistory(context.Background(), testConfig(), hist)
	if err != nil {
		t.Fatalf("second resume: %v", err)
	}
	// The second resume of an already-running thread returns the same handle.
	if first.Thread != second.Thread {
		t.Fatalf("expected resumed short-circuit to return the same thread instance")
	}
}

// ----------------------------------------------------------------------------
// Fork
// ----------------------------------------------------------------------------

func TestForkThreadFromHistoryLineage(t *testing.T) {
	rec := &recordingRecorder{}
	mgr := newManager(t, &stubStore{}, rec)
	defer mgr.ShutdownAllThreadsBounded(context.Background(), time.Second)

	parentID := protocol.NewThreadID("parent-7")
	// Two committed turns: [u, a, complete, u2, a2, complete].
	hist := resumedHistory(parentID, nil,
		userMsg("u1"), assistantMsg("a1"), turnComplete(),
		userMsg("u2"), assistantMsg("a2"), turnComplete(),
	)

	nt, err := mgr.ForkThreadFromHistory(
		context.Background(),
		ForkBeforeNthUserMessage(1), // keep only the first turn
		testConfig(),
		hist,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("ForkThreadFromHistory: %v", err)
	}
	// Fork mints a fresh id (not the parent id).
	if nt.ThreadID == parentID {
		t.Fatalf("fork must use a fresh thread id, got the parent id")
	}
	if nt.ThreadID.String() != "tm-0" {
		t.Fatalf("expected minted id tm-0, got %s", nt.ThreadID)
	}
	// The forked-from lineage of a resumed source is the source conversation id.
	if nt.SessionConfigured.ForkedFromID == nil || *nt.SessionConfigured.ForkedFromID != parentID {
		t.Fatalf("expected forked_from_id %s, got %v", parentID, nt.SessionConfigured.ForkedFromID)
	}

	// The recorded startup should be: session_meta + the forked prefix (1 user
	// message worth, cut before the 1st 0-based user message => just first turn).
	items := rec.items()
	if len(items) == 0 || items[0].Kind != rollout.RolloutItemKindSessionMeta {
		t.Fatalf("expected leading session_meta, got %#v", items)
	}
	meta := items[0].SessionMeta.Meta
	if meta.ForkedFromID == nil || *meta.ForkedFromID != parentID {
		t.Fatalf("session_meta forked_from_id mismatch: %v", meta.ForkedFromID)
	}
	// Count user messages in the appended forked history: should be exactly 1.
	userCount := 0
	for _, it := range items[1:] {
		if it.Kind == rollout.RolloutItemKindResponseItem && it.ResponseItem != nil && it.ResponseItem.IsUserMessage() {
			userCount++
		}
	}
	if userCount != 1 {
		t.Fatalf("expected forked prefix to contain 1 user message, got %d", userCount)
	}
}

func TestForkInterruptedAppendsBoundary(t *testing.T) {
	mgr := newManager(t, &stubStore{}, nil)
	defer mgr.ShutdownAllThreadsBounded(context.Background(), time.Second)

	parentID := protocol.NewThreadID("mid-turn")
	// History that ends mid-turn: last user message has no terminating boundary.
	hist := resumedHistory(parentID, nil,
		userMsg("u1"), assistantMsg("a1"), turnComplete(),
		userMsg("u2"), // unfinished turn
	)

	nt, err := mgr.ForkThreadFromHistory(
		context.Background(),
		ForkInterrupted(),
		testConfig(),
		hist,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("ForkThreadFromHistory(Interrupted): %v", err)
	}
	if nt.SessionConfigured.ForkedFromID == nil || *nt.SessionConfigured.ForkedFromID != parentID {
		t.Fatalf("expected forked_from_id %s", parentID)
	}
	// The InitialMessages on the SessionConfigured event should include the
	// synthesized TurnAborted boundary appended by the interrupted snapshot.
	if nt.SessionConfigured.InitialMessages == nil {
		t.Fatalf("expected initial messages carrying the aborted boundary")
	}
	sawAbort := false
	for _, m := range *nt.SessionConfigured.InitialMessages {
		if m.Type == protocol.EventMsgKindTurnAborted {
			sawAbort = true
		}
	}
	if !sawAbort {
		t.Fatalf("expected a TurnAborted boundary in the forked history")
	}
}

// ----------------------------------------------------------------------------
// Rollback
// ----------------------------------------------------------------------------

func TestRollbackHistory(t *testing.T) {
	id := protocol.NewThreadID("rb")
	// Three user turns.
	base := func() rollout.InitialHistory {
		return resumedHistory(id, nil,
			userMsg("u1"), assistantMsg("a1"), turnComplete(),
			userMsg("u2"), assistantMsg("a2"), turnComplete(),
			userMsg("u3"), assistantMsg("a3"), turnComplete(),
		)
	}
	tests := []struct {
		name         string
		numTurns     int
		wantUserMsgs int
		wantKind     rollout.InitialHistoryKind
	}{
		{name: "zero is no-op", numTurns: 0, wantUserMsgs: 3, wantKind: rollout.InitialHistoryKindResumed},
		{name: "drop last turn", numTurns: 1, wantUserMsgs: 2, wantKind: rollout.InitialHistoryKindResumed},
		{name: "drop two turns", numTurns: 2, wantUserMsgs: 1, wantKind: rollout.InitialHistoryKindResumed},
		{name: "drop all turns", numTurns: 3, wantUserMsgs: 0, wantKind: rollout.InitialHistoryKindNew},
		{name: "drop beyond range", numTurns: 5, wantUserMsgs: 0, wantKind: rollout.InitialHistoryKindNew},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rollbackHistory(base(), tc.numTurns)
			if got.Kind != tc.wantKind {
				t.Fatalf("kind = %s, want %s", got.Kind, tc.wantKind)
			}
			gotUsers := len(userMessagePositionsInRollout(got.GetRolloutItems()))
			if gotUsers != tc.wantUserMsgs {
				t.Fatalf("user messages = %d, want %d", gotUsers, tc.wantUserMsgs)
			}
		})
	}
}

func TestRollbackResumesTruncatedThread(t *testing.T) {
	id := protocol.NewThreadID("rb-resume")
	path := "/rollouts/rb-resume.jsonl"
	stored := threadstore.StoredThread{
		ThreadID:    id,
		RolloutPath: &path,
		History: &threadstore.StoredThreadHistory{
			ThreadID: id,
			Items: []rollout.RolloutItem{
				userMsg("u1"), assistantMsg("a1"), turnComplete(),
				userMsg("u2"), assistantMsg("a2"), turnComplete(),
			},
		},
	}
	store := &stubStore{byRolloutPath: map[string]threadstore.StoredThread{path: stored}}
	rec := &recordingRecorder{}
	mgr := newManager(t, store, rec)
	defer mgr.ShutdownAllThreadsBounded(context.Background(), time.Second)

	nt, err := mgr.Rollback(context.Background(), testConfig(), path, 1)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	// Rollback truncates then resumes; one user turn should remain in history.
	hist := nt.Thread.Codex().Session().HistoryItems()
	users := 0
	for _, it := range hist {
		if it.IsUserMessage() {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("expected 1 user message after rolling back 1 turn, got %d", users)
	}
	if store.readByPath != 1 {
		t.Fatalf("expected exactly one rollout-path read, got %d", store.readByPath)
	}
}

// ----------------------------------------------------------------------------
// Registry: get / remove / list / internal hiding
// ----------------------------------------------------------------------------

func TestGetThreadNotFound(t *testing.T) {
	mgr := newManager(t, &stubStore{}, nil)
	_, err := mgr.GetThread(protocol.NewThreadID("nope"))
	if !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("expected ErrThreadNotFound, got %v", err)
	}
}

func TestRemoveThread(t *testing.T) {
	mgr := newManager(t, &stubStore{}, nil)
	nt, err := mgr.StartThread(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	removed := mgr.RemoveThread(nt.ThreadID)
	if removed != nt.Thread {
		t.Fatalf("RemoveThread returned a different instance")
	}
	if _, err := mgr.GetThread(nt.ThreadID); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("expected thread to be gone after remove, got %v", err)
	}
	// Removing again returns nil.
	if again := mgr.RemoveThread(nt.ThreadID); again != nil {
		t.Fatalf("expected nil on second remove, got %v", again)
	}
	_ = nt.Thread.ShutdownAndWait(context.Background())
}

func TestListThreadIDsHidesInternal(t *testing.T) {
	mgr := newManager(t, &stubStore{}, nil)
	defer mgr.ShutdownAllThreadsBounded(context.Background(), time.Second)

	// Two externally-visible threads (exec source via manager default).
	a, err := mgr.StartThread(context.Background(), testConfig())
	if err != nil {
		t.Fatalf("start a: %v", err)
	}
	internalSource := rollout.SessionSource{Kind: rollout.SessionSourceKindInternal}
	b, err := mgr.StartThreadWithOptions(context.Background(), StartThreadOptions{
		Configuration:  testConfig(),
		InitialHistory: rollout.NewInitialHistory(),
		SessionSource:  &internalSource,
	})
	if err != nil {
		t.Fatalf("start b (internal): %v", err)
	}

	ids := mgr.ListThreadIDs()
	if len(ids) != 1 || ids[0] != a.ThreadID {
		t.Fatalf("expected only the external thread %s listed, got %v", a.ThreadID, ids)
	}
	// Internal threads are also hidden from GetThread.
	if _, err := mgr.GetThread(b.ThreadID); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("internal thread should be hidden from GetThread, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// Shutdown
// ----------------------------------------------------------------------------

func TestShutdownAllThreadsBounded(t *testing.T) {
	mgr := newManager(t, &stubStore{}, nil)

	var started []protocol.ThreadID
	for i := 0; i < 3; i++ {
		nt, err := mgr.StartThread(context.Background(), testConfig())
		if err != nil {
			t.Fatalf("StartThread %d: %v", i, err)
		}
		started = append(started, nt.ThreadID)
	}

	report := mgr.ShutdownAllThreadsBounded(context.Background(), 2*time.Second)
	if len(report.Completed) != 3 {
		t.Fatalf("expected 3 completed shutdowns, got %d (%v)", len(report.Completed), report)
	}
	if len(report.SubmitFailed) != 0 || len(report.TimedOut) != 0 {
		t.Fatalf("expected no failures/timeouts, got %+v", report)
	}
	// Completed ids are sorted and removed from the registry.
	if len(mgr.ListThreadIDs()) != 0 {
		t.Fatalf("expected registry empty after shutdown")
	}
	for _, id := range started {
		if _, err := mgr.GetThread(id); !errors.Is(err, ErrThreadNotFound) {
			t.Fatalf("thread %s should be gone, got %v", id, err)
		}
	}
}

// ----------------------------------------------------------------------------
// Error mapping
// ----------------------------------------------------------------------------

func TestInitialHistoryReadErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		store  *stubStore
		path   string
		wantIs error
	}{
		{
			name:   "thread not found",
			store:  &stubStore{readErr: &threadstore.Error{Kind: threadstore.ErrorKindThreadNotFound, ThreadID: protocol.NewThreadID("x")}},
			path:   "/missing",
			wantIs: ErrThreadNotFound,
		},
		{
			name:   "invalid request (unknown path)",
			store:  &stubStore{byRolloutPath: map[string]threadstore.StoredThread{}},
			path:   "/unknown",
			wantIs: ErrThreadStoreInvalidRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mgr := newManager(t, tc.store, nil)
			_, err := mgr.ForkThread(context.Background(), ForkInterrupted(), testConfig(), tc.path, nil, false)
			if !errors.Is(err, tc.wantIs) {
				t.Fatalf("expected error matching %v, got %v", tc.wantIs, err)
			}
		})
	}
}

func TestStoredThreadToInitialHistoryMissingHistory(t *testing.T) {
	stored := threadstore.StoredThread{ThreadID: protocol.NewThreadID("no-hist")}
	_, err := storedThreadToInitialHistory(stored, nil)
	if err == nil {
		t.Fatalf("expected error when stored thread lacks persisted history")
	}
}

func TestServicesFactoryErrorPropagates(t *testing.T) {
	gen, _ := idGen("tm")
	mgr, err := NewThreadManager(ThreadManagerConfig{
		Store:       &stubStore{},
		NewThreadID: gen,
		ServicesFactory: func(context.Context, protocol.ThreadID, SessionConfiguration) (SessionServices, error) {
			return SessionServices{}, errors.New("boom")
		},
	})
	if err != nil {
		t.Fatalf("NewThreadManager: %v", err)
	}
	_, err = mgr.StartThread(context.Background(), testConfig())
	if err == nil {
		t.Fatalf("expected services factory error to propagate")
	}
}

// ----------------------------------------------------------------------------
// userMessagePositionsInRollout / rollback prefix helpers
// ----------------------------------------------------------------------------

func TestUserMessagePositionsInRollout(t *testing.T) {
	items := []rollout.RolloutItem{
		assistantMsg("a0"),
		userMsg("u0"),
		turnComplete(),
		userMsg("u1"),
	}
	got := userMessagePositionsInRollout(items)
	want := []int{1, 3}
	if len(got) != len(want) {
		t.Fatalf("positions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("positions[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}
