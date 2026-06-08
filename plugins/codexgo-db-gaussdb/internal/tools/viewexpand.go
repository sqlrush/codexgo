package tools

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
)

// View definitions (sqltune parity #3). The planner already expands views for
// EXPLAIN, so rather than risk a regex rewrite of the SQL, we surface the
// definitions of any referenced views so the model can reason over the real
// underlying tables when proposing rewrites/indexes.

// collectViewDefs returns the definitions of any referenced names that are
// views/materialized views. Empty (and no error) when none are views.
func collectViewDefs(ctx context.Context, conn *db.Conn, names []string) (TableReport, bool) {
	if len(names) == 0 {
		return TableReport{}, false
	}
	res, err := conn.Query(ctx, fmt.Sprintf(`SELECT
  c.relname AS view_name,
  CASE c.relkind WHEN 'm' THEN 'matview' ELSE 'view' END AS kind,
  LEFT(REGEXP_REPLACE(pg_get_viewdef(c.oid, true), E'\\s+', ' ', 'g'), 500) AS definition
FROM pg_class c
WHERE c.relname IN (%s) AND c.relkind IN ('v','m')
ORDER BY c.relname`, sqlInList(names)))
	if err != nil || len(res.Rows) == 0 {
		return TableReport{}, false
	}
	return tableReport("视图定义", conn.Label(),
		"以下引用对象其实是视图;优化器会自动展开为底表。改写/建索引时按底表考虑。", nil, res), true
}
