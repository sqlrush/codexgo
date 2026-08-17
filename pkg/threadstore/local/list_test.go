package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/threadstore"
)

// ptr returns a pointer to v, for building patch fields in tests.
func ptr[T any](v T) *T { return &v }

// validTID returns a fixed, syntactically valid UUID thread id. The local store
// resolves rollout files by UUID, so list/archive helpers require valid ids.
func validTID() protocol.ThreadID {
	return protocol.NewThreadID("00000000-0000-0000-0000-0000000000ff")
}

// uuidFor builds a deterministic UUID string from a small integer so tests can
// create several distinct, valid thread ids.
func uuidFor(n int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
}

// newScanStore builds a state-DB-less local store rooted at a temp Codex home.
func newScanStore(t *testing.T) *LocalThreadStore {
	t.Helper()
	return NewLocalThreadStore(LocalThreadStoreConfig{
		CodexHome:              t.TempDir(),
		DefaultModelProviderID: "test-provider",
		Originator:             "codexgo-test",
		CliVersion:             "test",
	}, nil)
}

// createPersistedThread creates a live thread, appends a user message (so the
// thread has a non-empty preview), persists it, and shuts the writer down so the
// rollout file is discoverable by the scan path. It returns the thread id and
// its rollout path.
func createPersistedThread(t *testing.T, store *LocalThreadStore, n int, message string) (protocol.ThreadID, string) {
	t.Helper()
	ctx := context.Background()
	threadID := protocol.NewThreadID(uuidFor(n))
	cwd := store.config.CodexHome

	if err := store.CreateThread(ctx, threadstore.CreateThreadParams{
		ThreadID: threadID,
		Source:   rollout.NewCliSource(),
		Metadata: threadstore.ThreadPersistenceMetadata{
			Cwd:           &cwd,
			ModelProvider: "test-provider",
			MemoryMode:    protocol.ThreadMemoryModeEnabled,
		},
	}); err != nil {
		t.Fatalf("create thread %d: %v", n, err)
	}

	userMsg := rollout.NewEventMsgItem(protocol.EventMsg{
		Type:        protocol.EventMsgKindUserMessage,
		UserMessage: &protocol.UserMessageEvent{Message: message},
	})
	if err := store.AppendItems(ctx, threadstore.AppendThreadItemsParams{
		ThreadID: threadID,
		Items:    []rollout.RolloutItem{userMsg},
	}); err != nil {
		t.Fatalf("append items %d: %v", n, err)
	}
	if err := store.PersistThread(ctx, threadID); err != nil {
		t.Fatalf("persist thread %d: %v", n, err)
	}
	if err := store.FlushThread(ctx, threadID); err != nil {
		t.Fatalf("flush thread %d: %v", n, err)
	}
	path, err := store.LiveRolloutPath(threadID)
	if err != nil {
		t.Fatalf("live rollout path %d: %v", n, err)
	}
	if err := store.ShutdownThread(ctx, threadID); err != nil {
		t.Fatalf("shutdown thread %d: %v", n, err)
	}
	return threadID, path
}

// TestLocalListThreadsScanOrderingAndPaging verifies the scan fallback honors the
// requested ordering and page size, and that the cursor continues the list.
func TestLocalListThreadsScanOrderingAndPaging(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()

	// Create three threads with distinct previews. They are created in order so
	// their rollout file mod-times (used for updated_at) increase with n.
	for n := 1; n <= 3; n++ {
		createPersistedThread(t, store, n, fmt.Sprintf("message %d", n))
	}

	tests := []struct {
		name      string
		params    threadstore.ListThreadsParams
		wantCount int
		wantNext  bool
	}{
		{
			name:      "first page of two",
			params:    threadstore.ListThreadsParams{PageSize: 2, SortKey: threadstore.ThreadSortKeyUpdatedAt, SortDirection: threadstore.SortDirectionDesc},
			wantCount: 2,
			wantNext:  true,
		},
		{
			name:      "full page",
			params:    threadstore.ListThreadsParams{PageSize: 10, SortKey: threadstore.ThreadSortKeyUpdatedAt, SortDirection: threadstore.SortDirectionDesc},
			wantCount: 3,
			wantNext:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := store.ListThreads(ctx, tc.params)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(page.Items) != tc.wantCount {
				t.Fatalf("items = %d, want %d", len(page.Items), tc.wantCount)
			}
			if (page.NextCursor != nil) != tc.wantNext {
				t.Fatalf("next cursor present = %v, want %v", page.NextCursor != nil, tc.wantNext)
			}
			// Verify descending order by the sort key.
			for i := 1; i < len(page.Items); i++ {
				prev := sortTimestamp(page.Items[i-1], tc.params.SortKey)
				cur := sortTimestamp(page.Items[i], tc.params.SortKey)
				if cur.After(prev) {
					t.Fatalf("descending order violated at %d", i)
				}
			}
		})
	}

	// The cursor from the first page must continue to the remaining item.
	first, err := store.ListThreads(ctx, threadstore.ListThreadsParams{PageSize: 2, SortKey: threadstore.ThreadSortKeyUpdatedAt, SortDirection: threadstore.SortDirectionDesc})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if first.NextCursor == nil {
		t.Fatalf("expected a next cursor")
	}
	second, err := store.ListThreads(ctx, threadstore.ListThreadsParams{
		PageSize:      2,
		Cursor:        first.NextCursor,
		SortKey:       threadstore.ThreadSortKeyUpdatedAt,
		SortDirection: threadstore.SortDirectionDesc,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 1 {
		t.Fatalf("second page items = %d, want 1", len(second.Items))
	}
}

// TestLocalSearchThreadsScan verifies that search filters by the visible content
// substring and requires a non-empty term.
func TestLocalSearchThreadsScan(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()

	createPersistedThread(t, store, 11, "needle in a haystack")
	createPersistedThread(t, store, 12, "completely different")

	tests := []struct {
		name      string
		term      string
		wantCount int
		wantErr   bool
	}{
		{name: "matches one", term: "needle", wantCount: 1},
		{name: "matches none", term: "absent", wantCount: 0},
		{name: "case insensitive", term: "NEEDLE", wantCount: 1},
		{name: "empty term errors", term: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{PageSize: 10, SearchTerm: tc.term})
			if tc.wantErr {
				var storeErr *threadstore.Error
				if !errors.As(err, &storeErr) || storeErr.Kind != threadstore.ErrorKindInvalidRequest {
					t.Fatalf("expected InvalidRequest, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(page.Items) != tc.wantCount {
				t.Fatalf("results = %d, want %d", len(page.Items), tc.wantCount)
			}
			for _, item := range page.Items {
				if item.Snippet == "" {
					t.Fatalf("expected a non-empty snippet")
				}
			}
		})
	}
}

// TestLocalArchiveUnarchiveRoundTrip verifies archive moves the rollout file into
// the archived tree and unarchive restores it, returning the restored thread.
func TestLocalArchiveUnarchiveRoundTrip(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()

	threadID, activePath := createPersistedThread(t, store, 21, "to be archived")

	if err := store.ArchiveThread(ctx, threadstore.ArchiveThreadParams{ThreadID: threadID}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := os.Stat(activePath); !os.IsNotExist(err) {
		t.Fatalf("active rollout should be gone, stat err = %v", err)
	}
	archivedPath := filepath.Join(store.config.CodexHome, rollout.ArchivedSessionsSubdir, filepath.Base(activePath))
	if _, err := os.Stat(archivedPath); err != nil {
		t.Fatalf("archived rollout should exist: %v", err)
	}

	// The thread now appears only in the archived listing.
	active, err := store.ListThreads(ctx, threadstore.ListThreadsParams{PageSize: 10})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active.Items) != 0 {
		t.Fatalf("active listing should be empty, got %d", len(active.Items))
	}
	archived, err := store.ListThreads(ctx, threadstore.ListThreadsParams{PageSize: 10, Archived: true})
	if err != nil {
		t.Fatalf("list archived: %v", err)
	}
	if len(archived.Items) != 1 || archived.Items[0].ThreadID != threadID {
		t.Fatalf("archived listing mismatch: %+v", archived.Items)
	}
	if archived.Items[0].ArchivedAt == nil {
		t.Fatalf("archived thread should carry an archived_at timestamp")
	}

	restored, err := store.UnarchiveThread(ctx, threadstore.ArchiveThreadParams{ThreadID: threadID})
	if err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if restored.ThreadID != threadID {
		t.Fatalf("restored thread id = %s, want %s", restored.ThreadID, threadID)
	}
	if restored.ArchivedAt != nil {
		t.Fatalf("restored thread should not be archived")
	}
	if _, err := os.Stat(archivedPath); !os.IsNotExist(err) {
		t.Fatalf("archived rollout should be gone after unarchive, stat err = %v", err)
	}
	if restored.RolloutPath == nil {
		t.Fatalf("restored thread should have a rollout path")
	}
	if _, err := os.Stat(*restored.RolloutPath); err != nil {
		t.Fatalf("restored rollout should exist: %v", err)
	}
}

// TestLocalLoadHistoryScan verifies LoadHistory replays the persisted rollout
// items for a thread resolved from the sessions tree.
func TestLocalLoadHistoryScan(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()

	threadID, _ := createPersistedThread(t, store, 31, "history please")

	hist, err := store.LoadHistory(ctx, threadstore.LoadThreadHistoryParams{ThreadID: threadID})
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if hist.ThreadID != threadID {
		t.Fatalf("history thread id = %s, want %s", hist.ThreadID, threadID)
	}
	if len(hist.Items) == 0 {
		t.Fatalf("expected persisted history items")
	}
	// The persisted rollout must contain the session-meta line and the user msg.
	var sawSessionMeta, sawUserMessage bool
	for _, item := range hist.Items {
		switch item.Kind {
		case rollout.RolloutItemKindSessionMeta:
			sawSessionMeta = true
		case rollout.RolloutItemKindEventMsg:
			if item.EventMsg != nil && item.EventMsg.Type == protocol.EventMsgKindUserMessage {
				sawUserMessage = true
			}
		}
	}
	if !sawSessionMeta || !sawUserMessage {
		t.Fatalf("history missing expected items: meta=%v user=%v", sawSessionMeta, sawUserMessage)
	}
}
