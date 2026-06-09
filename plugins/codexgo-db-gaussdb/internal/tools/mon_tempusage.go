package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /tempusage — temp-file (on-disk spill) usage by database, plus the current
// work_mem. Temp spill means a sort/hash exceeded work_mem and went to disk.
// The per-session probe (pg_stat_activity.temp_files) is best-effort — that
// column is not present on every openGauss build, so its failure is a warning,
// not an error.

const tempByDBSQL = `SELECT
  datname,
  temp_files,
  pg_size_pretty(temp_bytes) AS temp_bytes_pretty,
  temp_bytes
FROM pg_stat_database
WHERE datname IS NOT NULL AND temp_files > 0
ORDER BY temp_bytes DESC
LIMIT 15`

const tempBySessionSQL = `SELECT
  pid, usename, datname, state,
  LEFT(regexp_replace(COALESCE(query,''), E'\\s+', ' ', 'g'), 60) AS query,
  temp_files
FROM pg_stat_activity
WHERE temp_files > 0
ORDER BY temp_files DESC
LIMIT 10`

type tempDBRow struct{ DB, Files, Pretty string }
type tempSessRow struct{ PID, User, DB, State, Query, Files string }

type tempData struct {
	Target   string
	DBs      []tempDBRow
	Sessions []tempSessRow
	WorkMem  string
	Warnings []string
}

func registerTempUsage(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "tempusage",
		Description: "Show GaussDB/openGauss temp-file (on-disk spill) usage by database (pg_stat_database temp_files/temp_bytes) plus the current work_mem, and best-effort active spilling sessions. Temp spill means sorts/hashes exceeded work_mem. Renders tables directly to the user. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectTempUsage(ctx, conn)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderTempUsage(d), "user"),
			mcp.TextContentFor(monDigest("临时文件", len(d.DBs), "勿复述;若 spill 多,提示调大 work_mem 或优化排序/哈希"), "assistant"),
		}}, nil
	})
}

func collectTempUsage(ctx context.Context, conn *db.Conn) *tempData {
	d := &tempData{Target: conn.Label()}
	if v, err := conn.QueryScalar(ctx, "SELECT setting || COALESCE(unit,'') FROM pg_settings WHERE name='work_mem'"); err == nil {
		d.WorkMem = v
	}
	if res, err := conn.Query(ctx, tempByDBSQL); err != nil {
		d.Warnings = append(d.Warnings, "库级临时文件采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 3 {
				continue
			}
			d.DBs = append(d.DBs, tempDBRow{DB: r[0], Files: r[1], Pretty: r[2]})
		}
	}
	// best-effort: pg_stat_activity.temp_files may not exist on this build.
	if res, err := conn.Query(ctx, tempBySessionSQL); err == nil {
		for _, r := range res.Rows {
			if len(r) < 6 {
				continue
			}
			d.Sessions = append(d.Sessions, tempSessRow{PID: r[0], User: nz(r[1]), DB: nz(r[2]), State: nz(r[3]), Query: r[4], Files: r[5]})
		}
	}
	return d
}

func renderTempUsage(d *tempData) string {
	var b strings.Builder
	b.WriteString("# 🗄️ 临时文件 / 落盘 · " + d.Target + "\n\n")
	if d.WorkMem != "" {
		b.WriteString("> 当前 work_mem = " + d.WorkMem + "(单条排序/哈希超过即落盘)。\n\n")
	}

	b.WriteString("## 按数据库(累计)\n\n```\n")
	if len(d.DBs) == 0 {
		b.WriteString("无临时文件累计(或统计已重置)。\n")
	} else {
		cols := []tableColumn{
			{Header: "数据库", Max: 20},
			{Header: "临时文件数", Right: true},
			{Header: "临时字节", Right: true},
		}
		var rows [][]string
		for _, r := range d.DBs {
			rows = append(rows, []string{r.DB, r.Files, r.Pretty})
		}
		b.WriteString(asciiTable(cols, rows))
	}
	b.WriteString("```\n\n")

	if len(d.Sessions) > 0 {
		b.WriteString("## 当前落盘会话\n\n```\n")
		cols := []tableColumn{
			{Header: "PID", Max: 18},
			{Header: "用户", Max: 12},
			{Header: "库", Max: 10},
			{Header: "临时文件", Right: true},
			{Header: "SQL", Max: 40},
		}
		var rows [][]string
		for _, r := range d.Sessions {
			rows = append(rows, []string{r.PID, r.User, r.DB, r.Files, r.Query})
		}
		b.WriteString(asciiTable(cols, rows))
		b.WriteString("```\n\n")
	}

	b.WriteString("> 临时文件多 = 排序/哈希溢出磁盘:适度调大 work_mem,或为大排序/连接补索引、缩小结果集。\n")
	if len(d.Warnings) > 0 {
		b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}
