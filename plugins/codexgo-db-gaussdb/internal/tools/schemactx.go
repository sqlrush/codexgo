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

var reTableRef = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|UPDATE|INTO|DELETE\s+FROM)\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)`)

var sqlKeywordsAfterFrom = map[string]bool{
	"select": true, "lateral": true, "where": true, "group": true, "order": true,
}

// extractTableNames pulls candidate table names from FROM/JOIN/etc. clauses. It
// returns the unqualified names (schema.tbl -> tbl), deduped, lowercased.
func extractTableNames(sql string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range reTableRef.FindAllStringSubmatch(sql, -1) {
		name := m[1]
		if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
			name = name[dot+1:]
		}
		name = strings.ToLower(name)
		if name == "" || sqlKeywordsAfterFrom[name] || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
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
  schemaname || '.' || relname AS table_name,
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
