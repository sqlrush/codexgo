package local

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/internal/state"
	"github.com/sqlrush/codexgo/internal/threadstore"
	"github.com/sqlrush/codexgo/internal/threadstore/contracttest"
)

// TestLocalStoreContract runs the storage-neutral contract suite against the
// local file + state-DB store. Delete is staged (Unsupported) for now.
func TestLocalStoreContract(t *testing.T) {
	contracttest.Run(t, contracttest.Config{
		NewStore: func(t *testing.T) threadstore.ThreadStore {
			home := t.TempDir()
			rt, err := state.InitRuntime(context.Background(), home, "test")
			if err != nil {
				t.Fatalf("init state runtime: %v", err)
			}
			t.Cleanup(func() { _ = rt.Close() })
			return NewLocalThreadStore(LocalThreadStoreConfig{CodexHome: home}, rt)
		},
		SupportsDelete:   false,
		TracksArchivedAt: true,
	})
}
