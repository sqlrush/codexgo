package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// registerQuery wires the dbe_perf.statement-based tools: slowsql, topsql,
// sqlfetch, planhistory. SQL is adapted from opendb's opengauss skills (GaussDB
// reuses openGauss views) and enriched with extra columns for richer evaluation.
func registerQuery(s *mcp.Server, conn *db.Conn) {
	registerSlowSQL(s, conn)
	registerTopSQL(s, conn)
	registerSQLFetch(s, conn)
	registerPlanHistory(s, conn)
}

// --- slowsql ---------------------------------------------------------------

type slowSQLArgs struct {
	ThresholdMS int `json:"threshold_ms"`
	Limit       int `json:"limit"`
}

func registerSlowSQL(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "slowsql",
		Description: "List slow SQL from dbe_perf.statement ranked by average elapsed time per call. Args: threshold_ms (min avg ms, default 1000), limit (default 20, max 100). Adds max_ms (latency variance) and per-SQL cache_hit_pct over opendb. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"threshold_ms": intProp("minimum average ms per call to include (default 1000)"),
			"limit":        intProp("max rows (default 20, max 100)"),
		}),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a slowSQLArgs
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		if a.ThresholdMS <= 0 {
			a.ThresholdMS = 1000
		}
		limit := clampLimit(a.Limit, 20, 100)
		query := fmt.Sprintf(`SELECT
  unique_sql_id,
  LEFT(REGEXP_REPLACE(query, E'\\s+', ' ', 'g'), 180) AS query,
  n_calls AS calls,
  ROUND((total_elapse_time/NULLIF(n_calls,0))/1000::numeric, 2) AS avg_ms,
  ROUND(max_elapse_time/1000::numeric, 2) AS max_ms,
  ROUND(total_elapse_time/1000000::numeric, 2) AS total_sec,
  n_returned_rows AS rows,
  CASE WHEN (n_blocks_hit + n_blocks_fetched) > 0
       THEN ROUND(n_blocks_hit::numeric*100/(n_blocks_hit + n_blocks_fetched), 1)
       ELSE NULL END AS cache_hit_pct
FROM dbe_perf.statement
WHERE (total_elapse_time/NULLIF(n_calls,0))/1000 > %d
  AND n_calls > 0
ORDER BY total_elapse_time/NULLIF(n_calls,0) DESC
LIMIT %d`, a.ThresholdMS, limit)

		res, err := conn.Query(ctx, query)
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		report := tableReport(
			"慢 SQL (按平均单次耗时排序)", conn.Label(),
			"avg_ms 超过阈值的语句;max_ms 反映抖动,cache_hit_pct 偏低说明走磁盘/缺索引。用 unique_sql_id 调 sqlfetch/sqltune 下钻。",
			map[string]string{"threshold_ms": strconv.Itoa(a.ThresholdMS), "limit": strconv.Itoa(limit)},
			res,
		)
		return tableResult(report)
	})
}

// --- topsql ----------------------------------------------------------------

// topSQLSorts whitelists the sort dimension → ORDER BY clause. ORDER BY cannot
// be parameterized, so the whitelist prevents injection.
var topSQLSorts = map[string]string{
	"el": "total_elapse_time DESC",                   // total elapsed
	"ae": "total_elapse_time/NULLIF(n_calls,0) DESC", // avg elapsed
	"ex": "n_calls DESC",                             // executions
	"lr": "(n_blocks_hit + n_blocks_fetched) DESC",   // logical reads
	"rw": "n_returned_rows DESC",                     // rows returned
}

type topSQLArgs struct {
	Sort  string `json:"sort"`
	Limit int    `json:"limit"`
}

func registerTopSQL(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "topsql",
		Description: "Top SQL from dbe_perf.statement by a chosen dimension. Args: sort one of el(total elapsed,default) | ae(avg elapsed) | ex(executions) | lr(logical reads) | rw(rows), limit (default 20, max 100). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"sort":  strProp("sort dimension: el|ae|ex|lr|rw (default el)"),
			"limit": intProp("max rows (default 20, max 100)"),
		}),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a topSQLArgs
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		sortKey := strings.ToLower(strings.TrimSpace(a.Sort))
		if sortKey == "" {
			sortKey = "el"
		}
		orderBy, ok := topSQLSorts[sortKey]
		if !ok {
			return mcp.CallToolResult{}, fmt.Errorf("invalid sort %q: use el|ae|ex|lr|rw", a.Sort)
		}
		limit := clampLimit(a.Limit, 20, 100)
		query := fmt.Sprintf(`SELECT
  unique_sql_id,
  LEFT(REGEXP_REPLACE(query, E'\\s+', ' ', 'g'), 100) AS query,
  n_calls AS calls,
  ROUND(total_elapse_time/1000000::numeric, 2) AS total_sec,
  ROUND((total_elapse_time/NULLIF(n_calls,0))/1000::numeric, 2) AS avg_ms,
  (n_blocks_hit + n_blocks_fetched) AS logical_reads,
  n_returned_rows AS rows
FROM dbe_perf.statement
WHERE n_calls > 0
ORDER BY %s
LIMIT %d`, orderBy, limit)

		res, err := conn.Query(ctx, query)
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		report := tableReport(
			"Top SQL ("+topSQLLabel(sortKey)+")", conn.Label(),
			"按所选维度排名的资源消耗大户;聚焦排名靠前者用 sqltune 优化。",
			map[string]string{"sort": sortKey, "limit": strconv.Itoa(limit)},
			res,
		)
		return tableResult(report)
	})
}

func topSQLLabel(k string) string {
	switch k {
	case "ae":
		return "按平均耗时"
	case "ex":
		return "按执行次数"
	case "lr":
		return "按逻辑读"
	case "rw":
		return "按返回行数"
	default:
		return "按总耗时"
	}
}

// --- sqlfetch --------------------------------------------------------------

type sqlFetchArgs struct {
	SQLID string `json:"sql_id"`
}

// SQLFetchResult is the resolved SQL plus provenance metadata. Carrying source
// and placeholder info (vs opendb's plain text) lets the LLM decide whether the
// SQL is EXPLAIN-ready.
type SQLFetchResult struct {
	SQLID        string `json:"sql_id"`
	Query        string `json:"query"`
	Source       string `json:"source"` // statement_history | statement
	Schema       string `json:"schema,omitempty"`
	StartTime    string `json:"start_time,omitempty"`
	HasLiterals  bool   `json:"has_literals"`
	Placeholders int    `json:"placeholders"`
	Note         string `json:"note,omitempty"`
}

func registerSQLFetch(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "sqlfetch",
		Description: "Resolve a unique SQL id to its SQL text. Prefers dbe_perf.statement_history (carries literals), falls back to dbe_perf.statement (normalized with ? placeholders). Arg: sql_id (required). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"sql_id": strProp("unique SQL id (unique_sql_id / unique_query_id)"),
		}, "sql_id"),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a sqlFetchArgs
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		id := strings.TrimSpace(a.SQLID)
		if id == "" {
			return mcp.CallToolResult{}, fmt.Errorf("sql_id is required")
		}
		res := resolveSQL(ctx, conn, id)
		if res.Query == "" {
			return mcp.CallToolResult{}, fmt.Errorf("sql_id %s not found in statement_history or statement", id)
		}
		return sqlFetchResult(res)
	})
}

// resolveSQL performs the two-stage lookup. Exported-ish for reuse by sqltune.
func resolveSQL(ctx context.Context, conn *db.Conn, id string) SQLFetchResult {
	out := SQLFetchResult{SQLID: id}

	// Stage 1: statement_history (has literals).
	hist, err := conn.Query(ctx, `SELECT schema_name, query, start_time::text
FROM dbe_perf.statement_history
WHERE unique_query_id = $1
  AND query NOT LIKE '/* missing SQL statement%'
ORDER BY start_time DESC
LIMIT 1`, id)
	if err == nil && len(hist.Rows) > 0 {
		row := hist.Rows[0]
		out.Schema, out.Query, out.StartTime = row[0], row[1], row[2]
		out.Source = "statement_history"
	} else {
		// Stage 2: statement (normalized).
		st, err2 := conn.Query(ctx, `SELECT query FROM dbe_perf.statement WHERE unique_sql_id = $1 LIMIT 1`, id)
		if err2 == nil && len(st.Rows) > 0 {
			out.Query = st.Rows[0][0]
			out.Source = "statement"
		}
	}
	out.Placeholders = countPlaceholders(out.Query)
	out.HasLiterals = out.Query != "" && out.Placeholders == 0
	if out.Source == "statement" && !out.HasLiterals {
		out.Note = "归一化语句含占位符,EXPLAIN 前需用真实字面量替换;如需带字面量请提高 track_stmt_stat_level。"
	}
	return out
}

// countPlaceholders counts ? / $N / :N occurrences outside string literals — a
// rough EXPLAIN-readiness signal.
func countPlaceholders(q string) int {
	n := 0
	inStr := false
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c == '\'' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch {
		case c == '?':
			n++
		case c == '$' && i+1 < len(q) && q[i+1] >= '0' && q[i+1] <= '9':
			n++
		case c == ':' && i+1 < len(q) && q[i+1] >= '0' && q[i+1] <= '9':
			n++
		}
	}
	return n
}

// --- planhistory -----------------------------------------------------------

type planHistoryArgs struct {
	SQLID string `json:"sql_id"`
	Limit int    `json:"limit"`
}

func registerPlanHistory(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "planhistory",
		Description: "Show recent executions of one SQL id from dbe_perf.statement_history with timing and execution plan, to spot plan regressions over time. Args: sql_id (required), limit (default 10, max 50). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"sql_id": strProp("unique SQL id (unique_query_id)"),
			"limit":  intProp("max executions (default 10, max 50)"),
		}, "sql_id"),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a planHistoryArgs
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		id := strings.TrimSpace(a.SQLID)
		if id == "" {
			return mcp.CallToolResult{}, fmt.Errorf("sql_id is required")
		}
		limit := clampLimit(a.Limit, 10, 50)
		query := fmt.Sprintf(`SELECT
  start_time::text AS start_time,
  ROUND(db_time/1000::numeric, 2) AS db_ms,
  ROUND(execution_time/1000::numeric, 2) AS exec_ms,
  ROUND(cpu_time/1000::numeric, 2) AS cpu_ms,
  n_hard_parse AS hard_parse,
  n_returned_rows AS rows,
  query_plan
FROM dbe_perf.statement_history
WHERE unique_query_id = $1
ORDER BY start_time DESC
LIMIT %d`, limit)

		res, err := conn.Query(ctx, query, id)
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		report := tableReport(
			"执行计划历史", conn.Label(),
			"对比各次执行的 exec_ms 与 query_plan:耗时跳变且计划文本变化即计划回退(plan regression),需固化计划或更新统计信息。",
			map[string]string{"sql_id": id, "limit": strconv.Itoa(limit)},
			res,
		)
		return tableResult(report)
	})
}
