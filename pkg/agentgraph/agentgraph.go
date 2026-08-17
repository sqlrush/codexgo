// Package agentgraph stores the storage-neutral parent/child topology for
// thread-spawned agents, a faithful Go port of the Rust `codex-agent-graph-store`
// crate.
//
// The graph records directional spawn edges between a parent thread and the
// child threads it spawned. Each edge carries a lifecycle status
// ([ThreadSpawnEdgeStatus]). Implementations return stable ordering for the list
// methods so callers can merge persisted graph state with live in-memory state
// without introducing nondeterministic output.
package agentgraph

import (
	"context"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ThreadSpawnEdgeStatus is the lifecycle status attached to a directional
// thread-spawn edge.
//
// Mirrors the Rust `ThreadSpawnEdgeStatus` enum, which serializes with
// `rename_all = "snake_case"`. On the wire and in the database it is the bare
// snake_case string ("open" / "closed").
type ThreadSpawnEdgeStatus string

const (
	// ThreadSpawnEdgeStatusOpen marks a child thread that is still live or
	// resumable as an open spawned agent.
	ThreadSpawnEdgeStatusOpen ThreadSpawnEdgeStatus = "open"
	// ThreadSpawnEdgeStatusClosed marks a child thread that has been closed from
	// the parent/child graph's perspective.
	ThreadSpawnEdgeStatusClosed ThreadSpawnEdgeStatus = "closed"
)

// String returns the snake_case wire representation of the status.
func (s ThreadSpawnEdgeStatus) String() string { return string(s) }

// IsValid reports whether s is one of the known statuses.
func (s ThreadSpawnEdgeStatus) IsValid() bool {
	switch s {
	case ThreadSpawnEdgeStatusOpen, ThreadSpawnEdgeStatusClosed:
		return true
	default:
		return false
	}
}

// AgentGraphStore is the storage-neutral boundary for persisted thread-spawn
// parent/child topology, mirroring the Rust `AgentGraphStore` trait.
//
// Implementations are expected to return stable ordering for the list methods.
type AgentGraphStore interface {
	// UpsertThreadSpawnEdge inserts or replaces the directional parent/child edge
	// for a spawned thread. A child has at most one persisted parent; re-inserting
	// the same child updates both the parent and the status to the supplied
	// values.
	UpsertThreadSpawnEdge(ctx context.Context, parentThreadID, childThreadID protocol.ThreadID, status ThreadSpawnEdgeStatus) error

	// SetThreadSpawnEdgeStatus updates the persisted lifecycle status of a spawned
	// thread's incoming edge. A missing child is treated as a successful no-op.
	SetThreadSpawnEdgeStatus(ctx context.Context, childThreadID protocol.ThreadID, status ThreadSpawnEdgeStatus) error

	// ListThreadSpawnChildren lists direct spawned children of a parent thread.
	//
	// When statusFilter is non-nil only child edges with that exact status are
	// returned; when it is nil all direct child edges are returned regardless of
	// status. Results are ordered by child thread id.
	ListThreadSpawnChildren(ctx context.Context, parentThreadID protocol.ThreadID, statusFilter *ThreadSpawnEdgeStatus) ([]protocol.ThreadID, error)

	// ListThreadSpawnDescendants lists spawned descendants breadth-first by depth,
	// then by thread id.
	//
	// statusFilter is applied to every traversed edge, not only to the returned
	// descendants. For example, a filter of Open walks only open edges, so
	// descendants under a closed edge are not included even if their own incoming
	// edge is open. A nil filter walks and returns every persisted edge.
	ListThreadSpawnDescendants(ctx context.Context, rootThreadID protocol.ThreadID, statusFilter *ThreadSpawnEdgeStatus) ([]protocol.ThreadID, error)
}
