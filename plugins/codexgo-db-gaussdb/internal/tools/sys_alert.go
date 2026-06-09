package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /alert — per-database anomaly counters (deadlocks, conflicts, temp-file
// spill) from pg_stat_database. Only databases with a non-zero counter are
// listed, ordered by severity.

const alertSQL = `SELECT
  datname,
  COALESCE(conflicts,0) AS conflicts,
  COALESCE(deadlocks,0) AS deadlocks,
  COALESCE(temp_files,0) AS temp_files,
  pg_size_pretty(COALESCE(temp_bytes,0)) AS temp_pretty
FROM pg_stat_database
WHERE datname NOT IN ('template0','template1')
  AND (conflicts > 0 OR deadlocks > 0 OR temp_files > 0)
ORDER BY deadlocks DESC, conflicts DESC, temp_files DESC`

type alertRow struct {
	DB, Conflicts, Deadlocks, TempFiles, TempPretty string
	Sev                                             string
}

type alertData struct {
	Target   string
	Rows     []alertRow
	Warnings []string
}

func registerAlert(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "alert",
		Description: "Show GaussDB/openGauss per-database anomaly counters — deadlocks, recovery conflicts, and temp-file spill (pg_stat_database). Only databases with non-zero counters are listed, severity-graded. Renders a table directly to the user. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectAlert(ctx, conn)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderAlert(d), "user"),
			mcp.TextContentFor(monDigest("数据库告警", len(d.Rows), "勿复述;死锁/冲突需重点排查"), "assistant"),
		}}, nil
	})
}

func collectAlert(ctx context.Context, conn *db.Conn) *alertData {
	d := &alertData{Target: conn.Label()}
	res, err := conn.Query(ctx, alertSQL)
	if err != nil {
		d.Warnings = append(d.Warnings, "告警采集失败: "+firstLine(err.Error()))
		return d
	}
	for _, r := range res.Rows {
		if len(r) < 5 {
			continue
		}
		sev := statusWarn
		if atof(r[2]) > 0 { // deadlocks present → serious
			sev = statusFail
		}
		d.Rows = append(d.Rows, alertRow{DB: r[0], Conflicts: r[1], Deadlocks: r[2], TempFiles: r[3], TempPretty: r[4], Sev: sev})
	}
	return d
}

func renderAlert(d *alertData) string {
	var b strings.Builder
	b.WriteString("# 🚨 数据库告警 · " + d.Target + "\n\n")
	if len(d.Rows) == 0 {
		b.WriteString("✅ 各库无死锁 / 冲突 / 临时文件累计异常。\n")
		if len(d.Warnings) > 0 {
			b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
		}
		return b.String()
	}
	b.WriteString("> 仅列出有异常计数的库(累计值);死锁优先级最高。\n\n```\n")
	cols := []tableColumn{
		{Header: "级别"},
		{Header: "数据库", Max: 20},
		{Header: "死锁", Right: true},
		{Header: "冲突", Right: true},
		{Header: "临时文件", Right: true},
		{Header: "临时大小", Right: true},
	}
	var rows [][]string
	for _, r := range d.Rows {
		rows = append(rows, []string{sevText(r.Sev), r.DB, r.Deadlocks, r.Conflicts, r.TempFiles, r.TempPretty})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n> 死锁:排查事务加锁顺序;冲突:多见于备库回放;临时文件多:见 /tempusage 调 work_mem。\n")
	if len(d.Warnings) > 0 {
		b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}
