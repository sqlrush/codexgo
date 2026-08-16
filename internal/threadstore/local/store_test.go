package local

import (
	"context"
	"errors"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/threadstore"
)

func tid(s string) protocol.ThreadID { return protocol.NewThreadID(s) }

func TestLocalStoreImplementsThreadStore(t *testing.T) {
	var _ threadstore.ThreadStore = NewLocalThreadStore(LocalThreadStoreConfig{}, nil)
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
			_, err := store.LoadHistory(ctx, threadstore.LoadThreadHistoryParams{ThreadID: validTID()})
			return err
		}},
		{"search requires term", func() error {
			_, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{})
			return err
		}},
		{"update_metadata requires state db", func() error {
			_, err := store.UpdateThreadMetadata(ctx, threadstore.UpdateThreadMetadataParams{
				ThreadID: validTID(),
				Patch:    threadstore.ThreadMetadataPatch{Preview: ptr("x")},
			})
			return err
		}},
		{"archive missing", func() error {
			return store.ArchiveThread(ctx, threadstore.ArchiveThreadParams{ThreadID: validTID()})
		}},
		{"unarchive missing", func() error {
			_, err := store.UnarchiveThread(ctx, threadstore.ArchiveThreadParams{ThreadID: validTID()})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var storeErr *threadstore.Error
			if err := tc.call(); !errors.As(err, &storeErr) || storeErr.Kind != threadstore.ErrorKindInvalidRequest {
				t.Fatalf("expected InvalidRequest, got %v", err)
			}
		})
	}
}

// TestLocalListThreadsEmptyOK verifies that listing with no sessions on disk and
// no state DB returns an empty page rather than an error.
func TestLocalListThreadsEmptyOK(t *testing.T) {
	store := NewLocalThreadStore(LocalThreadStoreConfig{CodexHome: t.TempDir()}, nil)
	page, err := store.ListThreads(context.Background(), threadstore.ListThreadsParams{PageSize: 10})
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
	err := store.CreateThread(context.Background(), threadstore.CreateThreadParams{
		ThreadID: tid("x"),
		Source:   rollout.DefaultSessionSource(),
		Metadata: threadstore.ThreadPersistenceMetadata{}, // Cwd is nil
	})
	var storeErr *threadstore.Error
	if !errors.As(err, &storeErr) || storeErr.Kind != threadstore.ErrorKindInvalidRequest {
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
