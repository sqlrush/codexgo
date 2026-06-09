package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /vacuum — dead-tuple accumulation + autovacuum status, merging opendb's vacuum
// + autovacuum. Optimization over opendb: computes each table's autovacuum
// trigger point (threshold + scale_factor × tuples) and flags tables that are
// OVER threshold but not recently autovacuumed, plus shows in-progress workers.

const vacuumMainSQL = `SELECT
  schemaname || '.' || relname AS table_name,
  n_live_tup, n_dead_tup,
  CASE WHEN n_live_tup > 0 THEN ROUND(n_dead_tup::numeric / n_live_tup * 100, 1) ELSE 0 END AS dead_pct,
  COALESCE(last_autovacuum::text, '') AS last_autovac,
  COALESCE(autovacuum_count, 0) AS autovac_count
FROM pg_stat_user_tables
WHERE n_dead_tup > 0 AND ` + userSchemaPred + `
ORDER BY n_dead_tup DESC
LIMIT %d`

const vacuumInProgressSQL = `SELECT
  pid,
  EXTRACT(EPOCH FROM now() - query_start)::int AS elapsed_sec,
  LEFT(regexp_replace(COALESCE(query,''), E'\\s+', ' ', 'g'), 70) AS query
FROM pg_stat_activity
WHERE pid <> pg_backend_pid()
  AND (application_name = 'AutoVacuum' OR query ILIKE '%vacuum%')
  AND query NOT ILIKE '%pg_stat%'
ORDER BY query_start`

type vacuumRow struct {
	Table, LastAutovac, AutovacCount string
	Live, Dead                       float64
	DeadPct                          float64
	OverThreshold                    bool
}

type vacuumWorker struct{ PID, Elapsed, Query string }

type vacuumData struct {
	Target      string
	Enabled     string
	Threshold   float64
	ScaleFactor float64
	Rows        []vacuumRow
	Workers     []vacuumWorker
	OverCount   int
	Warnings    []string
}

func registerVacuum(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "vacuum",
		Description: "Show GaussDB/openGauss dead-tuple accumulation and autovacuum status (pg_stat_user_tables). Computes each table's autovacuum trigger point (threshold + scale_factor × tuples) and flags tables OVER threshold, shows in-progress vacuum workers, and the autovacuum on/off setting. Renders a dead-tuple bar + table directly to the user. Optional arg limit (default 30). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{"limit": intProp("max rows (default 30)")}),
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
		d := collectVacuum(ctx, conn, clampLimit(a.Limit, 30, 100))
		hint := "勿复述"
		if d.OverCount > 0 {
			hint = fmt.Sprintf("勿复述;%d 个表已超 autovacuum 阈值,提示检查 autovacuum 是否生效/被长事务阻塞", d.OverCount)
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderVacuum(d), "user"),
			mcp.TextContentFor(monDigest("Vacuum/死元组", len(d.Rows), hint), "assistant"),
		}}, nil
	})
}

func collectVacuum(ctx context.Context, conn *db.Conn, limit int) *vacuumData {
	d := &vacuumData{Target: conn.Label(), Enabled: "?", Threshold: 50, ScaleFactor: 0.2}
	// autovacuum GUCs (best-effort; fall back to PG defaults).
	if res, err := conn.Query(ctx, "SELECT name, setting FROM pg_settings WHERE name IN ('autovacuum','autovacuum_vacuum_threshold','autovacuum_vacuum_scale_factor')"); err == nil {
		for _, r := range res.Rows {
			if len(r) < 2 {
				continue
			}
			switch r[0] {
			case "autovacuum":
				d.Enabled = r[1]
			case "autovacuum_vacuum_threshold":
				d.Threshold = atof(r[1])
			case "autovacuum_vacuum_scale_factor":
				d.ScaleFactor = atof(r[1])
			}
		}
	}
	if res, err := conn.Query(ctx, fmt.Sprintf(vacuumMainSQL, limit)); err != nil {
		d.Warnings = append(d.Warnings, "死元组采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 6 {
				continue
			}
			row := vacuumRow{
				Table: r[0], Live: atof(r[1]), Dead: atof(r[2]), DeadPct: atof(r[3]),
				LastAutovac: r[4], AutovacCount: r[5],
			}
			trigger := d.Threshold + d.ScaleFactor*(row.Live+row.Dead)
			row.OverThreshold = row.Dead >= trigger
			if row.OverThreshold {
				d.OverCount++
			}
			d.Rows = append(d.Rows, row)
		}
	}
	// in-progress workers (best-effort).
	if res, err := conn.Query(ctx, vacuumInProgressSQL); err == nil {
		for _, r := range res.Rows {
			if len(r) < 3 {
				continue
			}
			d.Workers = append(d.Workers, vacuumWorker{PID: r[0], Elapsed: r[1], Query: r[2]})
		}
	}
	return d
}

func renderVacuum(d *vacuumData) string {
	var b strings.Builder
	b.WriteString("# 🧹 Vacuum / 死元组 · " + d.Target + "\n\n")
	b.WriteString(fmt.Sprintf("> autovacuum=%s · 触发阈值 = %.0f + %.2f×表行数。", d.Enabled, d.Threshold, d.ScaleFactor))
	if d.OverCount > 0 {
		b.WriteString(fmt.Sprintf(" ⚠️ %d 个表已超阈。", d.OverCount))
	}
	b.WriteString("\n\n")

	if len(d.Workers) > 0 {
		b.WriteString("## 进行中的 vacuum\n\n```\n")
		cols := []tableColumn{
			{Header: "PID", Max: 18},
			{Header: "已运行", Right: true},
			{Header: "语句", Max: 50},
		}
		var rows [][]string
		for _, w := range d.Workers {
			rows = append(rows, []string{w.PID, humanSecs(atof(w.Elapsed)), w.Query})
		}
		b.WriteString(asciiTable(cols, rows))
		b.WriteString("```\n\n")
	}

	b.WriteString("## 死元组 Top\n\n")
	if len(d.Rows) == 0 {
		b.WriteString("✅ 无死元组堆积。\n")
		if len(d.Warnings) > 0 {
			b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
		}
		return b.String()
	}

	b.WriteString("```\n")
	for i, r := range d.Rows {
		if i >= 8 {
			break
		}
		b.WriteString(barLine(truncDisp(r.Table, 32), fmt.Sprintf("%.1f%%", r.DeadPct), r.DeadPct/100, 16, 32, 7, fmt.Sprintf("%.0f死", r.Dead)) + "\n")
	}
	b.WriteString("```\n\n```\n")
	cols := []tableColumn{
		{Header: "表", Max: 34},
		{Header: "死元组", Right: true},
		{Header: "存活", Right: true},
		{Header: "死占比%", Right: true},
		{Header: "超阈"},
		{Header: "末次autovac", Max: 19},
	}
	var rows [][]string
	for _, r := range d.Rows {
		over := "-"
		if r.OverThreshold {
			over = "是"
		}
		last := r.LastAutovac
		if last == "" {
			last = "从未"
		}
		rows = append(rows, []string{r.Table, fmt.Sprintf("%.0f", r.Dead), fmt.Sprintf("%.0f", r.Live), fmt.Sprintf("%.1f", r.DeadPct), over, last})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n> “超阈=是”但末次 autovacuum 为空/陈旧:检查 autovacuum 是否开启、是否被长事务(/longtx)阻塞,必要时手动 VACUUM。\n")
	if len(d.Warnings) > 0 {
		b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}
