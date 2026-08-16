package local

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/state"
	"github.com/sqlrush/codexgo/internal/threadstore"
)

// writeStateDBSessionFile writes a real rollout session file (a session_meta
// head line plus a user message) into the store's sessions tree so the rollout
// resolver can find it by thread id, returning the file path.
func writeStateDBSessionFile(t *testing.T, store *LocalThreadStore, threadID protocol.ThreadID) string {
	t.Helper()
	uuid := threadID.String()
	dayDir := filepath.Join(store.config.CodexHome, "sessions", "2025", "01", "03")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dayDir, "rollout-2025-01-03T12-00-00-"+uuid+".jsonl")
	lines := []string{
		`{"timestamp":"2025-01-03T12:00:00Z","type":"session_meta","payload":{"id":"` + uuid + `","timestamp":"2025-01-03T12:00:00Z","cwd":".","originator":"o","cli_version":"v","source":"cli","model_provider":"p"}}`,
		`{"timestamp":"2025-01-03T12:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	return path
}

// lastRolloutItem reads the final JSONL line of a rollout file and returns it as
// a generic map, mirroring the Rust test helper of the same name.
func lastRolloutItem(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rollout: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("rollout file is empty")
	}
	var item map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &item); err != nil {
		t.Fatalf("decode last rollout line %q: %v", lines[len(lines)-1], err)
	}
	return item
}

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
	rolloutPath := writeStateDBSessionFile(t, store, threadID)

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
		direction threadstore.SortDirection
		wantFirst protocol.ThreadID
	}{
		{name: "descending newest first", direction: threadstore.SortDirectionDesc, wantFirst: idC},
		{name: "ascending oldest first", direction: threadstore.SortDirectionAsc, wantFirst: idA},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := store.ListThreads(ctx, threadstore.ListThreadsParams{
				PageSize:      10,
				SortKey:       threadstore.ThreadSortKeyCreatedAt,
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

	updated, err := store.UpdateThreadMetadata(ctx, threadstore.UpdateThreadMetadataParams{
		ThreadID: threadID,
		Patch: threadstore.ThreadMetadataPatch{
			Name:    threadstore.SetClearable("A custom title"),
			Preview: ptr("updated preview"),
			GitInfo: &threadstore.GitInfoPatch{Branch: threadstore.SetClearable("feature")},
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

// TestUpdateThreadMetadataAppendsMemoryModeSessionMeta asserts a memory-mode
// patch appends a session_meta line carrying the new memory mode (and no git
// marker), mirroring the Rust update_thread_metadata_sets_memory_mode_on_active_rollout.
func TestUpdateThreadMetadataAppendsMemoryModeSessionMeta(t *testing.T) {
	store, runtime := newStateDBStore(t)
	ctx := context.Background()

	threadID := seedThreadRow(t, store, runtime, 61, "first", time.Date(2025, 2, 2, 9, 0, 0, 0, time.UTC))
	path := store.readSQLiteMetadata(ctx, threadID).RolloutPath

	mode := protocol.ThreadMemoryModeDisabled
	if _, err := store.UpdateThreadMetadata(ctx, threadstore.UpdateThreadMetadataParams{
		ThreadID: threadID,
		Patch:    threadstore.ThreadMetadataPatch{MemoryMode: &mode},
	}); err != nil {
		t.Fatalf("update memory mode: %v", err)
	}

	appended := lastRolloutItem(t, path)
	if appended["type"] != "session_meta" {
		t.Fatalf("appended type = %v, want session_meta", appended["type"])
	}
	payload, ok := appended["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload not an object: %T", appended["payload"])
	}
	if payload["id"] != threadID.String() {
		t.Errorf("payload id = %v, want %s", payload["id"], threadID)
	}
	if payload["memory_mode"] != "disabled" {
		t.Errorf("payload memory_mode = %v, want disabled", payload["memory_mode"])
	}
	if _, hasGit := payload["git"]; hasGit {
		t.Errorf("memory-mode-only session meta should omit git, got %v", payload["git"])
	}

	stored, err := runtime.GetThreadMemoryMode(ctx, threadID)
	if err != nil {
		t.Fatalf("get memory mode: %v", err)
	}
	if stored == nil || *stored != "disabled" {
		t.Errorf("stored memory mode = %v, want disabled", stored)
	}
}

// TestUpdateThreadMetadataAppendsGitInfoSessionMeta asserts a git-info patch
// appends a session_meta line carrying the resolved git marker, mirroring the
// Rust update_thread_metadata_sets_git_info rollout write.
func TestUpdateThreadMetadataAppendsGitInfoSessionMeta(t *testing.T) {
	store, runtime := newStateDBStore(t)
	ctx := context.Background()

	threadID := seedThreadRow(t, store, runtime, 62, "first", time.Date(2025, 2, 2, 9, 0, 0, 0, time.UTC))
	path := store.readSQLiteMetadata(ctx, threadID).RolloutPath

	if _, err := store.UpdateThreadMetadata(ctx, threadstore.UpdateThreadMetadataParams{
		ThreadID: threadID,
		Patch: threadstore.ThreadMetadataPatch{GitInfo: &threadstore.GitInfoPatch{
			SHA:       threadstore.SetClearable("abc123"),
			Branch:    threadstore.SetClearable("main"),
			OriginURL: threadstore.SetClearable("https://github.com/openai/codex"),
		}},
	}); err != nil {
		t.Fatalf("update git info: %v", err)
	}

	appended := lastRolloutItem(t, path)
	payload := appended["payload"].(map[string]any)
	git, ok := payload["git"].(map[string]any)
	if !ok {
		t.Fatalf("payload git not an object: %T", payload["git"])
	}
	if git["commit_hash"] != "abc123" {
		t.Errorf("git commit_hash = %v, want abc123", git["commit_hash"])
	}
	if git["branch"] != "main" {
		t.Errorf("git branch = %v, want main", git["branch"])
	}
	if git["repository_url"] != "https://github.com/openai/codex" {
		t.Errorf("git repository_url = %v, want the origin url", git["repository_url"])
	}
	_ = runtime
}

// TestUpdateThreadMetadataCombinedAppendsBothSessionMeta asserts a combined
// memory-mode + git-info patch appends two session_meta lines (memory mode then
// git info), with the last carrying the git marker plus the memory mode, mirroring
// the Rust update_thread_metadata_applies_combined_explicit_patch rollout writes.
func TestUpdateThreadMetadataCombinedAppendsBothSessionMeta(t *testing.T) {
	store, runtime := newStateDBStore(t)
	ctx := context.Background()

	threadID := seedThreadRow(t, store, runtime, 63, "first", time.Date(2025, 2, 2, 9, 0, 0, 0, time.UTC))
	path := store.readSQLiteMetadata(ctx, threadID).RolloutPath

	mode := protocol.ThreadMemoryModeDisabled
	if _, err := store.UpdateThreadMetadata(ctx, threadstore.UpdateThreadMetadataParams{
		ThreadID: threadID,
		Patch: threadstore.ThreadMetadataPatch{
			MemoryMode: &mode,
			GitInfo:    &threadstore.GitInfoPatch{Branch: threadstore.SetClearable("combined")},
		},
	}); err != nil {
		t.Fatalf("combined update: %v", err)
	}

	// The git write runs last, so the final line carries the git marker and the
	// current memory mode (read back from the just-updated SQLite row).
	appended := lastRolloutItem(t, path)
	payload := appended["payload"].(map[string]any)
	if payload["memory_mode"] != "disabled" {
		t.Errorf("payload memory_mode = %v, want disabled", payload["memory_mode"])
	}
	git, ok := payload["git"].(map[string]any)
	if !ok {
		t.Fatalf("payload git not an object: %T", payload["git"])
	}
	if git["branch"] != "combined" {
		t.Errorf("git branch = %v, want combined", git["branch"])
	}
	_ = runtime
}

// TestUpdateThreadMetadataRejectsMismatchedSessionMetaID asserts a session-meta
// id mismatch fails with an internal error, mirroring the Rust
// update_thread_metadata_rejects_mismatched_session_meta_id.
func TestUpdateThreadMetadataRejectsMismatchedSessionMetaID(t *testing.T) {
	store, runtime := newStateDBStore(t)
	ctx := context.Background()

	threadID := seedThreadRow(t, store, runtime, 64, "first", time.Date(2025, 2, 2, 9, 0, 0, 0, time.UTC))
	path := store.readSQLiteMetadata(ctx, threadID).RolloutPath

	// Rewrite the session-meta id to a different (but valid) thread id.
	otherID := uuidFor(640)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rollout: %v", err)
	}
	rewritten := strings.Replace(string(content), threadID.String(), otherID, 1)
	if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("rewrite rollout: %v", err)
	}

	mode := protocol.ThreadMemoryModeEnabled
	_, err = store.UpdateThreadMetadata(ctx, threadstore.UpdateThreadMetadataParams{
		ThreadID: threadID,
		Patch:    threadstore.ThreadMetadataPatch{MemoryMode: &mode},
	})
	var storeErr *threadstore.Error
	if !errors.As(err, &storeErr) || storeErr.Kind != threadstore.ErrorKindInternal {
		t.Fatalf("expected internal error, got %v", err)
	}
	if !strings.Contains(err.Error(), "metadata id mismatch") {
		t.Errorf("error = %q, want substring metadata id mismatch", err.Error())
	}
	_ = runtime
}

// TestLocalUpdateThreadMetadataMissingThread verifies an unknown thread yields an
// invalid-request error even when a state DB is present.
func TestLocalUpdateThreadMetadataMissingThread(t *testing.T) {
	store, _ := newStateDBStore(t)
	_, err := store.UpdateThreadMetadata(context.Background(), threadstore.UpdateThreadMetadataParams{
		ThreadID: validTID(),
		Patch:    threadstore.ThreadMetadataPatch{Preview: ptr("phantom")},
	})
	var storeErr *threadstore.Error
	if !errors.As(err, &storeErr) || storeErr.Kind != threadstore.ErrorKindInvalidRequest {
		t.Fatalf("expected InvalidRequest, got %v", err)
	}
}
