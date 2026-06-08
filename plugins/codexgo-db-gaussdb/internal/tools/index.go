package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// sysSchemaFilter excludes openGauss/GaussDB internal schemas from index checks
// (matches opendb's exclusion set).
const sysSchemaFilter = `'pg_catalog','information_schema','snapshot','dbe_perf','dbe_pldeveloper','dbe_pldebugger','db4ai','gs_logical_cluster','sqladvisor'`

// IndexHealthReport bundles the four index checks plus a roll-up summary. Each
// section is independent: one failing query degrades to an empty section with a
// note rather than aborting (robustness optimization over opendb).
type IndexHealthReport struct {
	Target    string         `json:"target"`
	Summary   map[string]int `json:"summary"` // unused / invalid / duplicate / bloat counts
	Unused    TableReport    `json:"unused"`
	Invalid   TableReport    `json:"invalid"`
	Duplicate TableReport    `json:"duplicate"`
	Bloat     TableReport    `json:"bloat"`
	Notes     []string       `json:"notes,omitempty"`
}

func registerIndexHealth(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "indexhealth",
		Description: "Index health audit: unused indexes (idx_scan=0), invalid/not-ready indexes, duplicate indexes (same column list), and large/bloat-candidate indexes (>10MB). Returns four sections plus a summary count. Args: limit (default 20, max 100). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"limit": intProp("max rows per section (default 20, max 100)"),
		}),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			Limit int `json:"limit"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		limit := clampLimit(a.Limit, 20, 100)
		target := conn.Label()
		report := IndexHealthReport{Target: target, Summary: map[string]int{}}

		report.Unused = idxSection(ctx, conn, &report, "unused", "未使用索引", fmt.Sprintf(`SELECT
  schemaname || '.' || relname AS table_name,
  indexrelname AS index_name,
  idx_scan,
  pg_size_pretty(pg_relation_size(indexrelid)) AS size
FROM pg_stat_user_indexes
WHERE idx_scan = 0
  AND schemaname NOT IN (%s)
ORDER BY pg_relation_size(indexrelid) DESC
LIMIT %d`, sysSchemaFilter, limit),
			"自统计重置以来零扫描;确认非周期性任务用途后可考虑删除以省空间和写开销。")

		report.Invalid = idxSection(ctx, conn, &report, "invalid", "失效/未就绪索引", fmt.Sprintf(`SELECT
  n.nspname || '.' || t.relname AS table_name,
  i.relname AS index_name,
  CASE WHEN NOT x.indisvalid THEN 'invalid'
       WHEN NOT x.indisready THEN 'not ready'
       ELSE 'other' END AS state
FROM pg_index x
JOIN pg_class i ON i.oid = x.indexrelid
JOIN pg_class t ON t.oid = x.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE (NOT x.indisvalid OR NOT x.indisready)
  AND n.nspname NOT IN ('pg_catalog','information_schema')
ORDER BY n.nspname, t.relname
LIMIT %d`, limit),
			"invalid 索引不会被优化器使用,通常是 CREATE INDEX CONCURRENTLY 失败遗留,应重建或删除。")

		report.Duplicate = idxSection(ctx, conn, &report, "duplicate", "重复索引", fmt.Sprintf(`SELECT
  n.nspname || '.' || t.relname AS table_name,
  array_agg(i.relname ORDER BY i.relname) AS duplicate_indexes,
  COUNT(*) AS dup_count
FROM pg_index x
JOIN pg_class i ON i.oid = x.indexrelid
JOIN pg_class t ON t.oid = x.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE n.nspname NOT IN ('pg_catalog','information_schema')
GROUP BY n.nspname, t.relname, x.indkey
HAVING COUNT(*) > 1
ORDER BY COUNT(*) DESC
LIMIT %d`, limit),
			"列组合完全相同的索引,保留一个即可,其余纯属写放大与空间浪费。")

		report.Bloat = idxSection(ctx, conn, &report, "bloat", "大索引(膨胀候选)", fmt.Sprintf(`SELECT
  schemaname || '.' || relname AS table_name,
  indexrelname AS index_name,
  pg_size_pretty(pg_relation_size(indexrelid)) AS size,
  idx_scan
FROM pg_stat_user_indexes
WHERE pg_relation_size(indexrelid) > 10 * 1024 * 1024
  AND schemaname NOT IN (%s)
ORDER BY pg_relation_size(indexrelid) DESC
LIMIT %d`, sysSchemaFilter, limit),
			"超过 10MB 的大索引;若 idx_scan 也低则优先排查,必要时 REINDEX 回收膨胀空间。")

		return jsonResult(report)
	})
}

// idxSection runs one index-check query, records its count into the summary, and
// appends a note on failure (fault-tolerant per section).
func idxSection(ctx context.Context, conn *db.Conn, report *IndexHealthReport, key, title, query, note string) TableReport {
	res, err := conn.Query(ctx, query)
	if err != nil {
		report.Notes = append(report.Notes, title+"采集失败: "+firstLine(err.Error()))
		report.Summary[key] = 0
		return TableReport{Title: title, Target: report.Target, Note: note}
	}
	report.Summary[key] = len(res.Rows)
	tr := tableReport(title, report.Target, note, map[string]string{"count": strconv.Itoa(len(res.Rows))}, res)
	return tr
}
