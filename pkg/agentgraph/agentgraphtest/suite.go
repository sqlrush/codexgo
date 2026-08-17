// Package agentgraphtest is the behavioural conformance suite for
// [agentgraph.AgentGraphStore] implementations. The in-memory store, the local
// SQLite store and any external backend (e.g. a PostgreSQL store built on top of
// the airush core) run the same suite so their observable semantics — upsert
// replaces parent and status, status filters, breadth-first descendant order —
// stay identical.
package agentgraphtest

import (
	"context"
	"fmt"
	"testing"

	"github.com/sqlrush/codexgo/internal/agentgraph"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// Factory returns a fresh, empty store and a cleanup function.
type Factory func(t *testing.T) (agentgraph.AgentGraphStore, func())

// RunSuite runs every behavioural test against stores produced by newStore.
func RunSuite(t *testing.T, newStore Factory) {
	t.Helper()
	t.Run("UpsertAndListDirectChildrenWithStatusFilters", func(t *testing.T) {
		testUpsertAndListDirectChildren(t, newStore)
	})
	t.Run("UpsertReplacesParentAndStatus", func(t *testing.T) {
		testUpsertReplacesParentAndStatus(t, newStore)
	})
	t.Run("SetEdgeStatus", func(t *testing.T) {
		testSetEdgeStatus(t, newStore)
	})
	t.Run("SetEdgeStatusMissingChildIsNoOp", func(t *testing.T) {
		testSetEdgeStatusMissingChildIsNoOp(t, newStore)
	})
	t.Run("ListDescendantsBreadthFirstWithStatusFilters", func(t *testing.T) {
		testListDescendantsBreadthFirst(t, newStore)
	})
}

// ThreadID builds a deterministic, syntactically valid thread id from a suffix.
func ThreadID(suffix uint64) protocol.ThreadID {
	return protocol.NewThreadID(fmt.Sprintf("00000000-0000-0000-0000-%012d", suffix))
}

func statusPtr(s agentgraph.ThreadSpawnEdgeStatus) *agentgraph.ThreadSpawnEdgeStatus { return &s }

func ids(suffixes ...uint64) []protocol.ThreadID {
	out := make([]protocol.ThreadID, 0, len(suffixes))
	for _, s := range suffixes {
		out = append(out, ThreadID(s))
	}
	return out
}

func assertThreadIDs(t *testing.T, got, want []protocol.ThreadID) {
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

func testUpsertAndListDirectChildren(t *testing.T, newStore Factory) {
	ctx := context.Background()
	store, cleanup := newStore(t)
	defer cleanup()

	parent := ThreadID(1)
	firstChild := ThreadID(2)
	secondChild := ThreadID(3)

	if err := store.UpsertThreadSpawnEdge(ctx, parent, secondChild, agentgraph.ThreadSpawnEdgeStatusClosed); err != nil {
		t.Fatalf("upsert closed child: %v", err)
	}
	if err := store.UpsertThreadSpawnEdge(ctx, parent, firstChild, agentgraph.ThreadSpawnEdgeStatusOpen); err != nil {
		t.Fatalf("upsert open child: %v", err)
	}

	all, err := store.ListThreadSpawnChildren(ctx, parent, nil)
	if err != nil {
		t.Fatalf("list all children: %v", err)
	}
	assertThreadIDs(t, all, ids(2, 3))

	open, err := store.ListThreadSpawnChildren(ctx, parent, statusPtr(agentgraph.ThreadSpawnEdgeStatusOpen))
	if err != nil {
		t.Fatalf("list open children: %v", err)
	}
	assertThreadIDs(t, open, ids(2))

	closed, err := store.ListThreadSpawnChildren(ctx, parent, statusPtr(agentgraph.ThreadSpawnEdgeStatusClosed))
	if err != nil {
		t.Fatalf("list closed children: %v", err)
	}
	assertThreadIDs(t, closed, ids(3))
}

func testUpsertReplacesParentAndStatus(t *testing.T, newStore Factory) {
	ctx := context.Background()
	store, cleanup := newStore(t)
	defer cleanup()

	oldParent := ThreadID(1)
	newParent := ThreadID(2)
	child := ThreadID(3)

	if err := store.UpsertThreadSpawnEdge(ctx, oldParent, child, agentgraph.ThreadSpawnEdgeStatusOpen); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := store.UpsertThreadSpawnEdge(ctx, newParent, child, agentgraph.ThreadSpawnEdgeStatusClosed); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	oldChildren, err := store.ListThreadSpawnChildren(ctx, oldParent, nil)
	if err != nil {
		t.Fatalf("list old parent children: %v", err)
	}
	assertThreadIDs(t, oldChildren, nil)

	newChildren, err := store.ListThreadSpawnChildren(ctx, newParent, statusPtr(agentgraph.ThreadSpawnEdgeStatusClosed))
	if err != nil {
		t.Fatalf("list new parent children: %v", err)
	}
	assertThreadIDs(t, newChildren, ids(3))
}

func testSetEdgeStatus(t *testing.T, newStore Factory) {
	ctx := context.Background()
	store, cleanup := newStore(t)
	defer cleanup()

	parent := ThreadID(10)
	child := ThreadID(11)

	if err := store.UpsertThreadSpawnEdge(ctx, parent, child, agentgraph.ThreadSpawnEdgeStatusOpen); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.SetThreadSpawnEdgeStatus(ctx, child, agentgraph.ThreadSpawnEdgeStatusClosed); err != nil {
		t.Fatalf("set status: %v", err)
	}

	open, err := store.ListThreadSpawnChildren(ctx, parent, statusPtr(agentgraph.ThreadSpawnEdgeStatusOpen))
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	assertThreadIDs(t, open, nil)

	closed, err := store.ListThreadSpawnChildren(ctx, parent, statusPtr(agentgraph.ThreadSpawnEdgeStatusClosed))
	if err != nil {
		t.Fatalf("list closed: %v", err)
	}
	assertThreadIDs(t, closed, ids(11))
}

func testSetEdgeStatusMissingChildIsNoOp(t *testing.T, newStore Factory) {
	ctx := context.Background()
	store, cleanup := newStore(t)
	defer cleanup()

	if err := store.SetThreadSpawnEdgeStatus(ctx, ThreadID(99), agentgraph.ThreadSpawnEdgeStatusClosed); err != nil {
		t.Fatalf("set status on missing child should be a no-op, got %v", err)
	}
}

func testListDescendantsBreadthFirst(t *testing.T, newStore Factory) {
	ctx := context.Background()
	store, cleanup := newStore(t)
	defer cleanup()

	root := ThreadID(20)
	earlierChild := ThreadID(21)
	laterChild := ThreadID(22)
	closedGrandchild := ThreadID(23)
	openGrandchild := ThreadID(24)
	closedChild := ThreadID(25)
	closedGreatGrandchild := ThreadID(26)

	edges := []struct {
		parent, child protocol.ThreadID
		status        agentgraph.ThreadSpawnEdgeStatus
	}{
		{root, laterChild, agentgraph.ThreadSpawnEdgeStatusOpen},
		{root, earlierChild, agentgraph.ThreadSpawnEdgeStatusOpen},
		{earlierChild, openGrandchild, agentgraph.ThreadSpawnEdgeStatusOpen},
		{laterChild, closedGrandchild, agentgraph.ThreadSpawnEdgeStatusClosed},
		{root, closedChild, agentgraph.ThreadSpawnEdgeStatusClosed},
		{closedChild, closedGreatGrandchild, agentgraph.ThreadSpawnEdgeStatusClosed},
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

	open, err := store.ListThreadSpawnDescendants(ctx, root, statusPtr(agentgraph.ThreadSpawnEdgeStatusOpen))
	if err != nil {
		t.Fatalf("list open descendants: %v", err)
	}
	assertThreadIDs(t, open, ids(21, 22, 24))

	closed, err := store.ListThreadSpawnDescendants(ctx, root, statusPtr(agentgraph.ThreadSpawnEdgeStatusClosed))
	if err != nil {
		t.Fatalf("list closed descendants: %v", err)
	}
	assertThreadIDs(t, closed, ids(25, 26))
}
