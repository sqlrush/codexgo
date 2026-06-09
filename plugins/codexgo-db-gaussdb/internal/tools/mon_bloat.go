package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /bloat — table bloat estimated from the dead-tuple ratio (same approximation
// as opendb; the classic relpages-based estimator is unreliable on openGauss's
// column stats, so we stay with the proven dead-tuple ratio). Optimization over
// opendb: a bloat-ratio bar and a reclaim recommendation.

const bloatSQL = `SELECT
  schemaname || '.' || relname AS table_name,
  n_live_tup, n_dead_tup,
  CASE WHEN n_live_tup > 0 THEN ROUND(n_dead_tup::numeric / n_live_tup * 100, 1) ELSE 0 END AS dead_pct,
  pg_size_pretty(pg_total_relation_size(schemaname || '.' || relname)) AS total_size
FROM pg_stat_user_tables
WHERE n_dead_tup > 0 AND n_live_tup > 0
  AND ROUND(n_dead_tup::numeric / n_live_tup * 100, 1) > %d
  AND ` + userSchemaPred + `
ORDER BY n_dead_tup DESC
LIMIT %d`

type bloatRow struct {
	Table, Live, Dead, Size string
	DeadPct                 float64
}

type bloatData struct {
	Target    string
	Threshold int
	Rows      []bloatRow
}

func registerBloat(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "bloat",
		Description: "Estimate GaussDB/openGauss table bloat from the dead-tuple ratio (pg_stat_user_tables), above a percentage threshold. Renders a bloat-ratio bar chart + table with total size directly to the user, plus reclaim guidance (VACUUM/rebuild). Optional args: threshold_pct (default 5), limit (default 30). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"threshold_pct": intProp("min dead-tuple percentage (default 5)"),
			"limit":         intProp("max rows (default 30)"),
		}),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			ThresholdPct int `json:"threshold_pct"`
			Limit        int `json:"limit"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		thr := a.ThresholdPct
		if thr <= 0 {
			thr = 5
		}
		d, err := collectBloat(ctx, conn, thr, clampLimit(a.Limit, 30, 100))
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderBloat(d), "user"),
			mcp.TextContentFor(monDigest("膨胀估算", len(d.Rows), "勿复述;可点评高膨胀表是否需 VACUUM FULL/重建"), "assistant"),
		}}, nil
	})
}

func collectBloat(ctx context.Context, conn *db.Conn, threshold, limit int) (*bloatData, error) {
	res, err := conn.Query(ctx, fmt.Sprintf(bloatSQL, threshold, limit))
	if err != nil {
		return nil, err
	}
	d := &bloatData{Target: conn.Label(), Threshold: threshold}
	for _, r := range res.Rows {
		if len(r) < 5 {
			continue
		}
		d.Rows = append(d.Rows, bloatRow{Table: r[0], Live: r[1], Dead: r[2], DeadPct: atof(r[3]), Size: r[4]})
	}
	return d, nil
}

func renderBloat(d *bloatData) string {
	var b strings.Builder
	b.WriteString("# 🎈 表膨胀估算 · " + d.Target + "\n\n")
	b.WriteString(fmt.Sprintf("> 基于死元组比例(近似);阈值 >%d%%。\n\n", d.Threshold))
	if len(d.Rows) == 0 {
		b.WriteString(fmt.Sprintf("✅ 无死元组比例 >%d%% 的表。\n", d.Threshold))
		return b.String()
	}

	b.WriteString("## 膨胀比 Top\n\n```\n")
	for i, r := range d.Rows {
		if i >= 8 {
			break
		}
		b.WriteString(barLine(truncDisp(r.Table, 32), fmt.Sprintf("%.1f%%", r.DeadPct), r.DeadPct/100, 16, 32, 7, r.Size) + "\n")
	}
	b.WriteString("```\n\n")

	b.WriteString("## 明细\n\n```\n")
	cols := []tableColumn{
		{Header: "表", Max: 34},
		{Header: "存活", Right: true},
		{Header: "死元组", Right: true},
		{Header: "死占比%", Right: true},
		{Header: "总大小", Right: true},
	}
	var rows [][]string
	for _, r := range d.Rows {
		rows = append(rows, []string{r.Table, r.Live, r.Dead, fmt.Sprintf("%.1f", r.DeadPct), r.Size})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n> 死占比高的表:常规 VACUUM 无法回收空间,需低峰 VACUUM FULL 或重建(会锁表);先排查 autovacuum/长事务。\n")
	return b.String()
}
