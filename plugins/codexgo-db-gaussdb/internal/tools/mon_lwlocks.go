package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /lwlocks — LWLock / lightweight-lock contention and other non-idle wait
// statuses, from openGauss's pg_thread_wait_status (an OG-specific view that
// covers LWLock / lock / IO / data-structure waits). Optimization over opendb:
// a contention bar chart by wait status, total-waiters context.

const lwlocksSQL = `SELECT
  thread_name,
  wait_status,
  COALESCE(NULLIF(wait_event, ''), '-') AS wait_event,
  COUNT(*) AS waiters,
  MIN(tid) AS sample_tid
FROM pg_thread_wait_status
WHERE wait_status IS NOT NULL
  AND wait_status <> 'none'
  AND sessionid <> pg_backend_pid()
GROUP BY thread_name, wait_status, wait_event
ORDER BY waiters DESC, thread_name
LIMIT %d`

type lwlockRow struct {
	Thread, Status, Event, Waiters, SampleTID string
	WaiterN                                   int
}

type lwlocksData struct {
	Target string
	Rows   []lwlockRow
	Total  int
}

func registerLWLocks(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "lwlocks",
		Description: "Show GaussDB/openGauss lightweight-lock (LWLock) and other non-idle wait-status contention from pg_thread_wait_status, grouped by thread/status/event with waiter counts. Renders a contention bar chart + table directly to the user. Optional arg limit (default 20). Read-only.",
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
		d, err := collectLWLocks(ctx, conn, clampLimit(a.Limit, 20, 200))
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderLWLocks(d), "user"),
			mcp.TextContentFor(lwlocksDigest(d), "assistant"),
		}}, nil
	})
}

func collectLWLocks(ctx context.Context, conn *db.Conn, limit int) (*lwlocksData, error) {
	res, err := conn.Query(ctx, fmt.Sprintf(lwlocksSQL, limit))
	if err != nil {
		return nil, err
	}
	d := &lwlocksData{Target: conn.Label()}
	for _, r := range res.Rows {
		if len(r) < 5 {
			continue
		}
		n := int(atof(r[3]))
		d.Rows = append(d.Rows, lwlockRow{Thread: r[0], Status: r[1], Event: r[2], Waiters: r[3], SampleTID: r[4], WaiterN: n})
		d.Total += n
	}
	return d, nil
}

func renderLWLocks(d *lwlocksData) string {
	var b strings.Builder
	b.WriteString("# 🧵 轻量锁 / 等待状态 · " + d.Target + "\n\n")
	if len(d.Rows) == 0 {
		b.WriteString("✅ 当前无非空闲等待状态(LWLock 等)。\n")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("> %d 类等待 · 合计 %d 个等待线程。\n\n", len(d.Rows), d.Total))

	b.WriteString("## 争用分布(按等待状态)\n\n```\n")
	max := d.Rows[0].WaiterN
	if max <= 0 {
		max = 1
	}
	for i, r := range d.Rows {
		if i >= 8 {
			break
		}
		label := truncDisp(r.Status+"·"+r.Event, 28)
		b.WriteString(barLine(label, r.Waiters, float64(r.WaiterN)/float64(max), 16, 28, 4, "") + "\n")
	}
	b.WriteString("```\n\n")

	b.WriteString("## 明细\n\n```\n")
	cols := []tableColumn{
		{Header: "线程", Max: 22},
		{Header: "等待状态", Max: 24},
		{Header: "等待事件", Max: 22},
		{Header: "等待数", Right: true},
		{Header: "样例tid", Right: true},
	}
	var rows [][]string
	for _, r := range d.Rows {
		rows = append(rows, []string{r.Thread, r.Status, r.Event, r.Waiters, r.SampleTID})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n")
	return b.String()
}

func lwlocksDigest(d *lwlocksData) string {
	if len(d.Rows) == 0 {
		return "LWLock/等待状态已渲染给用户:当前无非空闲等待。可一句话确认。"
	}
	top := d.Rows[0]
	return fmt.Sprintf("LWLock/等待状态已渲染给用户:%d 类、合计 %d 线程,最高争用 %s·%s(%s)。勿复述;可点评是否存在热点争用。",
		len(d.Rows), d.Total, top.Status, top.Event, top.Waiters)
}
