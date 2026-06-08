package tools

import (
	"context"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
)

// Runtime context snapshot (sqltune parity #8): current wait distribution and
// any blocking locks, so the model can tell a plan problem from contention.

// RuntimeContext bundles the runtime snapshot sections.
type RuntimeContext struct {
	Waits TableReport `json:"waits"`
	Locks TableReport `json:"locks"`
}

// collectRuntime snapshots active-session waits and blocking locks. Each section
// degrades to empty on error (e.g. missing privileges/columns).
func collectRuntime(ctx context.Context, conn *db.Conn) RuntimeContext {
	rc := RuntimeContext{}
	target := conn.Label()

	if res, err := conn.Query(ctx, `SELECT
  CASE WHEN waiting THEN 'Wait' ELSE 'CPU' END AS state,
  COALESCE(wait_status, CASE WHEN waiting THEN 'lock_wait' ELSE 'On CPU' END) AS detail,
  COUNT(*) AS sessions
FROM pg_stat_activity
WHERE state = 'active' AND pid <> pg_backend_pid()
GROUP BY state, detail
ORDER BY sessions DESC`); err == nil {
		rc.Waits = tableReport("当前等待分布", target, "On CPU 多=算力瓶颈;等待/锁多=阻塞。仅瞬时快照。", nil, res)
	}

	if res, err := conn.Query(ctx, `SELECT
  a.pid AS waiting_pid,
  a.usename AS db_user,
  l.locktype,
  l.mode,
  LEFT(REGEXP_REPLACE(a.query, E'\\s+', ' ', 'g'), 80) AS query
FROM pg_locks l
JOIN pg_stat_activity a ON a.pid = l.pid
WHERE NOT l.granted
LIMIT 20`); err == nil {
		rc.Locks = tableReport("锁等待(未授予的锁)", target, "有行即有会话在等锁;空表示无锁等待。", nil, res)
	}

	return rc
}
