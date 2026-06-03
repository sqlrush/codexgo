package agentgraph

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/state"
)

const sqliteDriverName = "sqlite"

// LocalAgentGraphStore is the SQLite-backed implementation of [AgentGraphStore].
//
// It mirrors the Rust `LocalAgentGraphStore`, which wraps an already-initialized
// state runtime. Because the state runtime owns schema creation/migration, this
// store must be constructed from a runtime that has already been initialized; it
// then opens its own connection to the same `state_5.sqlite` database file to run
// the thread-spawn-edge queries.
type LocalAgentGraphStore struct {
	codexHome string

	pool      *sql.DB
	closeOnce sync.Once
}

var _ AgentGraphStore = (*LocalAgentGraphStore)(nil)

// NewLocalAgentGraphStore creates a local graph store from an already-initialized
// state runtime, mirroring the Rust `LocalAgentGraphStore::new`.
//
// The runtime must already have created and migrated the state database (the
// thread_spawn_edges table). The returned store opens a separate connection pool
// to the same database file; call [LocalAgentGraphStore.Close] to release it.
func NewLocalAgentGraphStore(ctx context.Context, runtime *state.StateRuntime) (*LocalAgentGraphStore, error) {
	if runtime == nil {
		return nil, invalidRequestError("agent graph store requires an initialized state runtime")
	}
	codexHome := runtime.CodexHome()
	path := state.StateDBPath(codexHome)
	db, err := sql.Open(sqliteDriverName, stateDSN(path))
	if err != nil {
		return nil, internalError(err, "open state db at %q", path)
	}
	db.SetMaxOpenConns(5)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, internalError(err, "connect state db at %q", path)
	}
	return &LocalAgentGraphStore{codexHome: codexHome, pool: db}, nil
}

// CodexHome returns the Codex home directory backing this store.
func (s *LocalAgentGraphStore) CodexHome() string { return s.codexHome }

// Close releases the connection pool. It is safe to call multiple times.
func (s *LocalAgentGraphStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.pool != nil {
			err = s.pool.Close()
		}
	})
	return err
}

// stateDSN builds a modernc.org/sqlite DSN matching codex's connect options. It
// mirrors the connection PRAGMAs used by the state runtime so this store shares
// the same locking/journaling behavior as the runtime that created the DB.
func stateDSN(path string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "auto_vacuum(INCREMENTAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	return "file:" + path + "?" + q.Encode()
}

// UpsertThreadSpawnEdge inserts or replaces the directional parent/child edge for
// a spawned thread, mirroring the Rust `upsert_thread_spawn_edge`.
func (s *LocalAgentGraphStore) UpsertThreadSpawnEdge(
	ctx context.Context,
	parentThreadID, childThreadID protocol.ThreadID,
	status ThreadSpawnEdgeStatus,
) error {
	const query = `
INSERT INTO thread_spawn_edges (
    parent_thread_id,
    child_thread_id,
    status
) VALUES (?, ?, ?)
ON CONFLICT(child_thread_id) DO UPDATE SET
    parent_thread_id = excluded.parent_thread_id,
    status = excluded.status
`
	if _, err := s.pool.ExecContext(ctx, query, parentThreadID.String(), childThreadID.String(), status.String()); err != nil {
		return internalError(err, "upsert thread spawn edge for child %s", childThreadID)
	}
	return nil
}

// SetThreadSpawnEdgeStatus updates the persisted lifecycle status of a spawned
// thread's incoming edge, mirroring the Rust `set_thread_spawn_edge_status`. A
// missing child is a successful no-op.
func (s *LocalAgentGraphStore) SetThreadSpawnEdgeStatus(
	ctx context.Context,
	childThreadID protocol.ThreadID,
	status ThreadSpawnEdgeStatus,
) error {
	const query = "UPDATE thread_spawn_edges SET status = ? WHERE child_thread_id = ?"
	if _, err := s.pool.ExecContext(ctx, query, status.String(), childThreadID.String()); err != nil {
		return internalError(err, "set thread spawn edge status for child %s", childThreadID)
	}
	return nil
}

// ListThreadSpawnChildren lists direct spawned children of parentThreadID,
// optionally filtered by status, mirroring the Rust
// `list_thread_spawn_children`/`_with_status`. Results are ordered by child
// thread id.
func (s *LocalAgentGraphStore) ListThreadSpawnChildren(
	ctx context.Context,
	parentThreadID protocol.ThreadID,
	statusFilter *ThreadSpawnEdgeStatus,
) ([]protocol.ThreadID, error) {
	var builder strings.Builder
	builder.WriteString("SELECT child_thread_id FROM thread_spawn_edges WHERE parent_thread_id = ?")
	args := []any{parentThreadID.String()}
	if statusFilter != nil {
		builder.WriteString(" AND status = ?")
		args = append(args, statusFilter.String())
	}
	builder.WriteString(" ORDER BY child_thread_id")

	return s.queryThreadIDs(ctx, builder.String(), args, "list thread spawn children")
}

// ListThreadSpawnDescendants lists spawned descendants of rootThreadID
// breadth-first by depth then thread id, optionally filtered by status applied to
// every traversed edge, mirroring the Rust `list_thread_spawn_descendants`.
func (s *LocalAgentGraphStore) ListThreadSpawnDescendants(
	ctx context.Context,
	rootThreadID protocol.ThreadID,
	statusFilter *ThreadSpawnEdgeStatus,
) ([]protocol.ThreadID, error) {
	var builder strings.Builder
	builder.WriteString(`
WITH RECURSIVE subtree(child_thread_id, depth) AS (
    SELECT child_thread_id, 1
    FROM thread_spawn_edges
    WHERE parent_thread_id = ?`)
	args := []any{rootThreadID.String()}
	if statusFilter != nil {
		status := statusFilter.String()
		builder.WriteString(" AND status = ?")
		args = append(args, status)
		builder.WriteString(`
    UNION ALL
    SELECT edge.child_thread_id, subtree.depth + 1
    FROM thread_spawn_edges AS edge
    JOIN subtree ON edge.parent_thread_id = subtree.child_thread_id
    WHERE status = ?`)
		args = append(args, status)
	} else {
		builder.WriteString(`
    UNION ALL
    SELECT edge.child_thread_id, subtree.depth + 1
    FROM thread_spawn_edges AS edge
    JOIN subtree ON edge.parent_thread_id = subtree.child_thread_id`)
	}
	builder.WriteString(`
)
SELECT child_thread_id
FROM subtree
ORDER BY depth ASC, child_thread_id ASC`)

	return s.queryThreadIDs(ctx, builder.String(), args, "list thread spawn descendants")
}

// queryThreadIDs runs query with args and collects the single child_thread_id
// column into thread ids, wrapping failures as internal errors.
func (s *LocalAgentGraphStore) queryThreadIDs(ctx context.Context, query string, args []any, what string) ([]protocol.ThreadID, error) {
	rows, err := s.pool.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, internalError(err, "%s", what)
	}
	defer rows.Close()

	var out []protocol.ThreadID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, internalError(err, "%s: scan row", what)
		}
		out = append(out, protocol.NewThreadID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, internalError(err, "%s: iterate rows", what)
	}
	return out, nil
}
