package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /sqlcount — per-user SQL statement-type counts (gs_sql_count, openGauss-
// specific): SELECT/INSERT/UPDATE/DELETE/DDL/DML/DCL plus avg/max SELECT
// latency. Useful for workload characterization.

const sqlCountSQL = `SELECT user_name,
  select_count, update_count, insert_count, delete_count,
  ddl_count, dml_count, dcl_count,
  select_count + update_count + insert_count + delete_count AS total_dml,
  ROUND(avg_select_elapse/1000.0, 2) AS avg_sel_ms,
  ROUND(max_select_elapse/1000.0, 2) AS max_sel_ms
FROM gs_sql_count
ORDER BY select_count + update_count + insert_count + delete_count DESC
LIMIT %d`

type sqlCountRow struct {
	User, Sel, Upd, Ins, Del, DDL, DML, DCL, AvgSelMS, MaxSelMS string
	Total                                                       int
}

type sqlCountData struct {
	Target string
	Rows   []sqlCountRow
}

func registerSQLCount(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "sqlcount",
		Description: "Show per-user SQL statement-type counts (gs_sql_count, openGauss-specific): SELECT/INSERT/UPDATE/DELETE/DDL/DML/DCL with avg/max SELECT latency, for workload characterization. Renders a table directly to the user. Optional arg limit (default 20). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{"limit": intProp("max rows (default 20)")}),
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
		d, err := collectSQLCount(ctx, conn, clampLimit(a.Limit, 20, 100))
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderSQLCount(d), "user"),
			mcp.TextContentFor(monDigest("SQL 计数", len(d.Rows), "勿复述;可点评读写比与 DDL/DCL 占比"), "assistant"),
		}}, nil
	})
}

func collectSQLCount(ctx context.Context, conn *db.Conn, limit int) (*sqlCountData, error) {
	res, err := conn.Query(ctx, fmt.Sprintf(sqlCountSQL, limit))
	if err != nil {
		return nil, err
	}
	d := &sqlCountData{Target: conn.Label()}
	for _, r := range res.Rows {
		if len(r) < 11 {
			continue
		}
		d.Rows = append(d.Rows, sqlCountRow{
			User: r[0], Sel: r[1], Upd: r[2], Ins: r[3], Del: r[4],
			DDL: r[5], DML: r[6], DCL: r[7], Total: int(atof(r[8])),
			AvgSelMS: r[9], MaxSelMS: r[10],
		})
	}
	return d, nil
}

func renderSQLCount(d *sqlCountData) string {
	var b strings.Builder
	b.WriteString("# 🔢 SQL 类型计数 · " + d.Target + "\n\n")
	if len(d.Rows) == 0 {
		b.WriteString("无 gs_sql_count 数据(需开启 SQL 统计)。\n")
		return b.String()
	}
	b.WriteString("> 按用户累计的语句类型计数;延迟单位 ms。\n\n```\n")
	cols := []tableColumn{
		{Header: "用户", Max: 16},
		{Header: "SELECT", Right: true},
		{Header: "INSERT", Right: true},
		{Header: "UPDATE", Right: true},
		{Header: "DELETE", Right: true},
		{Header: "DDL", Right: true},
		{Header: "DCL", Right: true},
		{Header: "平均SEL", Right: true},
		{Header: "最大SEL", Right: true},
	}
	var rows [][]string
	for _, r := range d.Rows {
		rows = append(rows, []string{r.User, r.Sel, r.Ins, r.Upd, r.Del, r.DDL, r.DCL, r.AvgSelMS, r.MaxSelMS})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n")
	return b.String()
}
