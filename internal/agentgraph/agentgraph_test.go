package agentgraph

import (
	"context"
	"fmt"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/state"
)

func threadID(suffix uint64) protocol.ThreadID {
	return protocol.NewThreadID(fmt.Sprintf("00000000-0000-0000-0000-%012d", suffix))
}

func statusPtr(s ThreadSpawnEdgeStatus) *ThreadSpawnEdgeStatus { return &s }

func ids(suffixes ...uint64) []protocol.ThreadID {
	out := make([]protocol.ThreadID, 0, len(suffixes))
	for _, s := range suffixes {
		out = append(out, threadID(s))
	}
	return out
}

func assertThreadIDs(t *testing.T, got []protocol.ThreadID, want []protocol.ThreadID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("thread id count mismatch: got %v want %v", got, want)
	}
	for i := range got {
		if got[i].String() != want[i].String() {
			t.Fatalf("thread id mismatch at %d: got %v want %v", i, got, want)
		}
	}
}

func TestThreadSpawnEdgeStatusWireValue(t *testing.T) {
	if ThreadSpawnEdgeStatusOpen.String() != "open" {
		t.Fatalf("open status = %q, want open", ThreadSpawnEdgeStatusOpen)
	}
	if ThreadSpawnEdgeStatusClosed.String() != "closed" {
		t.Fatalf("closed status = %q, want closed", ThreadSpawnEdgeStatusClosed)
	}
}

// stores returns the set of store implementations to run a behavioral test
// against. Each entry returns a fresh store and a cleanup function.
type storeFactory struct {
	name string
	make func(t *testing.T) (AgentGraphStore, func())
}

func storeFactories() []storeFactory {
	return []storeFactory{
		{
			name: "in_memory",
			make: func(t *testing.T) (AgentGraphStore, func()) {
				return NewInMemoryAgentGraphStore(), func() {}
			},
		},
		{
			name: "local_sqlite",
			make: func(t *testing.T) (AgentGraphStore, func()) {
				ctx := context.Background()
				home := t.TempDir()
				rt, err := state.InitRuntime(ctx, home, "test-provider")
				if err != nil {
					t.Fatalf("init state runtime: %v", err)
				}
				store, err := NewLocalAgentGraphStore(ctx, rt)
				if err != nil {
					_ = rt.Close()
					t.Fatalf("new local store: %v", err)
				}
				return store, func() {
					_ = store.Close()
					_ = rt.Close()
				}
			},
		},
	}
}

func TestUpsertAndListDirectChildrenWithStatusFilters(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, cleanup := factory.make(t)
			defer cleanup()

			parent := threadID(1)
			firstChild := threadID(2)
			secondChild := threadID(3)

			if err := store.UpsertThreadSpawnEdge(ctx, parent, secondChild, ThreadSpawnEdgeStatusClosed); err != nil {
				t.Fatalf("upsert closed child: %v", err)
			}
			if err := store.UpsertThreadSpawnEdge(ctx, parent, firstChild, ThreadSpawnEdgeStatusOpen); err != nil {
				t.Fatalf("upsert open child: %v", err)
			}

			all, err := store.ListThreadSpawnChildren(ctx, parent, nil)
			if err != nil {
				t.Fatalf("list all children: %v", err)
			}
			assertThreadIDs(t, all, ids(2, 3))

			open, err := store.ListThreadSpawnChildren(ctx, parent, statusPtr(ThreadSpawnEdgeStatusOpen))
			if err != nil {
				t.Fatalf("list open children: %v", err)
			}
			assertThreadIDs(t, open, ids(2))

			closed, err := store.ListThreadSpawnChildren(ctx, parent, statusPtr(ThreadSpawnEdgeStatusClosed))
			if err != nil {
				t.Fatalf("list closed children: %v", err)
			}
			assertThreadIDs(t, closed, ids(3))
		})
	}
}

func TestUpsertReplacesParentAndStatus(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, cleanup := factory.make(t)
			defer cleanup()

			oldParent := threadID(1)
			newParent := threadID(2)
			child := threadID(3)

			if err := store.UpsertThreadSpawnEdge(ctx, oldParent, child, ThreadSpawnEdgeStatusOpen); err != nil {
				t.Fatalf("first upsert: %v", err)
			}
			if err := store.UpsertThreadSpawnEdge(ctx, newParent, child, ThreadSpawnEdgeStatusClosed); err != nil {
				t.Fatalf("second upsert: %v", err)
			}

			oldChildren, err := store.ListThreadSpawnChildren(ctx, oldParent, nil)
			if err != nil {
				t.Fatalf("list old parent children: %v", err)
			}
			assertThreadIDs(t, oldChildren, nil)

			newChildren, err := store.ListThreadSpawnChildren(ctx, newParent, statusPtr(ThreadSpawnEdgeStatusClosed))
			if err != nil {
				t.Fatalf("list new parent children: %v", err)
			}
			assertThreadIDs(t, newChildren, ids(3))
		})
	}
}

func TestSetEdgeStatus(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, cleanup := factory.make(t)
			defer cleanup()

			parent := threadID(10)
			child := threadID(11)

			if err := store.UpsertThreadSpawnEdge(ctx, parent, child, ThreadSpawnEdgeStatusOpen); err != nil {
				t.Fatalf("upsert: %v", err)
			}
			if err := store.SetThreadSpawnEdgeStatus(ctx, child, ThreadSpawnEdgeStatusClosed); err != nil {
				t.Fatalf("set status: %v", err)
			}

			open, err := store.ListThreadSpawnChildren(ctx, parent, statusPtr(ThreadSpawnEdgeStatusOpen))
			if err != nil {
				t.Fatalf("list open: %v", err)
			}
			assertThreadIDs(t, open, nil)

			closed, err := store.ListThreadSpawnChildren(ctx, parent, statusPtr(ThreadSpawnEdgeStatusClosed))
			if err != nil {
				t.Fatalf("list closed: %v", err)
			}
			assertThreadIDs(t, closed, ids(11))
		})
	}
}

func TestSetEdgeStatusMissingChildIsNoOp(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, cleanup := factory.make(t)
			defer cleanup()

			if err := store.SetThreadSpawnEdgeStatus(ctx, threadID(99), ThreadSpawnEdgeStatusClosed); err != nil {
				t.Fatalf("set status on missing child should be a no-op, got %v", err)
			}
		})
	}
}

func TestListDescendantsBreadthFirstWithStatusFilters(t *testing.T) {
	for _, factory := range storeFactories() {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			store, cleanup := factory.make(t)
			defer cleanup()

			root := threadID(20)
			earlierChild := threadID(21)
			laterChild := threadID(22)
			closedGrandchild := threadID(23)
			openGrandchild := threadID(24)
			closedChild := threadID(25)
			closedGreatGrandchild := threadID(26)

			edges := []struct {
				parent, child protocol.ThreadID
				status        ThreadSpawnEdgeStatus
			}{
				{root, laterChild, ThreadSpawnEdgeStatusOpen},
				{root, earlierChild, ThreadSpawnEdgeStatusOpen},
				{earlierChild, openGrandchild, ThreadSpawnEdgeStatusOpen},
				{laterChild, closedGrandchild, ThreadSpawnEdgeStatusClosed},
				{root, closedChild, ThreadSpawnEdgeStatusClosed},
				{closedChild, closedGreatGrandchild, ThreadSpawnEdgeStatusClosed},
			}
			for _, e := range edges {
				if err := store.UpsertThreadSpawnEdge(ctx, e.parent, e.child, e.status); err != nil {
					t.Fatalf("upsert edge: %v", err)
				}
			}

			all, err := store.ListThreadSpawnDescendants(ctx, root, nil)
			if err != nil {
				t.Fatalf("list all descendants: %v", err)
			}
			assertThreadIDs(t, all, ids(21, 22, 25, 23, 24, 26))

			open, err := store.ListThreadSpawnDescendants(ctx, root, statusPtr(ThreadSpawnEdgeStatusOpen))
			if err != nil {
				t.Fatalf("list open descendants: %v", err)
			}
			assertThreadIDs(t, open, ids(21, 22, 24))

			closed, err := store.ListThreadSpawnDescendants(ctx, root, statusPtr(ThreadSpawnEdgeStatusClosed))
			if err != nil {
				t.Fatalf("list closed descendants: %v", err)
			}
			assertThreadIDs(t, closed, ids(25, 26))
		})
	}
}

func TestNewLocalAgentGraphStoreRejectsNilRuntime(t *testing.T) {
	_, err := NewLocalAgentGraphStore(context.Background(), nil)
	var storeErr *Error
	if err == nil {
		t.Fatalf("expected error for nil runtime")
	}
	if !asError(err, &storeErr) || storeErr.Kind != ErrorKindInvalidRequest {
		t.Fatalf("expected invalid request error, got %v", err)
	}
}

// asError is a tiny errors.As wrapper kept local to avoid an extra import in the
// common test body.
func asError(err error, target **Error) bool {
	for err != nil {
		if e, ok := err.(*Error); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
