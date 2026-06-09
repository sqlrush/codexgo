package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /tableinfo — full structure of a table: columns, indexes, constraints, and
// size/stats. Input is schema.table (or just table → public). Identifiers are
// validated before interpolation (the connection uses simple_protocol, so bound
// params aren't available for the regclass literals).

type tableInfoData struct {
	Schema, Table string
	Target        string
	Columns       [][]string
	Indexes       [][]string
	Constraints   [][]string
	Stats         []kv
	Warnings      []string
}

func registerTableInfo(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "tableinfo",
		Description: "Show the full structure of a GaussDB/openGauss table — columns (type/nullable/default), indexes (with definitions + size), constraints, and size/row/analyze stats. Renders multiple sections directly to the user. Arg table required: 'schema.table' (or just 'table' for public). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"table": strProp("the table as schema.table (or just table for the public schema)"),
		}, "table"),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			Table string `json:"table"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		schema, table, err := parseQualified(a.Table)
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectTableInfo(ctx, conn, schema, table)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderTableInfo(d), "user"),
			mcp.TextContentFor(monDigest("表结构 "+schema+"."+table, len(d.Columns), "勿复述;可点评索引/约束/统计是否合理"), "assistant"),
		}}, nil
	})
}

func collectTableInfo(ctx context.Context, conn *db.Conn, schema, table string) *tableInfoData {
	d := &tableInfoData{Schema: schema, Table: table, Target: conn.Label()}
	qn := schema + "." + table

	if res, err := conn.Query(ctx, fmt.Sprintf(`SELECT a.attname,
  pg_catalog.format_type(a.atttypid, a.atttypmod),
  CASE WHEN a.attnotnull THEN 'NOT NULL' ELSE '' END,
  COALESCE(pg_get_expr(d.adbin, d.adrelid), '')
FROM pg_catalog.pg_attribute a
LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE a.attrelid = '%s'::regclass AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`, qn)); err != nil {
		d.Warnings = append(d.Warnings, "列采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) >= 4 {
				d.Columns = append(d.Columns, []string{r[0], r[1], r[2], r[3]})
			}
		}
	}

	if res, err := conn.Query(ctx, fmt.Sprintf(`SELECT i.relname,
  CASE WHEN ix.indisprimary THEN 'PK' WHEN ix.indisunique THEN 'UNIQUE' ELSE '' END,
  pg_get_indexdef(ix.indexrelid),
  ROUND(pg_relation_size(i.oid)/1048576.0, 1) AS size_mb
FROM pg_index ix
JOIN pg_class i ON i.oid = ix.indexrelid
WHERE ix.indrelid = '%s'::regclass
ORDER BY ix.indisprimary DESC, i.relname`, qn)); err == nil {
		for _, r := range res.Rows {
			if len(r) >= 4 {
				d.Indexes = append(d.Indexes, []string{r[0], r[1], r[3] + " MB", r[2]})
			}
		}
	}

	if res, err := conn.Query(ctx, fmt.Sprintf(`SELECT conname,
  CASE contype WHEN 'p' THEN 'PRIMARY' WHEN 'f' THEN 'FOREIGN' WHEN 'u' THEN 'UNIQUE' WHEN 'c' THEN 'CHECK' ELSE contype::text END,
  pg_get_constraintdef(oid)
FROM pg_constraint WHERE conrelid = '%s'::regclass
ORDER BY contype, conname`, qn)); err == nil {
		for _, r := range res.Rows {
			if len(r) >= 3 {
				d.Constraints = append(d.Constraints, []string{r[0], r[1], r[2]})
			}
		}
	}

	if res, err := conn.Query(ctx, fmt.Sprintf(`SELECT
  pg_size_pretty(pg_total_relation_size('%s'::regclass)),
  pg_size_pretty(pg_table_size('%s'::regclass)),
  pg_size_pretty(pg_indexes_size('%s'::regclass)),
  COALESCE((SELECT n_live_tup::text FROM pg_stat_all_tables WHERE schemaname='%s' AND relname='%s'),'-'),
  COALESCE((SELECT n_dead_tup::text FROM pg_stat_all_tables WHERE schemaname='%s' AND relname='%s'),'-'),
  COALESCE((SELECT last_analyze::text FROM pg_stat_all_tables WHERE schemaname='%s' AND relname='%s'),'-')`,
		qn, qn, qn, schema, table, schema, table, schema, table)); err == nil && len(res.Rows) > 0 {
		r := res.Rows[0]
		if len(r) >= 6 {
			d.Stats = []kv{
				{"总大小", r[0]}, {"表大小", r[1]}, {"索引大小", r[2]},
				{"存活行", r[3]}, {"死元组", r[4]}, {"末次analyze", r[5]},
			}
		}
	}
	return d
}

func renderTableInfo(d *tableInfoData) string {
	var b strings.Builder
	b.WriteString("# 📋 表结构 · " + d.Schema + "." + d.Table + "\n\n")

	if len(d.Stats) > 0 {
		b.WriteString("## 大小 / 统计\n\n```\n")
		b.WriteString(kvBlock(d.Stats))
		b.WriteString("```\n\n")
	}

	b.WriteString("## 列\n\n```\n")
	if len(d.Columns) == 0 {
		b.WriteString("未找到列(表不存在或无权限)。\n")
	} else {
		cols := []tableColumn{
			{Header: "列名", Max: 28},
			{Header: "类型", Max: 28},
			{Header: "非空"},
			{Header: "默认值", Max: 30},
		}
		b.WriteString(asciiTable(cols, d.Columns))
	}
	b.WriteString("```\n\n")

	if len(d.Indexes) > 0 {
		b.WriteString("## 索引\n\n```\n")
		cols := []tableColumn{
			{Header: "索引名", Max: 28},
			{Header: "类型"},
			{Header: "大小", Right: true},
			{Header: "定义", Max: 50},
		}
		b.WriteString(asciiTable(cols, d.Indexes))
		b.WriteString("```\n\n")
	}

	if len(d.Constraints) > 0 {
		b.WriteString("## 约束\n\n```\n")
		cols := []tableColumn{
			{Header: "名称", Max: 26},
			{Header: "类型"},
			{Header: "定义", Max: 50},
		}
		b.WriteString(asciiTable(cols, d.Constraints))
		b.WriteString("```\n\n")
	}

	if len(d.Warnings) > 0 {
		b.WriteString("> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}

// parseQualified splits "schema.table" (or "table" → public) and validates both
// identifiers (the names are interpolated into ::regclass literals).
func parseQualified(input string) (string, string, error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return "", "", fmt.Errorf("table is required (schema.table or table)")
	}
	schema, table := "public", in
	if i := strings.IndexByte(in, '.'); i >= 0 {
		schema, table = in[:i], in[i+1:]
	}
	if !isIdent(schema) || !isIdent(table) {
		return "", "", fmt.Errorf("invalid table name %q — use plain schema.table identifiers", input)
	}
	return schema, table, nil
}

// isIdent reports whether s is a safe SQL identifier (letters/digits/_/$).
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c == '_' || c == '$' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}
