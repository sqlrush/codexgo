// Package contracttest is the behavioural contract suite for
// [threadstore.ThreadStore] implementations. The in-memory store runs it as
// the reference; the local file store and external backends (e.g. a
// PostgreSQL store layered on the airush core) run the same suite so the
// storage-neutral semantics core relies on — create/append/load ordering,
// metadata patch application, unsupported-operation signalling, delete and
// bulk-operation rules — stay identical across implementations.
//
// The suite only asserts what the trait contract guarantees. Store-specific
// behaviour (archive moving files, search snippets, section ordering) belongs
// to the implementation's own tests.
package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/threadstore"
)

// Config describes how to build a store under test.
type Config struct {
	// NewStore returns a fresh, empty store. Required.
	NewStore func(t *testing.T) threadstore.ThreadStore
	// NewCreateParams builds create params for a thread id. Optional; the
	// default builds a minimal legacy thread with a temp-dir cwd, which is what
	// stores need at minimum. Override when the store requires more (e.g. a
	// specific provider or metadata shape).
	NewCreateParams func(t *testing.T, id protocol.ThreadID) threadstore.CreateThreadParams
	// SupportsDelete reports whether DeleteThread is implemented. Stores that
	// stage delete for later (the local file store today) set false and the
	// suite asserts DeleteThread reports Unsupported instead of exercising it.
	SupportsDelete bool
	// PersistsParentThreadID reports whether CreateThreadParams.ParentThreadID
	// round-trips through ReadThread. The 0.136-shaped local file store does not
	// persist it yet; paginated/PG stores must.
	PersistsParentThreadID bool
	// TracksRecency reports whether the store persists a recency watermark
	// distinct from updated_at (0.147 recency_at) so AdvanceRecencyAt round-trips
	// into StoredThread.RecencyAt. The 0.136-shaped local store reports
	// UpdatedAt as recency and sets false.
	TracksRecency bool
	// TracksArchivedAt reports whether ArchiveThread sets StoredThread.ArchivedAt
	// (the in-memory debug store does not). When true the suite asserts the
	// archive/unarchive round trip on ArchivedAt.
	TracksArchivedAt bool
}

// Run runs the contract suite.
func Run(t *testing.T, cfg Config) {
	t.Helper()
	if cfg.NewStore == nil {
		t.Fatal("contracttest: Config.NewStore is required")
	}
	if cfg.NewCreateParams == nil {
		cfg.NewCreateParams = DefaultCreateParams
	}
	s := &suite{cfg: cfg}
	t.Run("CreateReadRoundTrip", s.testCreateReadRoundTrip)
	t.Run("AppendLoadOrder", s.testAppendLoadOrder)
	t.Run("ReadUnknownIsThreadNotFound", s.testReadUnknown)
	t.Run("LifecycleOpsOnLiveThread", s.testLifecycleOps)
	t.Run("UpdateMetadataPatch", s.testUpdateMetadataPatch)
	t.Run("ListThreadsContainsCreated", s.testListThreads)
	t.Run("ArchiveUnarchive", s.testArchiveUnarchive)
	t.Run("ArchiveThreadsInOrder", s.testArchiveThreads)
	t.Run("Delete", s.testDelete)
	t.Run("OptionalOperationsSignalUnsupported", s.testOptionalOps)
	t.Run("HistoryModeDefault", s.testHistoryMode)
}

// DefaultCreateParams builds minimal legacy create params: a temp cwd, the
// default CLI session source, "test" provider.
func DefaultCreateParams(t *testing.T, id protocol.ThreadID) threadstore.CreateThreadParams {
	t.Helper()
	cwd := t.TempDir()
	return threadstore.CreateThreadParams{
		SessionID: id.ToSessionID(),
		ThreadID:  id,
		Source:    rollout.DefaultSessionSource(),
		Metadata: threadstore.ThreadPersistenceMetadata{
			Cwd:           &cwd,
			ModelProvider: "test",
		},
	}
}

// ThreadID builds a deterministic, syntactically valid UUID thread id.
func ThreadID(n int) protocol.ThreadID {
	const base = "00000000-0000-4000-8000-"
	suffix := ""
	for _, c := range []byte{byte('0' + (n/100)%10), byte('0' + (n/10)%10), byte('0' + n%10)} {
		suffix += string(c)
	}
	return protocol.NewThreadID(base + "000000000" + suffix)
}

// UserMessage builds a rollout response item carrying one user text message.
func UserMessage(text string) rollout.RolloutItem {
	return rollout.NewResponseItem(protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "user",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: text}},
	})
}

// AssistantMessage builds a rollout response item carrying one assistant text message.
func AssistantMessage(text string) rollout.RolloutItem {
	return rollout.NewResponseItem(protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "assistant",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
	})
}

type suite struct {
	cfg Config
}

func (s *suite) create(t *testing.T, store threadstore.ThreadStore, n int) protocol.ThreadID {
	t.Helper()
	id := ThreadID(n)
	if err := store.CreateThread(context.Background(), s.cfg.NewCreateParams(t, id)); err != nil {
		t.Fatalf("CreateThread(%d): %v", n, err)
	}
	return id
}

// createLive creates a thread and makes it durable the way a real session
// does: one user message appended and flushed (stores with lazy
// materialization, like the local rollout file, only become readable once an
// item is durable) and the preview recorded through UpdateThreadMetadata (core
// syncs the preview from the first user message; stores list only threads
// that have one).
func (s *suite) createLive(t *testing.T, store threadstore.ThreadStore, n int) protocol.ThreadID {
	t.Helper()
	ctx := context.Background()
	id := s.create(t, store, n)
	if err := store.AppendItems(ctx, threadstore.AppendThreadItemsParams{ThreadID: id, Items: []rollout.RolloutItem{UserMessage("hello")}}); err != nil {
		t.Fatalf("AppendItems(%d): %v", n, err)
	}
	if err := store.FlushThread(ctx, id); err != nil {
		t.Fatalf("FlushThread(%d): %v", n, err)
	}
	preview := "hello"
	if _, err := store.UpdateThreadMetadata(ctx, threadstore.UpdateThreadMetadataParams{ThreadID: id, Patch: threadstore.ThreadMetadataPatch{Preview: &preview}}); err != nil {
		t.Fatalf("UpdateThreadMetadata(preview, %d): %v", n, err)
	}
	return id
}

// requireNotFound accepts either ThreadNotFound or InvalidRequest: the Rust
// local store reports an unknown id as "no rollout found" (invalid request)
// while in-memory/PG stores report ThreadNotFound.
func requireNotFound(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s should fail", what)
	}
	if !isKind(err, threadstore.ErrorKindThreadNotFound) && !isKind(err, threadstore.ErrorKindInvalidRequest) {
		t.Fatalf("%s = %v, want ThreadNotFound or InvalidRequest", what, err)
	}
}

func requireKind(t *testing.T, err error, kind threadstore.ErrorKind, what string) {
	t.Helper()
	var storeErr *threadstore.Error
	if !errors.As(err, &storeErr) || storeErr.Kind != kind {
		t.Fatalf("%s: got %v, want store error kind %v", what, err, kind)
	}
}

func isKind(err error, kind threadstore.ErrorKind) bool {
	var storeErr *threadstore.Error
	return errors.As(err, &storeErr) && storeErr.Kind == kind
}

func (s *suite) testCreateReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	parent := s.createLive(t, store, 1)
	id := ThreadID(2)
	params := s.cfg.NewCreateParams(t, id)
	params.ParentThreadID = &parent
	params.ForkedFromID = &parent
	if err := store.CreateThread(ctx, params); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if err := store.AppendItems(ctx, threadstore.AppendThreadItemsParams{ThreadID: id, Items: []rollout.RolloutItem{UserMessage("hello")}}); err != nil {
		t.Fatalf("AppendItems: %v", err)
	}
	if err := store.FlushThread(ctx, id); err != nil {
		t.Fatalf("FlushThread: %v", err)
	}
	thread, err := store.ReadThread(ctx, threadstore.ReadThreadParams{ThreadID: id, IncludeHistory: true})
	if err != nil {
		t.Fatalf("ReadThread: %v", err)
	}
	if thread.ThreadID != id {
		t.Errorf("ThreadID = %v, want %v", thread.ThreadID, id)
	}
	if thread.ForkedFromID == nil || *thread.ForkedFromID != parent {
		t.Errorf("ForkedFromID = %v, want %v", thread.ForkedFromID, parent)
	}
	if s.cfg.PersistsParentThreadID && (thread.ParentThreadID == nil || *thread.ParentThreadID != parent) {
		t.Errorf("ParentThreadID = %v, want %v", thread.ParentThreadID, parent)
	}
	if !thread.HistoryMode.IsValid() {
		t.Errorf("HistoryMode = %q, want a valid mode", thread.HistoryMode)
	}
	if thread.History == nil {
		t.Errorf("IncludeHistory should populate History")
	} else {
		assertMessageTexts(t, "ReadThread history", thread.History.Items, []string{"hello"})
	}
	if thread.RecencyAt.IsZero() {
		t.Errorf("RecencyAt should be populated")
	}
	summary, err := store.ReadThread(ctx, threadstore.ReadThreadParams{ThreadID: id})
	if err != nil {
		t.Fatalf("ReadThread summary: %v", err)
	}
	if summary.History != nil {
		t.Errorf("summary read should not include history")
	}
}

func (s *suite) testAppendLoadOrder(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	id := s.create(t, store, 3)
	items := []rollout.RolloutItem{UserMessage("first"), AssistantMessage("second"), UserMessage("third")}
	if err := store.AppendItems(ctx, threadstore.AppendThreadItemsParams{ThreadID: id, Items: items[:2]}); err != nil {
		t.Fatalf("AppendItems: %v", err)
	}
	if err := store.AppendItems(ctx, threadstore.AppendThreadItemsParams{ThreadID: id, Items: items[2:]}); err != nil {
		t.Fatalf("AppendItems: %v", err)
	}
	if err := store.FlushThread(ctx, id); err != nil {
		t.Fatalf("FlushThread: %v", err)
	}
	history, err := store.LoadHistory(ctx, threadstore.LoadThreadHistoryParams{ThreadID: id})
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	assertMessageTexts(t, "LoadHistory", history.Items, []string{"first", "second", "third"})

	modelCtx, err := store.LoadLatestModelContext(ctx, threadstore.LoadThreadHistoryParams{ThreadID: id})
	if err != nil {
		t.Fatalf("LoadLatestModelContext: %v", err)
	}
	if modelCtx.ThreadID != id {
		t.Errorf("model context ThreadID = %v, want %v", modelCtx.ThreadID, id)
	}
	// The contract allows a resumable suffix; it must be a suffix of the
	// history in replay order and end with the latest item.
	got := messageTexts(modelCtx.Items)
	if len(got) == 0 || got[len(got)-1] != "third" {
		t.Errorf("model context items = %v, want a suffix ending in the latest item", got)
	}
}

func (s *suite) testReadUnknown(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	unknown := ThreadID(999)
	_, err := store.ReadThread(ctx, threadstore.ReadThreadParams{ThreadID: unknown})
	requireNotFound(t, err, "ReadThread(unknown)")
	_, err = store.LoadHistory(ctx, threadstore.LoadThreadHistoryParams{ThreadID: unknown})
	requireNotFound(t, err, "LoadHistory(unknown)")
}

func (s *suite) testLifecycleOps(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	id := s.create(t, store, 4)
	if err := store.AppendItems(ctx, threadstore.AppendThreadItemsParams{ThreadID: id, Items: []rollout.RolloutItem{UserMessage("x")}}); err != nil {
		t.Fatalf("AppendItems: %v", err)
	}
	if err := store.PersistThread(ctx, id); err != nil {
		t.Fatalf("PersistThread: %v", err)
	}
	if err := store.FlushThread(ctx, id); err != nil {
		t.Fatalf("FlushThread: %v", err)
	}
	if err := store.ShutdownThread(ctx, id); err != nil {
		t.Fatalf("ShutdownThread: %v", err)
	}
	// After shutdown the thread is durable and readable; resume reopens it.
	if _, err := store.ReadThread(ctx, threadstore.ReadThreadParams{ThreadID: id}); err != nil {
		t.Fatalf("ReadThread after shutdown: %v", err)
	}
	if err := store.ResumeThread(ctx, threadstore.ResumeThreadParams{ThreadID: id}); err != nil {
		// Some stores require path/history on resume; only a well-formed
		// resume must succeed, so accept InvalidRequest here.
		if !isKind(err, threadstore.ErrorKindInvalidRequest) {
			t.Fatalf("ResumeThread: %v", err)
		}
		return
	}
	if err := store.DiscardThread(ctx, id); err != nil {
		t.Fatalf("DiscardThread: %v", err)
	}
	if _, err := store.ReadThread(ctx, threadstore.ReadThreadParams{ThreadID: id}); err != nil {
		t.Fatalf("ReadThread after discard should keep durable data: %v", err)
	}
}

func (s *suite) testUpdateMetadataPatch(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	id := s.create(t, store, 5)
	name := "renamed"
	preview := "hello there"
	model := "gpt-test"
	effort := protocol.ReasoningEffortHigh
	recency := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	updated, err := store.UpdateThreadMetadata(ctx, threadstore.UpdateThreadMetadataParams{
		ThreadID: id,
		Patch: threadstore.ThreadMetadataPatch{
			Name:             threadstore.SetClearable(name),
			Preview:          &preview,
			Model:            &model,
			ReasoningEffort:  threadstore.SetClearable(effort),
			AdvanceRecencyAt: &recency,
		},
	})
	if err != nil {
		if isKind(err, threadstore.ErrorKindInvalidRequest) {
			t.Skip("store requires a metadata backend for UpdateThreadMetadata")
		}
		t.Fatalf("UpdateThreadMetadata: %v", err)
	}
	if updated.Name == nil || *updated.Name != name {
		t.Errorf("Name = %v, want %q", updated.Name, name)
	}
	if updated.Preview != preview {
		t.Errorf("Preview = %q, want %q", updated.Preview, preview)
	}
	if updated.Model == nil || *updated.Model != model {
		t.Errorf("Model = %v, want %q", updated.Model, model)
	}
	if updated.ReasoningEffort == nil || *updated.ReasoningEffort != effort {
		t.Errorf("ReasoningEffort = %v, want %v", updated.ReasoningEffort, effort)
	}
	if s.cfg.TracksRecency && !updated.RecencyAt.Equal(recency) {
		t.Errorf("RecencyAt = %v, want %v (advance_recency_at)", updated.RecencyAt, recency)
	}
	// Clearing the reasoning effort removes it; an empty patch is a no-op.
	cleared, err := store.UpdateThreadMetadata(ctx, threadstore.UpdateThreadMetadataParams{
		ThreadID: id,
		Patch:    threadstore.ThreadMetadataPatch{ReasoningEffort: threadstore.ClearField[protocol.ReasoningEffort]()},
	})
	if err != nil {
		t.Fatalf("UpdateThreadMetadata(clear): %v", err)
	}
	if cleared.ReasoningEffort != nil {
		t.Errorf("ReasoningEffort after clear = %v, want nil", cleared.ReasoningEffort)
	}
	if cleared.Name == nil || *cleared.Name != name {
		t.Errorf("Name after unrelated patch = %v, want %q retained", cleared.Name, name)
	}
}

func (s *suite) testListThreads(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	a := s.createLive(t, store, 6)
	b := s.createLive(t, store, 7)
	page, err := store.ListThreads(ctx, threadstore.ListThreadsParams{PageSize: 10})
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	seen := map[protocol.ThreadID]bool{}
	for _, th := range page.Items {
		seen[th.ThreadID] = true
	}
	if !seen[a] || !seen[b] {
		t.Errorf("ListThreads items = %v, want to contain %v and %v", ids(page.Items), a, b)
	}
}

func (s *suite) testArchiveUnarchive(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	id := s.createLive(t, store, 8)
	if err := store.ArchiveThread(ctx, threadstore.ArchiveThreadParams{ThreadID: id}); err != nil {
		t.Fatalf("ArchiveThread: %v", err)
	}
	if s.cfg.TracksArchivedAt {
		archived, err := store.ReadThread(ctx, threadstore.ReadThreadParams{ThreadID: id, IncludeArchived: true})
		if err != nil {
			t.Fatalf("ReadThread(archived): %v", err)
		}
		if archived.ArchivedAt == nil {
			t.Errorf("ArchivedAt should be set after ArchiveThread")
		}
	}
	restored, err := store.UnarchiveThread(ctx, threadstore.ArchiveThreadParams{ThreadID: id})
	if err != nil {
		t.Fatalf("UnarchiveThread: %v", err)
	}
	if restored.ThreadID != id {
		t.Errorf("UnarchiveThread returned %v, want %v", restored.ThreadID, id)
	}
	if s.cfg.TracksArchivedAt && restored.ArchivedAt != nil {
		t.Errorf("ArchivedAt after unarchive = %v, want nil", restored.ArchivedAt)
	}
}

func (s *suite) testArchiveThreads(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	a := s.createLive(t, store, 9)
	b := s.createLive(t, store, 10)
	got, err := store.ArchiveThreads(ctx, threadstore.ArchiveThreadsParams{ThreadIDs: []protocol.ThreadID{a, b}})
	if err != nil {
		t.Fatalf("ArchiveThreads: %v", err)
	}
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("ArchiveThreads = %v, want [%v %v] in order", got, a, b)
	}
}

func (s *suite) testDelete(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	id := s.createLive(t, store, 11)
	if !s.cfg.SupportsDelete {
		err := store.DeleteThread(ctx, threadstore.DeleteThreadParams{ThreadID: id})
		requireKind(t, err, threadstore.ErrorKindUnsupported, "DeleteThread (store without delete support)")
		return
	}
	if err := store.DeleteThread(ctx, threadstore.DeleteThreadParams{ThreadID: id}); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	if _, err := store.ReadThread(ctx, threadstore.ReadThreadParams{ThreadID: id, IncludeArchived: true}); err == nil {
		t.Fatalf("ReadThread after delete should fail")
	} else {
		requireKind(t, err, threadstore.ErrorKindThreadNotFound, "ReadThread after delete")
	}
	err := store.DeleteThread(ctx, threadstore.DeleteThreadParams{ThreadID: id})
	requireKind(t, err, threadstore.ErrorKindThreadNotFound, "DeleteThread twice")
	// DeleteThreads treats missing members as deleted and deletes the rest.
	other := s.createLive(t, store, 12)
	if err := store.DeleteThreads(ctx, threadstore.DeleteThreadsParams{ThreadIDs: []protocol.ThreadID{id, other}}); err != nil {
		t.Fatalf("DeleteThreads with a missing member: %v", err)
	}
	if _, err := store.ReadThread(ctx, threadstore.ReadThreadParams{ThreadID: other}); err == nil {
		t.Fatalf("second member should be deleted")
	}
}

func (s *suite) testOptionalOps(t *testing.T) {
	ctx := context.Background()
	store := s.cfg.NewStore(t)
	id := s.create(t, store, 13)
	if !store.SupportsThreadSections() {
		_, err := store.ListThreadSections(ctx, threadstore.ListThreadSectionsParams{Limit: 10})
		requireKind(t, err, threadstore.ErrorKindUnsupported, "ListThreadSections without section support")
		_, err = store.CreateThreadSection(ctx, threadstore.CreateThreadSectionParams{Name: "x"})
		requireKind(t, err, threadstore.ErrorKindUnsupported, "CreateThreadSection without section support")
		err = store.MoveThreadToSection(ctx, threadstore.MoveThreadToSectionParams{ThreadID: id})
		requireKind(t, err, threadstore.ErrorKindUnsupported, "MoveThreadToSection without section support")
	}
	if !store.SupportsPaginatedHistoryLists() {
		_, err := store.ListTurns(ctx, threadstore.ListTurnsParams{ThreadID: id, PageSize: 10})
		requireKind(t, err, threadstore.ErrorKindUnsupported, "ListTurns without paginated support")
		_, err = store.ListItems(ctx, threadstore.ListItemsParams{ThreadID: id, PageSize: 10})
		requireKind(t, err, threadstore.ErrorKindUnsupported, "ListItems without paginated support")
	}
	// Search and occurrence search either work or signal Unsupported — never
	// a silent empty page from a store that does not implement them.
	if _, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{PageSize: 10, SearchTerm: "x"}); err != nil {
		requireKind(t, err, threadstore.ErrorKindUnsupported, "SearchThreads")
	}
	if _, err := store.SearchThreadOccurrences(ctx, threadstore.SearchThreadOccurrencesParams{ThreadID: id, SearchTerm: "x", PageSize: 10}); err != nil {
		requireKind(t, err, threadstore.ErrorKindUnsupported, "SearchThreadOccurrences")
	}
	if _, err := store.PrepareFork(ctx, threadstore.PrepareForkParams{ThreadID: id, Boundary: threadstore.ForkBoundary{Kind: threadstore.ForkBoundaryLatest}}); err != nil {
		requireKind(t, err, threadstore.ErrorKindUnsupported, "PrepareFork")
	}
}

func (s *suite) testHistoryMode(t *testing.T) {
	store := s.cfg.NewStore(t)
	if mode := store.DefaultHistoryMode(); !mode.IsValid() {
		t.Fatalf("DefaultHistoryMode = %q, want legacy or paginated", mode)
	}
}

func messageTexts(items []rollout.RolloutItem) []string {
	var out []string
	for _, it := range items {
		if it.Kind != rollout.RolloutItemKindResponseItem || it.ResponseItem == nil {
			continue
		}
		for _, c := range it.ResponseItem.Content {
			out = append(out, c.Text)
		}
	}
	return out
}

func assertMessageTexts(t *testing.T, what string, items []rollout.RolloutItem, want []string) {
	t.Helper()
	got := messageTexts(items)
	if len(got) != len(want) {
		t.Fatalf("%s texts = %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s texts = %v, want %v", what, got, want)
		}
	}
}

func ids(threads []threadstore.StoredThread) []protocol.ThreadID {
	out := make([]protocol.ThreadID, 0, len(threads))
	for _, th := range threads {
		out = append(out, th.ThreadID)
	}
	return out
}
