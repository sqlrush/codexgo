package threadstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

func tid(s string) protocol.ThreadID { return protocol.NewThreadID(s) }

// TestErrorRendering verifies the Error message wording matches the Rust
// `thiserror` attributes byte-for-byte.
func TestErrorRendering(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "thread not found",
			err:  threadNotFoundError(tid("abc")),
			want: "thread abc not found",
		},
		{
			name: "invalid request",
			err:  invalidRequestError("bad %s", "input"),
			want: "invalid thread-store request: bad input",
		},
		{
			name: "conflict",
			err:  &Error{Kind: ErrorKindConflict, Message: "stale"},
			want: "thread-store conflict: stale",
		},
		{
			name: "unsupported",
			err:  unsupportedError("thread/search"),
			want: "thread-store unsupported operation: thread/search",
		},
		{
			name: "internal",
			err:  internalError(errors.New("io"), "boom %d", 1),
			want: "thread-store internal error: boom 1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestInternalErrorUnwrap verifies the wrapped cause is exposed for errors.Is.
func TestInternalErrorUnwrap(t *testing.T) {
	cause := errors.New("disk full")
	err := internalError(cause, "failed")
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is should find the wrapped cause")
	}
}

// TestClearableFieldStates verifies the three states of a ClearableField and
// their IsSome semantics, mirroring the Rust ClearableField (Option<Option<T>>).
func TestClearableFieldStates(t *testing.T) {
	var absent ClearableField[string]
	if absent.IsSome() {
		t.Fatalf("absent field should not be Some")
	}

	cleared := ClearField[string]()
	if !cleared.IsSome() || cleared.Value != nil {
		t.Fatalf("cleared field should be Some(None)")
	}

	set := SetClearable("hi")
	if !set.IsSome() || set.Value == nil || *set.Value != "hi" {
		t.Fatalf("set field should be Some(Some(hi))")
	}
}

// TestGitInfoPatchOptionalOptionJSON verifies the serde optional_option wire
// semantics for a clearable patch: absent fields are omitted, cleared fields
// serialize as null, set fields as the value.
func TestGitInfoPatchOptionalOptionJSON(t *testing.T) {
	tests := []struct {
		name  string
		patch GitInfoPatch
		want  string
	}{
		{
			name:  "all absent omits everything",
			patch: GitInfoPatch{},
			want:  `{}`,
		},
		{
			name:  "set sha",
			patch: GitInfoPatch{SHA: SetClearable("deadbeef")},
			want:  `{"sha":"deadbeef"}`,
		},
		{
			name:  "clear branch",
			patch: GitInfoPatch{Branch: ClearField[string]()},
			want:  `{"branch":null}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.patch)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("json = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestGitInfoPatchRoundTrip verifies that missing keys decode as absent and
// explicit null decodes as a clear request.
func TestGitInfoPatchRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSHA    ClearableField[string]
		wantBranch ClearableField[string]
		wantOrigin ClearableField[string]
	}{
		{
			name:  "empty object all absent",
			input: `{}`,
		},
		{
			name:    "null clears",
			input:   `{"sha":null}`,
			wantSHA: ClearField[string](),
		},
		{
			name:       "value sets",
			input:      `{"branch":"main"}`,
			wantBranch: SetClearable("main"),
		},
		{
			name:       "origin set",
			input:      `{"origin_url":"https://example.com"}`,
			wantOrigin: SetClearable("https://example.com"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var patch GitInfoPatch
			if err := json.Unmarshal([]byte(tc.input), &patch); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			assertClearable(t, "sha", patch.SHA, tc.wantSHA)
			assertClearable(t, "branch", patch.Branch, tc.wantBranch)
			assertClearable(t, "origin_url", patch.OriginURL, tc.wantOrigin)
		})
	}
}

func assertClearable(t *testing.T, name string, got, want ClearableField[string]) {
	t.Helper()
	if got.Present != want.Present {
		t.Fatalf("%s present = %v, want %v", name, got.Present, want.Present)
	}
	if (got.Value == nil) != (want.Value == nil) {
		t.Fatalf("%s value nil-ness mismatch: got %v want %v", name, got.Value, want.Value)
	}
	if got.Value != nil && want.Value != nil && *got.Value != *want.Value {
		t.Fatalf("%s value = %q, want %q", name, *got.Value, *want.Value)
	}
}

// TestGitInfoPatchMerge verifies field-presence merge semantics including clears.
func TestGitInfoPatchMerge(t *testing.T) {
	base := GitInfoPatch{SHA: SetClearable("old"), Branch: SetClearable("main")}
	base.Merge(GitInfoPatch{SHA: SetClearable("new"), OriginURL: ClearField[string]()})

	if base.SHA.Value == nil || *base.SHA.Value != "new" {
		t.Fatalf("sha should be overwritten to new")
	}
	if base.Branch.Value == nil || *base.Branch.Value != "main" {
		t.Fatalf("branch should be unchanged")
	}
	if !base.OriginURL.IsSome() || base.OriginURL.Value != nil {
		t.Fatalf("origin should be a clear request")
	}
}

// TestThreadMetadataPatchIsEmpty checks the IsEmpty predicate.
func TestThreadMetadataPatchIsEmpty(t *testing.T) {
	if !(ThreadMetadataPatch{}).IsEmpty() {
		t.Fatalf("zero patch should be empty")
	}
	name := "n"
	if (ThreadMetadataPatch{Preview: &name}).IsEmpty() {
		t.Fatalf("patch with preview should not be empty")
	}
	if (ThreadMetadataPatch{Name: SetClearable("x")}).IsEmpty() {
		t.Fatalf("patch with clearable name should not be empty")
	}
}

// TestThreadMetadataPatchMerge verifies that present plain fields overwrite,
// absent fields are preserved, and nested git patches merge recursively.
func TestThreadMetadataPatchMerge(t *testing.T) {
	preview := "old preview"
	base := ThreadMetadataPatch{
		Preview: &preview,
		GitInfo: &GitInfoPatch{SHA: SetClearable("old")},
	}
	newProvider := "openai"
	base.Merge(ThreadMetadataPatch{
		ModelProvider: &newProvider,
		GitInfo:       &GitInfoPatch{Branch: SetClearable("dev")},
	})

	if base.Preview == nil || *base.Preview != "old preview" {
		t.Fatalf("preview should be preserved")
	}
	if base.ModelProvider == nil || *base.ModelProvider != "openai" {
		t.Fatalf("model provider should be set")
	}
	if base.GitInfo == nil || base.GitInfo.SHA.Value == nil || *base.GitInfo.SHA.Value != "old" {
		t.Fatalf("nested git sha should be preserved")
	}
	if base.GitInfo.Branch.Value == nil || *base.GitInfo.Branch.Value != "dev" {
		t.Fatalf("nested git branch should be merged in")
	}
}

// TestThreadMetadataPatchJSONClearableVsPlain verifies the distinction between
// clearable fields (omitted when absent) and plain Option fields (emitted as
// null when nil) on the wire.
func TestThreadMetadataPatchJSONClearable(t *testing.T) {
	// A clearable field that is absent must be omitted; one that is set appears.
	patch := ThreadMetadataPatch{AgentRole: SetClearable("planner")}
	got, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["agent_role"]; !ok {
		t.Fatalf("agent_role should be present, got %s", got)
	}
	if string(decoded["agent_role"]) != `"planner"` {
		t.Fatalf("agent_role = %s, want \"planner\"", decoded["agent_role"])
	}
	// Absent clearable fields must not appear.
	if _, ok := decoded["agent_nickname"]; ok {
		t.Fatalf("absent agent_nickname should be omitted, got %s", got)
	}
}

// TestThreadMetadataPatchJSONRoundTrip exercises a fuller decode path.
func TestThreadMetadataPatchJSONRoundTrip(t *testing.T) {
	input := `{
		"name": "my thread",
		"agent_role": null,
		"model_provider": "openai",
		"git_info": {"sha": "abc123"}
	}`
	var patch ThreadMetadataPatch
	if err := json.Unmarshal([]byte(input), &patch); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if patch.Name.Value == nil || *patch.Name.Value != "my thread" {
		t.Fatalf("name should decode to set value")
	}
	if !patch.AgentRole.IsSome() || patch.AgentRole.Value != nil {
		t.Fatalf("agent_role null should decode to clear request")
	}
	if patch.ModelProvider == nil || *patch.ModelProvider != "openai" {
		t.Fatalf("model_provider should decode")
	}
	if patch.GitInfo == nil || patch.GitInfo.SHA.Value == nil || *patch.GitInfo.SHA.Value != "abc123" {
		t.Fatalf("nested git info should decode")
	}
}

// TestGitInfoJSONSkipsNil verifies GitInfo omits nil fields, matching serde
// skip_serializing_if = "Option::is_none".
func TestGitInfoJSONSkipsNil(t *testing.T) {
	sha := GitSha("deadbeef")
	branch := "main"
	info := GitInfo{CommitHash: &sha, Branch: &branch}
	got, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"commit_hash":"deadbeef","branch":"main"}`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}

// TestGitInfoFromPartsAndPatch verifies the helper constructors.
func TestGitInfoFromPartsAndPatch(t *testing.T) {
	if gitInfoFromParts(nil, nil, nil) != nil {
		t.Fatalf("all-nil parts should produce nil GitInfo")
	}
	sha := "abc"
	info := gitInfoFromParts(&sha, nil, nil)
	if info == nil || info.CommitHash == nil || info.CommitHash.String() != "abc" {
		t.Fatalf("git info from parts mismatch: %+v", info)
	}

	if gitInfoFromPatch(nil) != nil {
		t.Fatalf("nil patch should produce nil GitInfo")
	}
	if gitInfoFromPatch(&GitInfoPatch{}) != nil {
		t.Fatalf("empty patch should produce nil GitInfo")
	}
	fromPatch := gitInfoFromPatch(&GitInfoPatch{Branch: SetClearable("dev")})
	if fromPatch == nil || fromPatch.Branch == nil || *fromPatch.Branch != "dev" {
		t.Fatalf("git info from patch mismatch: %+v", fromPatch)
	}
}

// TestThreadEventPersistenceModeMapping verifies the rollout-mode conversion.
func TestThreadEventPersistenceModeMapping(t *testing.T) {
	if ThreadEventPersistenceModeLimited.ToRolloutMode() != rollout.EventPersistenceModeLimited {
		t.Fatalf("limited mapping mismatch")
	}
	if ThreadEventPersistenceModeExtended.ToRolloutMode() != rollout.EventPersistenceModeExtended {
		t.Fatalf("extended mapping mismatch")
	}
}

func TestInMemoryStoreImplementsThreadStore(t *testing.T) {
	var _ ThreadStore = NewInMemoryThreadStore()
	var _ ThreadStore = NewLocalThreadStore(LocalThreadStoreConfig{}, nil)
}

// TestInMemoryCreateReadAppendLoad exercises the core in-memory lifecycle and
// the call counters.
func TestInMemoryCreateReadAppendLoad(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryThreadStore()
	thread := tid("thread-1")

	if err := store.CreateThread(ctx, CreateThreadParams{
		ThreadID: thread,
		Source:   rollout.DefaultSessionSource(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	read, err := store.ReadThread(ctx, ReadThreadParams{ThreadID: thread})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read.ThreadID.String() != "thread-1" {
		t.Fatalf("read thread id = %s", read.ThreadID)
	}
	if read.ModelProvider != "test" || read.CliVersion != "test" {
		t.Fatalf("defaults mismatch: provider=%q cli=%q", read.ModelProvider, read.CliVersion)
	}

	items := []rollout.RolloutItem{
		rollout.NewTurnContextItem(rollout.TurnContextItem{}),
		rollout.NewTurnContextItem(rollout.TurnContextItem{}),
	}
	if err := store.AppendItems(ctx, AppendThreadItemsParams{ThreadID: thread, Items: items}); err != nil {
		t.Fatalf("append: %v", err)
	}

	hist, err := store.LoadHistory(ctx, LoadThreadHistoryParams{ThreadID: thread})
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(hist.Items) != 2 {
		t.Fatalf("history items = %d, want 2", len(hist.Items))
	}

	calls := store.Calls()
	if calls.CreateThread != 1 || calls.ReadThread != 1 || calls.AppendItems != 1 || calls.LoadHistory != 1 {
		t.Fatalf("call counts mismatch: %+v", calls)
	}
}

// TestInMemoryReadThreadNotFound verifies the not-found error path.
func TestInMemoryReadThreadNotFound(t *testing.T) {
	store := NewInMemoryThreadStore()
	_, err := store.ReadThread(context.Background(), ReadThreadParams{ThreadID: tid("missing")})
	var storeErr *Error
	if !errors.As(err, &storeErr) || storeErr.Kind != ErrorKindThreadNotFound {
		t.Fatalf("expected ThreadNotFound, got %v", err)
	}
}

// TestInMemoryForkSetsForkedFromID verifies that a forked thread carries the
// source thread id through to the stored thread, mirroring the Rust fork flow.
func TestInMemoryForkSetsForkedFromID(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryThreadStore()
	parent := tid("parent")
	child := tid("child")

	if err := store.CreateThread(ctx, CreateThreadParams{ThreadID: parent, Source: rollout.DefaultSessionSource()}); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if err := store.CreateThread(ctx, CreateThreadParams{
		ThreadID:     child,
		ForkedFromID: &parent,
		Source:       rollout.DefaultSessionSource(),
	}); err != nil {
		t.Fatalf("create child fork: %v", err)
	}

	read, err := store.ReadThread(ctx, ReadThreadParams{ThreadID: child})
	if err != nil {
		t.Fatalf("read child: %v", err)
	}
	if read.ForkedFromID == nil || read.ForkedFromID.String() != "parent" {
		t.Fatalf("forked_from_id = %v, want parent", read.ForkedFromID)
	}
}

// TestInMemoryUpdateThreadMetadata verifies that a metadata patch is applied and
// reflected in subsequent reads.
func TestInMemoryUpdateThreadMetadata(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryThreadStore()
	thread := tid("t")
	if err := store.CreateThread(ctx, CreateThreadParams{ThreadID: thread, Source: rollout.DefaultSessionSource()}); err != nil {
		t.Fatalf("create: %v", err)
	}

	provider := "openai"
	name := "my title"
	created := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	updated, err := store.UpdateThreadMetadata(ctx, UpdateThreadMetadataParams{
		ThreadID: thread,
		Patch: ThreadMetadataPatch{
			Name:          SetClearable(name),
			ModelProvider: &provider,
			CreatedAt:     &created,
		},
	})
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if updated.ModelProvider != "openai" {
		t.Fatalf("provider = %q, want openai", updated.ModelProvider)
	}
	if updated.Name == nil || *updated.Name != name {
		t.Fatalf("name = %v, want %q", updated.Name, name)
	}
	if !updated.CreatedAt.Equal(created) {
		t.Fatalf("created_at = %v, want %v", updated.CreatedAt, created)
	}

	if store.Calls().UpdateThreadMetadata != 1 {
		t.Fatalf("update call count = %d, want 1", store.Calls().UpdateThreadMetadata)
	}
}

// TestInMemoryListThreadsSortedByID verifies stable ordering of list results.
func TestInMemoryListThreadsSortedByID(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryThreadStore()
	for _, id := range []string{"c", "a", "b"} {
		if err := store.CreateThread(ctx, CreateThreadParams{ThreadID: tid(id), Source: rollout.DefaultSessionSource()}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	page, err := store.ListThreads(ctx, ListThreadsParams{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(page.Items))
	}
	want := []string{"a", "b", "c"}
	for i, item := range page.Items {
		if item.ThreadID.String() != want[i] {
			t.Fatalf("item[%d] = %s, want %s", i, item.ThreadID, want[i])
		}
	}
}

// TestInMemorySearchUnsupported verifies SearchThreads returns Unsupported.
func TestInMemorySearchUnsupported(t *testing.T) {
	store := NewInMemoryThreadStore()
	_, err := store.SearchThreads(context.Background(), SearchThreadsParams{})
	var storeErr *Error
	if !errors.As(err, &storeErr) || storeErr.Kind != ErrorKindUnsupported || storeErr.Operation != "thread/search" {
		t.Fatalf("expected Unsupported thread/search, got %v", err)
	}
}

// TestInMemoryResumeMapsRolloutPath verifies that resume records the rollout
// path mapping used by ReadThreadByRolloutPath.
func TestInMemoryResumeMapsRolloutPath(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryThreadStore()
	thread := tid("t")
	path := "/tmp/rollout.jsonl"

	if err := store.CreateThread(ctx, CreateThreadParams{ThreadID: thread, Source: rollout.DefaultSessionSource()}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.ResumeThread(ctx, ResumeThreadParams{ThreadID: thread, RolloutPath: &path}); err != nil {
		t.Fatalf("resume: %v", err)
	}

	read, err := store.ReadThreadByRolloutPath(ctx, ReadThreadByRolloutPathParams{RolloutPath: path})
	if err != nil {
		t.Fatalf("read by path: %v", err)
	}
	if read.ThreadID.String() != "t" {
		t.Fatalf("resolved thread = %s, want t", read.ThreadID)
	}
}

// TestInMemoryRegistrySharesByID verifies the process-global registry returns a
// shared store for the same id and removal works.
func TestInMemoryRegistrySharesByID(t *testing.T) {
	id := "registry-test-id"
	defer RemoveInMemoryThreadStoreID(id)

	a := InMemoryThreadStoreForID(id)
	b := InMemoryThreadStoreForID(id)
	if a != b {
		t.Fatalf("expected same shared store instance")
	}
	if removed := RemoveInMemoryThreadStoreID(id); removed != a {
		t.Fatalf("removed store should be the shared instance")
	}
	c := InMemoryThreadStoreForID(id)
	if c == a {
		t.Fatalf("expected a fresh store after removal")
	}
}

// TestLocalReadPathInvalidRequests verifies the invalid-request behavior of the
// now-implemented read/list/metadata operations when the target thread or its
// rollout does not exist.
func TestLocalReadPathInvalidRequests(t *testing.T) {
	ctx := context.Background()
	store := NewLocalThreadStore(LocalThreadStoreConfig{CodexHome: t.TempDir()}, nil)

	tests := []struct {
		name string
		call func() error
	}{
		{"load_history missing", func() error {
			_, err := store.LoadHistory(ctx, LoadThreadHistoryParams{ThreadID: validTID()})
			return err
		}},
		{"search requires term", func() error {
			_, err := store.SearchThreads(ctx, SearchThreadsParams{})
			return err
		}},
		{"update_metadata requires state db", func() error {
			_, err := store.UpdateThreadMetadata(ctx, UpdateThreadMetadataParams{
				ThreadID: validTID(),
				Patch:    ThreadMetadataPatch{Preview: ptr("x")},
			})
			return err
		}},
		{"archive missing", func() error {
			return store.ArchiveThread(ctx, ArchiveThreadParams{ThreadID: validTID()})
		}},
		{"unarchive missing", func() error {
			_, err := store.UnarchiveThread(ctx, ArchiveThreadParams{ThreadID: validTID()})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var storeErr *Error
			if err := tc.call(); !errors.As(err, &storeErr) || storeErr.Kind != ErrorKindInvalidRequest {
				t.Fatalf("expected InvalidRequest, got %v", err)
			}
		})
	}
}

// TestLocalListThreadsEmptyOK verifies that listing with no sessions on disk and
// no state DB returns an empty page rather than an error.
func TestLocalListThreadsEmptyOK(t *testing.T) {
	store := NewLocalThreadStore(LocalThreadStoreConfig{CodexHome: t.TempDir()}, nil)
	page, err := store.ListThreads(context.Background(), ListThreadsParams{PageSize: 10})
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(page.Items) != 0 || page.NextCursor != nil {
		t.Fatalf("expected empty page, got %+v", page)
	}
}

// TestLocalCreateThreadRequiresCwd verifies the invalid-request path when no cwd
// is supplied for live persistence.
func TestLocalCreateThreadRequiresCwd(t *testing.T) {
	store := NewLocalThreadStore(LocalThreadStoreConfig{CodexHome: t.TempDir()}, nil)
	err := store.CreateThread(context.Background(), CreateThreadParams{
		ThreadID: tid("x"),
		Source:   rollout.DefaultSessionSource(),
		Metadata: ThreadPersistenceMetadata{}, // Cwd is nil
	})
	var storeErr *Error
	if !errors.As(err, &storeErr) || storeErr.Kind != ErrorKindInvalidRequest {
		t.Fatalf("expected InvalidRequest for missing cwd, got %v", err)
	}
}

// TestLocalLiveRecorderMissingThread verifies operations on an unknown live
// thread report not found.
func TestLocalLiveRecorderMissingThread(t *testing.T) {
	store := NewLocalThreadStore(LocalThreadStoreConfig{CodexHome: t.TempDir()}, nil)
	if err := store.FlushThread(context.Background(), tid("ghost")); err == nil {
		t.Fatalf("expected error flushing unknown thread")
	}
	if err := store.DiscardThread(context.Background(), tid("ghost")); err == nil {
		t.Fatalf("expected error discarding unknown thread")
	}
}
