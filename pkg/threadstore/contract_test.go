package threadstore_test

import (
	"testing"

	"github.com/sqlrush/codexgo/pkg/threadstore"
	"github.com/sqlrush/codexgo/pkg/threadstore/contracttest"
)

// TestInMemoryStoreContract runs the storage-neutral contract suite against
// the in-memory reference store.
func TestInMemoryStoreContract(t *testing.T) {
	contracttest.Run(t, contracttest.Config{
		NewStore:               func(*testing.T) threadstore.ThreadStore { return threadstore.NewInMemoryThreadStore() },
		SupportsDelete:         true,
		PersistsParentThreadID: true,
		TracksRecency:          true,
	})
}
