package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
)

// Schema context collection (sqltune parity #7): for the tables referenced by a
// SQL, gather table sizes, indexes, freshness stats and foreign keys so the
// model can reason about missing/over-large indexes and stale statistics.

// SchemaContext bundles the per-table metadata sections.
type SchemaContext struct {
	Tables  TableReport `json:"tables"`
	Indexes TableReport `json:"indexes"`
	Stats   TableReport `json:"stats"`
	FKs     TableReport `json:"foreign_keys"`
	Names   []string    `json:"table_names"`
}

var (
	// reFromList captures the FROM-list text up to the next major clause / JOIN,
	// so comma-separated implicit joins (FROM a, b, c) are ALL picked up — not
	// just the first table.
	reFromList = regexp.MustCompile(`(?is)\bFROM\s+(.*?)(?:\bWHERE\b|\bGROUP\b|\bORDER\b|\bHAVING\b|\bLIMIT\b|\bUNION\b|\bJOIN\b|\bLEFT\b|\bRIGHT\b|\bINNER\b|\bFULL\b|\bCROSS\b|\)|$)`)
	// reJoinRef captures the table after each explicit JOIN.
	reJoinRef = regexp.MustCompile(`(?i)\bJOIN\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)`)
)

var sqlKeywordsAfterFrom = map[string]bool{
	"select": true, "lateral": true, "where": true, "group": true, "order": true,
}

// extractTableNames pulls candidate table names from FROM (including
// comma-separated implicit joins) and JOIN clauses, across subqueries. Returns
// the unqualified names (schema.tbl -> tbl), deduped, lowercased, sorted.
func extractTableNames(sql string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
			name = name[dot+1:]
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || sqlKeywordsAfterFrom[name] || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, m := range reJoinRef.FindAllStringSubmatch(sql, -1) {
		add(m[1])
	}
	for _, m := range reFromList.FindAllStringSubmatch(sql, -1) {
		for _, part := range strings.Split(m[1], ",") {
			f := strings.Fields(strings.TrimSpace(part))
			if len(f) == 0 {
				continue
			}
			if tok := f[0]; !strings.ContainsAny(tok, "()") { // skip subquery/function
				add(tok)
			}
		}
	}
	sort.Strings(out)
	return out
}

// sqlInList renders names as a quoted SQL IN-list: 'a','b'. Names are validated
// to identifier chars by the regex, so no injection surface.
func sqlInList(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = "'" + strings.ReplaceAll(n, "'", "''") + "'"
	}
	return strings.Join(parts, ",")
}

// bestSchema picks the schema that owns the most of the query's tables, so the
// tuner can pin search_path to it and avoid cross-schema same-name ambiguity
// (e.g. public.orders vs sqltune_demo.orders). System schemas are excluded.
// Returns "" when undetermined.
func bestSchema(ctx context.Context, conn *db.Conn, names []string) string {
	if len(names) == 0 {
		return ""
	}
	in := sqlInList(names)
	s, err := conn.QueryScalar(ctx, fmt.Sprintf(`SELECT n.nspname
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relname IN (%s) AND c.relkind IN ('r','p','v','m')
  AND n.nspname NOT IN ('pg_catalog','information_schema','pg_toast')
GROUP BY n.nspname
ORDER BY count(DISTINCT c.relname) DESC, n.nspname
LIMIT 1`, in))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// collectSchema gathers the schema sections for the given tables. Each section is
// independently fault-tolerant (a failing query yields an empty section + note).
func collectSchema(ctx context.Context, conn *db.Conn, names []string) SchemaContext {
	sc := SchemaContext{Names: names}
	if len(names) == 0 {
		return sc
	}
	in := sqlInList(names)
	target := conn.Label()

	if res, err := conn.Query(ctx, fmt.Sprintf(`SELECT
  c.relname AS table_name,
  CASE c.relkind WHEN 'r' THEN 'table' WHEN 'p' THEN 'partitioned' WHEN 'v' THEN 'view' WHEN 'm' THEN 'matview' ELSE c.relkind::text END AS kind,
  c.reltuples::bigint AS est_rows,
  pg_size_pretty(pg_total_relation_size(c.oid)) AS total_size
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relname IN (%s) AND c.relkind IN ('r','p','v','m') AND pg_table_is_visible(c.oid)
ORDER BY pg_total_relation_size(c.oid) DESC`, in)); err == nil {
		sc.Tables = tableReport("表", target, "est_rows 是优化器估算行数;若与实际差很多说明统计陈旧。", nil, res)
	}

	if res, err := conn.Query(ctx, fmt.Sprintf(`SELECT
  relname AS table_name,
  indexrelname AS index_name,
  idx_scan AS scans,
  pg_size_pretty(pg_relation_size(indexrelid)) AS size
FROM pg_stat_user_indexes
WHERE relname IN (%s) AND pg_table_is_visible((quote_ident(schemaname) || '.' || quote_ident(relname))::regclass)
ORDER BY relname, idx_scan DESC`, in)); err == nil {
		sc.Indexes = tableReport("索引", target, "scans=0 的索引可能没用上;缺关键过滤/连接列的索引则要补。", nil, res)
	}

	if res, err := conn.Query(ctx, fmt.Sprintf(`SELECT
  relname AS table_name,
  n_live_tup AS live_rows,
  n_dead_tup AS dead_rows,
  COALESCE(to_char(last_analyze,'YYYY-MM-DD HH24:MI'), to_char(last_autoanalyze,'YYYY-MM-DD HH24:MI'), '从未') AS last_analyze
FROM pg_stat_user_tables
WHERE relname IN (%s) AND pg_table_is_visible((quote_ident(schemaname) || '.' || quote_ident(relname))::regclass)
ORDER BY n_live_tup DESC`, in)); err == nil {
		sc.Stats = tableReport("统计信息新鲜度", target, "last_analyze 很旧或'从未',且 dead_rows 多 → 先 ANALYZE 再看计划。", nil, res)
	}

	if res, err := conn.Query(ctx, fmt.Sprintf(`SELECT
  c.relname AS table_name,
  con.conname AS fk_name,
  pg_get_constraintdef(con.oid) AS definition
FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
WHERE con.contype = 'f' AND c.relname IN (%s) AND pg_table_is_visible(c.oid)`, in)); err == nil {
		sc.FKs = tableReport("外键", target, "外键列通常是连接键,确认其有索引。", nil, res)
	}

	return sc
}
