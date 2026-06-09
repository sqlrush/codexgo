package tools

import (
	"context"
	"encoding/json"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// AshReport combines the wait-type distribution (opendb's view) with a detailed
// active-session list (codexgo addition) so the LLM can both characterize the
// workload AND name the offending sessions in one call.
type AshReport struct {
	Target         string      `json:"target"`
	Distribution   TableReport `json:"distribution"`
	ActiveSessions TableReport `json:"active_sessions"`
	Note           string      `json:"note,omitempty"`
}

func registerASH(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "ash",
		Description: "Point-in-time active session sampling from pg_stat_activity: wait-type/event distribution (CPU vs Lock vs wait events) plus the list of currently active sessions with run time and SQL head. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		report := AshReport{Target: conn.Label()}

		dist, err := conn.Query(ctx, `SELECT
  CASE WHEN waiting THEN 'Lock' ELSE 'CPU' END AS wait_type,
  CASE WHEN waiting THEN 'lock_wait' WHEN enqueue <> '' THEN enqueue ELSE 'On CPU' END AS wait_event,
  COUNT(*) AS sessions,
  ROUND(COUNT(*)::numeric / NULLIF(SUM(COUNT(*)) OVER (), 0) * 100, 1) AS pct
FROM pg_stat_activity
WHERE state = 'active' AND pid <> pg_backend_pid()
GROUP BY wait_type, wait_event
ORDER BY sessions DESC`)
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		report.Distribution = tableReport(
			"等待分布", conn.Label(),
			"On CPU 占比高=算力瓶颈(看 Top SQL);Lock/等待事件占比高=阻塞,需查锁与长事务。",
			nil, dist,
		)

		detail, err := conn.Query(ctx, `SELECT
  pid,
  usename AS db_user,
  application_name AS app,
  state,
  COALESCE(wait_status, '') AS wait_status,
  ROUND(EXTRACT(EPOCH FROM (clock_timestamp() - query_start))::numeric, 1) AS run_sec,
  LEFT(REGEXP_REPLACE(query, E'\\s+', ' ', 'g'), 100) AS query
FROM pg_stat_activity
WHERE state = 'active' AND pid <> pg_backend_pid()
ORDER BY query_start ASC
LIMIT 20`)
		if err != nil {
			// detail is an enrichment; degrade gracefully rather than fail the call.
			report.Note = "活动会话明细采集失败(可能缺少 wait_status 列或权限不足),仅返回等待分布。"
		} else {
			report.ActiveSessions = tableReport(
				"活动会话明细", conn.Label(),
				"run_sec 越大越可疑;同一 query 反复出现即热点。结合 unique 化后的 topsql 定位。",
				nil, detail,
			)
		}
		return ashResult(report)
	})
}
