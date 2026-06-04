package threadstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/state"
)

// newStateDBStore builds a local store backed by an initialized state runtime
// rooted at a temp Codex home.
func newStateDBStore(t *testing.T) (*LocalThreadStore, *state.StateRuntime) {
	t.Helper()
	ctx := context.Background()
	home := t.TempDir()
	runtime, err := state.InitRuntime(ctx, home, "test-provider")
	if err != nil {
		t.Fatalf("init state runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	store := NewLocalThreadStore(LocalThreadStoreConfig{
		CodexHome:              home,
		SQLiteHome:             home,
		DefaultModelProviderID: "test-provider",
		Originator:             "codexgo-test",
		CliVersion:             "test",
	}, runtime)
	return store, runtime
}

// seedThreadRow writes a placeholder rollout file and upserts a state-DB row so
// the store's state-DB list/read path has something to return.
func seedThreadRow(t *testing.T, store *LocalThreadStore, runtime *state.StateRuntime, n int, preview string, createdAt time.Time) protocol.ThreadID {
	t.Helper()
	ctx := context.Background()
	threadID := protocol.NewThreadID(uuidFor(n))
	rolloutPath := filepath.Join(store.config.CodexHome, "rollout-"+uuidFor(n)+".jsonl")

	builder := state.NewThreadMetadataBuilder(threadID, rolloutPath, createdAt, rollout.NewCliSource())
	builder.Cwd = store.config.CodexHome
	provider := "test-provider"
	builder.ModelProvider = &provider
	cli := "test"
	builder.CliVersion = &cli
	metadata := builder.Build("test-provider")
	metadata.Preview = &preview
	metadata.FirstUserMessage = &preview

	if err := runtime.UpsertThread(ctx, &metadata); err != nil {
		t.Fatalf("upsert thread %d: %v", n, err)
	}
	return threadID
}

// TestLocalListThreadsStateDBOrdering verifies the state-DB path returns rows in
// the requested created_at ordering.
func TestLocalListThreadsStateDBOrdering(t *testing.T) {
	store, runtime := newStateDBStore(t)
	ctx := context.Background()

	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	idA := seedThreadRow(t, store, runtime, 41, "alpha", base)
	idB := seedThreadRow(t, store, runtime, 42, "beta", base.Add(time.Hour))
	idC := seedThreadRow(t, store, runtime, 43, "gamma", base.Add(2*time.Hour))

	tests := []struct {
		name      string
		direction SortDirection
		wantFirst protocol.ThreadID
	}{
		{name: "descending newest first", direction: SortDirectionDesc, wantFirst: idC},
		{name: "ascending oldest first", direction: SortDirectionAsc, wantFirst: idA},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := store.ListThreads(ctx, ListThreadsParams{
				PageSize:      10,
				SortKey:       ThreadSortKeyCreatedAt,
				SortDirection: tc.direction,
			})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(page.Items) != 3 {
				t.Fatalf("items = %d, want 3", len(page.Items))
			}
			if page.Items[0].ThreadID != tc.wantFirst {
				t.Fatalf("first item = %s, want %s", page.Items[0].ThreadID, tc.wantFirst)
			}
		})
	}
	_ = idB
}

// TestLocalUpdateThreadMetadataStateDB verifies the patch is applied to the
// state-DB row and reflected in the returned summary.
func TestLocalUpdateThreadMetadataStateDB(t *testing.T) {
	store, runtime := newStateDBStore(t)
	ctx := context.Background()

	createdAt := time.Date(2025, 2, 2, 9, 0, 0, 0, time.UTC)
	threadID := seedThreadRow(t, store, runtime, 51, "first message", createdAt)

	updated, err := store.UpdateThreadMetadata(ctx, UpdateThreadMetadataParams{
		ThreadID: threadID,
		Patch: ThreadMetadataPatch{
			Name:    SetClearable("A custom title"),
			Preview: ptr("updated preview"),
			GitInfo: &GitInfoPatch{Branch: SetClearable("feature")},
		},
	})
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if updated.Name == nil || *updated.Name != "A custom title" {
		t.Fatalf("name = %v, want A custom title", updated.Name)
	}
	if updated.Preview != "updated preview" {
		t.Fatalf("preview = %q, want updated preview", updated.Preview)
	}
	if updated.GitInfo == nil || updated.GitInfo.Branch == nil || *updated.GitInfo.Branch != "feature" {
		t.Fatalf("git branch mismatch: %+v", updated.GitInfo)
	}

	// The change must be durable in the state DB.
	stored, err := runtime.GetThread(ctx, threadID)
	if err != nil || stored == nil {
		t.Fatalf("get thread: %v", err)
	}
	if stored.Title != "A custom title" {
		t.Fatalf("stored title = %q", stored.Title)
	}
	if stored.GitBranch == nil || *stored.GitBranch != "feature" {
		t.Fatalf("stored git branch mismatch: %+v", stored.GitBranch)
	}
}

// TestLocalUpdateThreadMetadataMissingThread verifies an unknown thread yields an
// invalid-request error even when a state DB is present.
func TestLocalUpdateThreadMetadataMissingThread(t *testing.T) {
	store, _ := newStateDBStore(t)
	_, err := store.UpdateThreadMetadata(context.Background(), UpdateThreadMetadataParams{
		ThreadID: validTID(),
		Patch:    ThreadMetadataPatch{Preview: ptr("phantom")},
	})
	var storeErr *Error
	if !errors.As(err, &storeErr) || storeErr.Kind != ErrorKindInvalidRequest {
		t.Fatalf("expected InvalidRequest, got %v", err)
	}
}
