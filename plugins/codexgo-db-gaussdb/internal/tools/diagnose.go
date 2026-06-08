package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
)

// Multi-dimension open-ended diagnosis ("当前数据库有什么问题").
//
// The plugin collects ALL dimensions deterministically — health + wait events +
// top slow SQL + lock-blocking chain + dead-tuple distribution + index summary —
// so coverage never depends on the model choosing the right tools (the core
// difference from opendb, which forces the model to call 6 tools via prompt).
// Each probe is independently fault-tolerant (a failure becomes a warning, the
// rest of the diagnosis still runs).

type DiagnosisData struct {
	Health    HealthReport
	Waits     []waitRow
	SlowSQL   []diagSlowRow
	LockChain []lockRow
	DeadTop   []deadRow
	IdxUnused int
	IdxLarge  int
	Warnings  []string
}

type waitRow struct {
	Type  string
	Event string
	Count int
	Pct   float64
}
type diagSlowRow struct{ SQLID, AvgMS, Calls, TotalSec, CacheHit string }
type lockRow struct{ BlockedPID, BlockerPID, Query string }
type deadRow struct{ Table, Dead, Live, Pct string }

// collectDiagnosis runs every dimension probe and returns the combined evidence.
func collectDiagnosis(ctx context.Context, conn *db.Conn) *DiagnosisData {
	d := &DiagnosisData{Health: runHealth(ctx, conn)}
	d.Waits = collectWaitDist(ctx, conn, d)
	d.SlowSQL = collectTopSlow(ctx, conn, d)
	d.LockChain = collectLockChain(ctx, conn, d)
	d.DeadTop = collectDeadTop(ctx, conn, d)
	collectIdxSummary(ctx, conn, d)
	return d
}

// collectWaitDist: active-session wait-type/event distribution with percentage.
func collectWaitDist(ctx context.Context, conn *db.Conn, d *DiagnosisData) []waitRow {
	res, err := conn.Query(ctx, `SELECT
  CASE WHEN waiting THEN 'Lock' ELSE 'CPU' END AS wt,
  CASE WHEN waiting THEN 'lock_wait' WHEN enqueue <> '' THEN enqueue ELSE 'On CPU' END AS we,
  count(*) AS cnt
FROM pg_stat_activity
WHERE state = 'active' AND pid <> pg_backend_pid()
GROUP BY wt, we
ORDER BY cnt DESC`)
	if err != nil {
		d.Warnings = append(d.Warnings, "等待事件采集失败: "+firstLine(err.Error()))
		return nil
	}
	var rows []waitRow
	total := 0
	for _, r := range res.Rows {
		if len(r) < 3 {
			continue
		}
		c := int(atof(r[2]))
		rows = append(rows, waitRow{Type: r[0], Event: r[1], Count: c})
		total += c
	}
	for i := range rows {
		if total > 0 {
			rows[i].Pct = 100 * float64(rows[i].Count) / float64(total)
		}
	}
	return rows
}

// collectTopSlow: top-5 slow SQL by avg elapsed (>1000ms), from dbe_perf.statement.
func collectTopSlow(ctx context.Context, conn *db.Conn, d *DiagnosisData) []diagSlowRow {
	res, err := conn.Query(ctx, `SELECT
  unique_sql_id,
  ROUND((total_elapse_time/NULLIF(n_calls,0))/1000::numeric, 2) AS avg_ms,
  n_calls AS calls,
  ROUND(total_elapse_time/1000000::numeric, 2) AS total_sec,
  CASE WHEN (n_blocks_hit + n_blocks_fetched) > 0
       THEN ROUND(n_blocks_hit::numeric*100/(n_blocks_hit + n_blocks_fetched), 1)::text
       ELSE '-' END AS cache_hit
FROM dbe_perf.statement
WHERE n_calls > 0 AND (total_elapse_time/NULLIF(n_calls,0))/1000 > 1000
ORDER BY total_elapse_time/NULLIF(n_calls,0) DESC
LIMIT 5`)
	if err != nil {
		d.Warnings = append(d.Warnings, "慢 SQL 采集失败: "+firstLine(err.Error()))
		return nil
	}
	var rows []diagSlowRow
	for _, r := range res.Rows {
		if len(r) < 5 {
			continue
		}
		rows = append(rows, diagSlowRow{SQLID: r[0], AvgMS: r[1], Calls: r[2], TotalSec: r[3], CacheHit: r[4]})
	}
	return rows
}

// collectLockChain: blocked->blocker pairs via pg_locks self-join. The kl.granted
// requirement is CRITICAL — without it an N-waiter pile-up explodes into N×(N-1)
// rows (opendb's lesson). Bounded LIMIT 20.
func collectLockChain(ctx context.Context, conn *db.Conn, d *DiagnosisData) []lockRow {
	res, err := conn.Query(ctx, `SELECT
  w.pid AS blocked_pid,
  h.pid AS blocker_pid,
  LEFT(REGEXP_REPLACE(COALESCE(a.query,''), E'\\s+', ' ', 'g'), 60) AS blocked_query
FROM pg_locks w
JOIN pg_locks h
  ON w.locktype = h.locktype
  AND w.database IS NOT DISTINCT FROM h.database
  AND w.relation IS NOT DISTINCT FROM h.relation
  AND w.pid <> h.pid
LEFT JOIN pg_stat_activity a ON a.pid = w.pid
WHERE NOT w.granted AND h.granted
LIMIT 20`)
	if err != nil {
		d.Warnings = append(d.Warnings, "锁阻塞采集失败: "+firstLine(err.Error()))
		return nil
	}
	var rows []lockRow
	for _, r := range res.Rows {
		if len(r) < 3 {
			continue
		}
		rows = append(rows, lockRow{BlockedPID: r[0], BlockerPID: r[1], Query: r[2]})
	}
	return rows
}

// collectDeadTop: top-5 tables by dead tuples (per-table distribution, vs health's total).
func collectDeadTop(ctx context.Context, conn *db.Conn, d *DiagnosisData) []deadRow {
	res, err := conn.Query(ctx, `SELECT
  relname,
  n_dead_tup,
  n_live_tup,
  CASE WHEN (n_dead_tup + n_live_tup) > 0
       THEN ROUND(100.0*n_dead_tup/(n_dead_tup + n_live_tup), 1)::text
       ELSE '0' END AS dead_pct
FROM pg_stat_user_tables
WHERE n_dead_tup > 0
ORDER BY n_dead_tup DESC
LIMIT 5`)
	if err != nil {
		d.Warnings = append(d.Warnings, "死元组分布采集失败: "+firstLine(err.Error()))
		return nil
	}
	var rows []deadRow
	for _, r := range res.Rows {
		if len(r) < 4 {
			continue
		}
		rows = append(rows, deadRow{Table: r[0], Dead: r[1], Live: r[2], Pct: r[3]})
	}
	return rows
}

// collectIdxSummary: counts of unused (idx_scan=0) and large (>10MB) indexes.
func collectIdxSummary(ctx context.Context, conn *db.Conn, d *DiagnosisData) {
	if v, err := conn.QueryScalar(ctx, "SELECT count(*) FROM pg_stat_user_indexes WHERE idx_scan = 0"); err == nil {
		d.IdxUnused = int(atof(v))
	} else {
		d.Warnings = append(d.Warnings, "索引采集失败: "+firstLine(err.Error()))
	}
	if v, err := conn.QueryScalar(ctx, "SELECT count(*) FROM pg_stat_user_indexes WHERE pg_relation_size(indexrelid) > 10*1024*1024"); err == nil {
		d.IdxLarge = int(atof(v))
	}
}

// --- problem detection ------------------------------------------------------

// diagProblem is one detected issue across dimensions, for the 核心问题 section.
type diagProblem struct {
	Severity string // statusFail | statusWarn
	Dim      string // 维度
	Title    string
	Evidence string
	Hint     string
}

// detectDiagProblems aggregates problems across all dimensions, most severe first.
func detectDiagProblems(d *DiagnosisData) []diagProblem {
	var ps []diagProblem

	// health WARN/FAIL items (cache hit / dead tuples total / XID / connections ...)
	for _, it := range healthProblems(&d.Health) {
		ps = append(ps, diagProblem{
			Severity: it.Status, Dim: it.Category, Title: it.Name,
			Evidence: it.Value + thresholdSuffix(it.Threshold), Hint: it.Suggestion,
		})
	}

	// lock blocking — always serious
	if n := len(d.LockChain); n > 0 {
		ps = append(ps, diagProblem{
			Severity: statusFail, Dim: "锁", Title: "存在锁阻塞",
			Evidence: fmt.Sprintf("%d 个被阻塞会话(阻塞源 pid %s)", n, d.LockChain[0].BlockerPID),
			Hint:     "定位阻塞源会话,确认长事务/未提交,必要时 /kill 阻塞源",
		})
	}

	// slow SQL — FAIL if the worst avg exceeds 1 minute
	if n := len(d.SlowSQL); n > 0 {
		sev := statusWarn
		if atof(d.SlowSQL[0].AvgMS) >= 60000 {
			sev = statusFail
		}
		ps = append(ps, diagProblem{
			Severity: sev, Dim: "慢SQL", Title: "存在慢 SQL",
			Evidence: fmt.Sprintf("%d 条 >1s;最慢 sql_id %s 平均 %sms 命中 %s%%", n, d.SlowSQL[0].SQLID, d.SlowSQL[0].AvgMS, d.SlowSQL[0].CacheHit),
			Hint:     "对最慢 SQL 调 sqltune 深度调优(改写/索引,自动校验)",
		})
	}

	// unused indexes
	if d.IdxUnused > 0 {
		ps = append(ps, diagProblem{
			Severity: statusWarn, Dim: "索引", Title: "未使用索引",
			Evidence: fmt.Sprintf("%d 个零扫描索引(另有 %d 个大索引)", d.IdxUnused, d.IdxLarge),
			Hint:     "测试环境确认无依赖后清理,回收空间并减少写放大",
		})
	}

	sort.SliceStable(ps, func(i, j int) bool {
		return statusRank(ps[i].Severity) > statusRank(ps[j].Severity)
	})
	return ps
}

func thresholdSuffix(t string) string {
	if strings.TrimSpace(t) == "" {
		return ""
	}
	return "(阈值 " + t + ")"
}
