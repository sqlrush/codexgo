package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /space — space usage, merging opendb's space (database level) + segments
// (per-table table/index breakdown) + toasttable (large-object storage). One
// tool drills database → table → TOAST.

const spaceDBSQL = `SELECT
  datname,
  pg_database_size(datname) / 1048576 AS size_mb
FROM pg_database
WHERE datname NOT IN ('template0', 'template1')
ORDER BY pg_database_size(datname) DESC`

const spaceTableSQL = `SELECT
  schemaname || '.' || relname AS table_name,
  pg_size_pretty(pg_total_relation_size(schemaname || '.' || relname)) AS total,
  pg_size_pretty(pg_table_size(schemaname || '.' || relname))          AS tbl,
  pg_size_pretty(pg_indexes_size(schemaname || '.' || relname))        AS idx
FROM pg_stat_user_tables
WHERE ` + userSchemaPred + `
ORDER BY pg_total_relation_size(schemaname || '.' || relname) DESC
LIMIT %d`

const spaceToastSQL = `SELECT
  n.nspname || '.' || c.relname AS table_name,
  pg_size_pretty(pg_relation_size(c.reltoastrelid)) AS toast_size,
  pg_size_pretty(pg_total_relation_size(c.oid))     AS total_size,
  CASE WHEN pg_total_relation_size(c.oid) > 0
       THEN ROUND(100.0 * pg_relation_size(c.reltoastrelid) / pg_total_relation_size(c.oid), 1)
       ELSE 0 END AS toast_pct
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r' AND c.reltoastrelid <> 0
  AND n.nspname NOT IN ('pg_catalog','information_schema','pg_toast')
ORDER BY pg_relation_size(c.reltoastrelid) DESC
LIMIT %d`

type spaceDBRow struct {
	Name   string
	SizeMB float64
}
type spaceTableRow struct{ Table, Total, Tbl, Idx string }
type spaceToastRow struct{ Table, Toast, Total, Pct string }

type spaceData struct {
	Target   string
	DBs      []spaceDBRow
	Tables   []spaceTableRow
	Toasts   []spaceToastRow
	Warnings []string
}

func registerSpace(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "space",
		Description: "Show GaussDB/openGauss space usage: per-database size, the largest tables (table vs index breakdown), and the largest TOAST (out-of-line large object) tables. Renders size bars + tables directly to the user. Optional arg limit (default 20 per section). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{"limit": intProp("max rows per section (default 20)")}),
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
		d := collectSpace(ctx, conn, clampLimit(a.Limit, 20, 100))
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderSpace(d), "user"),
			mcp.TextContentFor(monDigest("空间使用", len(d.Tables), "勿复述;可点评大表/大TOAST是否需归档或清理"), "assistant"),
		}}, nil
	})
}

func collectSpace(ctx context.Context, conn *db.Conn, limit int) *spaceData {
	d := &spaceData{Target: conn.Label()}
	if res, err := conn.Query(ctx, spaceDBSQL); err != nil {
		d.Warnings = append(d.Warnings, "库级空间采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 2 {
				continue
			}
			d.DBs = append(d.DBs, spaceDBRow{Name: r[0], SizeMB: atof(r[1])})
		}
	}
	if res, err := conn.Query(ctx, fmt.Sprintf(spaceTableSQL, limit)); err != nil {
		d.Warnings = append(d.Warnings, "表级空间采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 4 {
				continue
			}
			d.Tables = append(d.Tables, spaceTableRow{Table: r[0], Total: r[1], Tbl: r[2], Idx: r[3]})
		}
	}
	if res, err := conn.Query(ctx, fmt.Sprintf(spaceToastSQL, limit)); err != nil {
		d.Warnings = append(d.Warnings, "TOAST 采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 4 {
				continue
			}
			d.Toasts = append(d.Toasts, spaceToastRow{Table: r[0], Toast: r[1], Total: r[2], Pct: r[3]})
		}
	}
	return d
}

func renderSpace(d *spaceData) string {
	var b strings.Builder
	b.WriteString("# 💾 空间使用 · " + d.Target + "\n\n")

	if len(d.DBs) > 0 {
		b.WriteString("## 数据库大小\n\n```\n")
		max := d.DBs[0].SizeMB
		if max <= 0 {
			max = 1
		}
		for _, r := range d.DBs {
			b.WriteString(barLine(truncDisp(r.Name, 18), prettyMB(r.SizeMB), r.SizeMB/max, 18, 18, 10, "") + "\n")
		}
		b.WriteString("```\n\n")
	}

	if len(d.Tables) > 0 {
		b.WriteString("## 大表 Top(总=表+索引)\n\n```\n")
		cols := []tableColumn{
			{Header: "表", Max: 40},
			{Header: "总大小", Right: true},
			{Header: "表", Right: true},
			{Header: "索引", Right: true},
		}
		var rows [][]string
		for _, r := range d.Tables {
			rows = append(rows, []string{r.Table, r.Total, r.Tbl, r.Idx})
		}
		b.WriteString(asciiTable(cols, rows))
		b.WriteString("```\n\n")
	}

	if len(d.Toasts) > 0 {
		b.WriteString("## 大 TOAST Top(大字段外存)\n\n```\n")
		cols := []tableColumn{
			{Header: "表", Max: 40},
			{Header: "TOAST", Right: true},
			{Header: "总大小", Right: true},
			{Header: "TOAST占比%", Right: true},
		}
		var rows [][]string
		for _, r := range d.Toasts {
			rows = append(rows, []string{r.Table, r.Toast, r.Total, r.Pct})
		}
		b.WriteString(asciiTable(cols, rows))
		b.WriteString("```\n\n")
	}

	if len(d.Warnings) > 0 {
		b.WriteString("> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}

// prettyMB renders a size given in MB as MB/GB/TB.
func prettyMB(mb float64) string {
	switch {
	case mb >= 1024*1024:
		return fmt.Sprintf("%.1f TB", mb/1024/1024)
	case mb >= 1024:
		return fmt.Sprintf("%.1f GB", mb/1024)
	default:
		return fmt.Sprintf("%.0f MB", mb)
	}
}
