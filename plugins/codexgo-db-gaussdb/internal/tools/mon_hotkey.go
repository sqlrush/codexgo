package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /hotkey — hottest tables ranked by combined read/write activity, with access
// pattern flags. Optimization over opendb: a real activity-score ORDER BY (not
// just LIMIT) and an activity bar chart.

const hotkeySQL = `SELECT
  schemaname || '.' || relname AS table_name,
  seq_scan, idx_scan, n_tup_ins, n_tup_upd, n_tup_del,
  (COALESCE(seq_scan,0)+COALESCE(idx_scan,0)+COALESCE(n_tup_ins,0)+COALESCE(n_tup_upd,0)+COALESCE(n_tup_del,0)) AS activity,
  CASE WHEN seq_scan > 0 AND idx_scan = 0 THEN 'seq only'
       WHEN seq_scan > idx_scan * 3      THEN 'seq heavy'
       WHEN n_tup_upd > n_tup_ins * 5    THEN 'update heavy'
       ELSE '-'
  END AS flag
FROM pg_stat_all_tables
WHERE ` + userSchemaPred + `
ORDER BY activity DESC
LIMIT %d`

type hotkeyRow struct {
	Table, SeqScan, IdxScan, Ins, Upd, Del, Flag string
	Activity                                     int
}

type hotkeyData struct {
	Target string
	Rows   []hotkeyRow
}

func registerHotKey(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "hotkey",
		Description: "Identify hotspot GaussDB/openGauss tables ranked by combined read/write activity (seq+idx scans + ins/upd/del), flagging seq-heavy / update-heavy access patterns (pg_stat_all_tables). Renders an activity bar chart + table directly to the user. Optional arg limit (default 20). Read-only.",
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
		d, err := collectHotKey(ctx, conn, clampLimit(a.Limit, 20, 100))
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderHotKey(d), "user"),
			mcp.TextContentFor(monDigest("热点表", len(d.Rows), "勿复述;可点评 seq-heavy 表是否缺索引"), "assistant"),
		}}, nil
	})
}

func collectHotKey(ctx context.Context, conn *db.Conn, limit int) (*hotkeyData, error) {
	res, err := conn.Query(ctx, fmt.Sprintf(hotkeySQL, limit))
	if err != nil {
		return nil, err
	}
	d := &hotkeyData{Target: conn.Label()}
	for _, r := range res.Rows {
		if len(r) < 8 {
			continue
		}
		d.Rows = append(d.Rows, hotkeyRow{
			Table: r[0], SeqScan: r[1], IdxScan: r[2], Ins: r[3], Upd: r[4], Del: r[5],
			Activity: int(atof(r[6])), Flag: r[7],
		})
	}
	return d, nil
}

func renderHotKey(d *hotkeyData) string {
	var b strings.Builder
	b.WriteString("# 🔥 热点表 · " + d.Target + "\n\n")
	if len(d.Rows) == 0 {
		b.WriteString("无活动统计数据(可能统计已重置)。\n")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("> 按总活动量(扫描+增删改)排序的 Top %d 表。\n\n", len(d.Rows)))

	b.WriteString("## 活动量 Top\n\n```\n")
	max := d.Rows[0].Activity
	if max <= 0 {
		max = 1
	}
	for i, r := range d.Rows {
		if i >= 8 {
			break
		}
		b.WriteString(barLine(truncDisp(r.Table, 32), fmt.Sprintf("%d", r.Activity), float64(r.Activity)/float64(max), 16, 32, 10, r.Flag) + "\n")
	}
	b.WriteString("```\n\n")

	b.WriteString("## 明细\n\n```\n")
	cols := []tableColumn{
		{Header: "表", Max: 34},
		{Header: "顺序扫描", Right: true},
		{Header: "索引扫描", Right: true},
		{Header: "插入", Right: true},
		{Header: "更新", Right: true},
		{Header: "删除", Right: true},
		{Header: "模式"},
	}
	var rows [][]string
	for _, r := range d.Rows {
		rows = append(rows, []string{r.Table, r.SeqScan, r.IdxScan, r.Ins, r.Upd, r.Del, r.Flag})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n> seq heavy / seq only 的表若伴随大表,优先评估索引;update heavy 关注 HOT 与膨胀。\n")
	return b.String()
}

// monDigest builds a terse assistant digest for a list-style monitoring tool.
func monDigest(name string, n int, hint string) string {
	if n == 0 {
		return name + "已渲染给用户:无数据。可一句话确认。"
	}
	return fmt.Sprintf("%s已渲染给用户:%d 行。%s。", name, n, hint)
}
