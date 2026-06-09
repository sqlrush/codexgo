package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /xid — transaction-ID age and wraparound risk. Optimization over opendb (which
// only checks per-database): adds the Top high-age tables (per-relation
// relfrozenxid) and a wraparound-risk bar (age / 2^31).

const xidDBSQL = `SELECT
  datname,
  datfrozenxid::text AS frozen_xid,
  txid_current() - datfrozenxid::text::bigint AS xid_age
FROM pg_database
WHERE datname NOT IN ('template0')
ORDER BY txid_current() - datfrozenxid::text::bigint DESC`

// openGauss relfrozenxid is xid32 (age() rejects it), so compute age the same
// way the per-database query does: txid_current() minus the frozen xid as bigint.
const xidTableSQL = `SELECT
  n.nspname || '.' || c.relname AS table_name,
  txid_current() - c.relfrozenxid::text::bigint AS xid_age
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r','t','m')
  AND c.relfrozenxid::text::bigint > 0
  AND n.nspname NOT IN ('pg_catalog','information_schema','snapshot','dbe_perf','db4ai','gs_logical_cluster','sqladvisor')
ORDER BY txid_current() - c.relfrozenxid::text::bigint DESC
LIMIT %d`

type xidRow struct {
	Name string
	Age  float64
}

type xidData struct {
	Target   string
	DBs      []xidRow
	Tables   []xidRow
	Warnings []string
}

func registerXID(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "xid",
		Description: "Show GaussDB/openGauss transaction-ID (XID) age and wraparound risk per database (pg_database datfrozenxid) plus the Top high-age tables (pg_class relfrozenxid). Renders a wraparound-risk bar + tables directly to the user. Optional arg limit (default 10 tables). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{"limit": intProp("max high-age tables (default 10)")}),
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
		d := collectXID(ctx, conn, clampLimit(a.Limit, 10, 50))
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderXID(d), "user"),
			mcp.TextContentFor(monDigest("XID/回卷", len(d.DBs), "勿复述;若风险条接近满,提示对高龄表 VACUUM FREEZE"), "assistant"),
		}}, nil
	})
}

func collectXID(ctx context.Context, conn *db.Conn, limit int) *xidData {
	d := &xidData{Target: conn.Label()}
	if res, err := conn.Query(ctx, xidDBSQL); err != nil {
		d.Warnings = append(d.Warnings, "库级 XID 采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 3 {
				continue
			}
			d.DBs = append(d.DBs, xidRow{Name: r[0], Age: atof(r[2])})
		}
	}
	if res, err := conn.Query(ctx, fmt.Sprintf(xidTableSQL, limit)); err != nil {
		d.Warnings = append(d.Warnings, "表级 XID 采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 2 {
				continue
			}
			d.Tables = append(d.Tables, xidRow{Name: r[0], Age: atof(r[1])})
		}
	}
	return d
}

func renderXID(d *xidData) string {
	var b strings.Builder
	b.WriteString("# 🧊 事务ID年龄 / 回卷风险 · " + d.Target + "\n\n")
	b.WriteString("> 风险 = XID 年龄 / 2³¹(约 21.4 亿);接近满则需尽快 FREEZE。\n\n")

	if len(d.DBs) > 0 {
		b.WriteString("## 数据库\n\n```\n")
		for _, r := range d.DBs {
			frac := r.Age / xidWraparound
			b.WriteString(barLine(truncDisp(r.Name, 18), fmt.Sprintf("%.0f", r.Age), frac, 18, 18, 12, fmt.Sprintf("%.1f%%", 100*frac)) + "\n")
		}
		b.WriteString("```\n\n")
	}

	if len(d.Tables) > 0 {
		b.WriteString("## 高龄表 Top\n\n```\n")
		cols := []tableColumn{
			{Header: "表", Max: 40},
			{Header: "XID年龄", Right: true},
			{Header: "风险%", Right: true},
		}
		var rows [][]string
		for _, r := range d.Tables {
			rows = append(rows, []string{r.Name, fmt.Sprintf("%.0f", r.Age), fmt.Sprintf("%.1f", 100*r.Age/xidWraparound)})
		}
		b.WriteString(asciiTable(cols, rows))
		b.WriteString("```\n\n")
	}

	b.WriteString("> 风险升高时对高龄表执行 VACUUM (FREEZE);确认 autovacuum 与 freeze 参数。\n")
	if len(d.Warnings) > 0 {
		b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}
