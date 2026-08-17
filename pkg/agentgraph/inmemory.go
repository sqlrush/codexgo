package agentgraph

import (
	"context"
	"sort"
	"sync"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// InMemoryAgentGraphStore is a process-local [AgentGraphStore] used for tests and
// debug configs.
//
// It reproduces the ordering and traversal semantics of [LocalAgentGraphStore]:
// children are returned ordered by thread id, and descendants are returned
// breadth-first by depth then thread id, with status filters applied to every
// traversed edge.
type InMemoryAgentGraphStore struct {
	mu sync.Mutex
	// edges maps a child thread id to its single incoming edge.
	edges map[string]edge
}

// edge is a single directional parent/child spawn edge.
type edge struct {
	parent string
	status ThreadSpawnEdgeStatus
}

var _ AgentGraphStore = (*InMemoryAgentGraphStore)(nil)

// NewInMemoryAgentGraphStore returns an empty in-memory graph store.
func NewInMemoryAgentGraphStore() *InMemoryAgentGraphStore {
	return &InMemoryAgentGraphStore{edges: make(map[string]edge)}
}

// UpsertThreadSpawnEdge inserts or replaces the edge for childThreadID.
func (s *InMemoryAgentGraphStore) UpsertThreadSpawnEdge(
	_ context.Context,
	parentThreadID, childThreadID protocol.ThreadID,
	status ThreadSpawnEdgeStatus,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edges[childThreadID.String()] = edge{parent: parentThreadID.String(), status: status}
	return nil
}

// SetThreadSpawnEdgeStatus updates the status of childThreadID's edge. A missing
// child is a no-op.
func (s *InMemoryAgentGraphStore) SetThreadSpawnEdgeStatus(
	_ context.Context,
	childThreadID protocol.ThreadID,
	status ThreadSpawnEdgeStatus,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.edges[childThreadID.String()]; ok {
		existing.status = status
		s.edges[childThreadID.String()] = existing
	}
	return nil
}

// ListThreadSpawnChildren lists direct children of parentThreadID ordered by
// thread id, optionally filtered by status.
func (s *InMemoryAgentGraphStore) ListThreadSpawnChildren(
	_ context.Context,
	parentThreadID protocol.ThreadID,
	statusFilter *ThreadSpawnEdgeStatus,
) ([]protocol.ThreadID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var children []string
	for child, e := range s.edges {
		if e.parent != parentThreadID.String() {
			continue
		}
		if statusFilter != nil && e.status != *statusFilter {
			continue
		}
		children = append(children, child)
	}
	sort.Strings(children)
	return toThreadIDs(children), nil
}

// ListThreadSpawnDescendants lists descendants of rootThreadID breadth-first by
// depth then thread id, with statusFilter applied to every traversed edge.
func (s *InMemoryAgentGraphStore) ListThreadSpawnDescendants(
	_ context.Context,
	rootThreadID protocol.ThreadID,
	statusFilter *ThreadSpawnEdgeStatus,
) ([]protocol.ThreadID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// childrenByParent groups child ids under their parent, filtered by status, so
	// the breadth-first walk only follows edges that match the filter.
	childrenByParent := make(map[string][]string)
	for child, e := range s.edges {
		if statusFilter != nil && e.status != *statusFilter {
			continue
		}
		childrenByParent[e.parent] = append(childrenByParent[e.parent], child)
	}

	var ordered []string
	visited := map[string]bool{}
	frontier := []string{rootThreadID.String()}
	for len(frontier) > 0 {
		var next []string
		var level []string
		for _, parent := range frontier {
			for _, child := range childrenByParent[parent] {
				if visited[child] {
					continue
				}
				visited[child] = true
				level = append(level, child)
				next = append(next, child)
			}
		}
		sort.Strings(level)
		ordered = append(ordered, level...)
		frontier = next
	}
	return toThreadIDs(ordered), nil
}

func toThreadIDs(ids []string) []protocol.ThreadID {
	if len(ids) == 0 {
		return nil
	}
	out := make([]protocol.ThreadID, 0, len(ids))
	for _, id := range ids {
		out = append(out, protocol.NewThreadID(id))
	}
	return out
}
