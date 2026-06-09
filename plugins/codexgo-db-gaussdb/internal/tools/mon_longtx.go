package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /longtx — long-running transactions. Long transactions hold an old snapshot
// that blocks VACUUM, retain locks, and inflate bloat — so they are a root cause
// behind several other dimensions. Optimization over opendb: numeric xact age
// for severity grading (>5min ⚠️ / >30min 🔴). (openGauss 5.0's
// pg_stat_activity has no backend_xmin column, so the snapshot-blocks-vacuum
// effect is surfaced as guidance, not a per-row flag.)

const (
	longTxWarnSec = 300  // > 5 min
	longTxCritSec = 1800 // > 30 min
)

const longtxSQL = `SELECT
  pid,
  COALESCE(usename,'-') AS usename,
  state,
  EXTRACT(EPOCH FROM clock_timestamp() - xact_start)::bigint AS xact_sec,
  LEFT(regexp_replace(COALESCE(query,''), E'\\s+', ' ', 'g'), 80) AS query
FROM pg_stat_activity
WHERE xact_start IS NOT NULL
  AND state <> 'idle'
  AND pid <> pg_backend_pid()
  AND query NOT LIKE '%%WLM fetch collect info%%'
  AND query NOT LIKE '%%pg_stat_get_wlm%%'
ORDER BY xact_start
LIMIT %d`

type longtxRow struct {
	PID, User, State, Query string
	XactSec                 float64
	Sev                     string
}

type longtxData struct {
	Target string
	Rows   []longtxRow
}

func registerLongTx(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "longtx",
		Description: "List long-running GaussDB/openGauss transactions (pg_stat_activity, xact_start set, not idle). Grades each by transaction age (>5min WARN, >30min CRIT) and renders a graded table directly to the user. Long transactions hold an old snapshot that blocks VACUUM and retains locks. Optional arg limit (default 20). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"limit": intProp("max rows (default 20)"),
		}),
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
		d, err := collectLongTx(ctx, conn, clampLimit(a.Limit, 20, 200))
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderLongTx(d), "user"),
			mcp.TextContentFor(longtxDigest(d), "assistant"),
		}}, nil
	})
}

func collectLongTx(ctx context.Context, conn *db.Conn, limit int) (*longtxData, error) {
	res, err := conn.Query(ctx, fmt.Sprintf(longtxSQL, limit))
	if err != nil {
		return nil, err
	}
	d := &longtxData{Target: conn.Label()}
	for _, r := range res.Rows {
		if len(r) < 5 {
			continue
		}
		sec := atof(r[3])
		d.Rows = append(d.Rows, longtxRow{
			PID: r[0], User: nz(r[1]), State: nz(r[2]), XactSec: sec,
			Query: r[4], Sev: longTxSeverity(sec),
		})
	}
	return d, nil
}

func longTxSeverity(sec float64) string {
	switch {
	case sec >= longTxCritSec:
		return statusFail
	case sec >= longTxWarnSec:
		return statusWarn
	default:
		return statusOK
	}
}

func renderLongTx(d *longtxData) string {
	var b strings.Builder
	b.WriteString("# ⏱️ 长事务 · " + d.Target + "\n\n")
	if len(d.Rows) == 0 {
		b.WriteString("✅ 当前无活动中的长事务。\n")
		return b.String()
	}
	warn, crit := 0, 0
	for _, r := range d.Rows {
		switch r.Sev {
		case statusWarn:
			warn++
		case statusFail:
			crit++
		}
	}
	b.WriteString(fmt.Sprintf("> %d 个事务 · ⚠️%d(>5min)· 🔴%d(>30min)。\n\n", len(d.Rows), warn, crit))

	b.WriteString("```\n")
	cols := []tableColumn{
		{Header: "级别"},
		{Header: "PID", Max: 18},
		{Header: "用户", Max: 12},
		{Header: "状态", Max: 22},
		{Header: "事务时长", Right: true},
		{Header: "SQL", Max: 40},
	}
	var rows [][]string
	for _, r := range d.Rows {
		rows = append(rows, []string{sevText(r.Sev), r.PID, r.User, r.State, humanSecs(r.XactSec), r.Query})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n")
	b.WriteString("> 长事务持旧快照会阻塞 VACUUM 回收并放大膨胀;确认业务后及时提交/回滚,必要时 /kill。\n")
	return b.String()
}

func longtxDigest(d *longtxData) string {
	if len(d.Rows) == 0 {
		return "长事务已渲染给用户:当前无活动长事务。可一句话确认。"
	}
	warn, crit := 0, 0
	for _, r := range d.Rows {
		switch r.Sev {
		case statusWarn:
			warn++
		case statusFail:
			crit++
		}
	}
	return fmt.Sprintf("长事务已渲染给用户:%d 个(>5min %d、>30min %d)。勿复述;可点评严重度并提示提交/回滚或 /kill。",
		len(d.Rows), warn, crit)
}

// sevText is the in-cell ASCII severity label (no emoji — keeps tables aligned).
func sevText(sev string) string {
	switch sev {
	case statusFail:
		return "严重"
	case statusWarn:
		return "警告"
	default:
		return "正常"
	}
}
