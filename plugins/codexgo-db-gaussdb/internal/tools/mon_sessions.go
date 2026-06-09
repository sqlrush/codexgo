package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /sessions — all connections with state distribution, wait event, age, SQL.
// Merges opendb's sessions + activesessions (scope=active). Optimizations over
// opendb: a state-distribution bar chart, idle-in-transaction aging + warning,
// and multiline-query normalization. openGauss pg_stat_activity exposes
// waiting(bool)/enqueue(text) (not vanilla PG's wait_event_type).

const idleInTxWarnSec = 600 // idle-in-transaction longer than this is flagged

const sessionsSQL = `SELECT
  pid, usename, COALESCE(client_addr::text,'local') AS client_addr, datname,
  state,
  CASE WHEN waiting THEN 'lock_wait' WHEN enqueue <> '' THEN enqueue ELSE '-' END AS wait_event,
  LEFT(regexp_replace(COALESCE(query,''), E'\\s+', ' ', 'g'), 80) AS query,
  EXTRACT(EPOCH FROM clock_timestamp() - query_start)::int AS query_sec,
  EXTRACT(EPOCH FROM clock_timestamp() - xact_start)::int AS xact_sec
FROM pg_stat_activity
WHERE pid <> pg_backend_pid()%s
ORDER BY query_start NULLS LAST`

type sessionRow struct {
	PID, User, Addr, DB, State, Wait, Query string
	QuerySec, XactSec                       float64
}

type sessionsData struct {
	Target string
	Rows   []sessionRow
	Counts map[string]int
	Total  int
}

func registerSessions(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "sessions",
		Description: "List all GaussDB/openGauss connections (pg_stat_activity) with a state-distribution chart + a detail table (pid/user/db/state/wait/age/SQL), rendered DIRECTLY to the user. Long idle-in-transaction sessions are flagged. Optional arg scope='active' shows only active sessions. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"scope": strProp("'active' = only active sessions; 'all' (default) = every session"),
		}),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			Scope string `json:"scope"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		activeOnly := strings.EqualFold(strings.TrimSpace(a.Scope), "active")
		d, err := collectSessions(ctx, conn, activeOnly)
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderSessions(d, activeOnly), "user"),
			mcp.TextContentFor(sessionsDigest(d, activeOnly), "assistant"),
		}}, nil
	})
}

func collectSessions(ctx context.Context, conn *db.Conn, activeOnly bool) (*sessionsData, error) {
	filter := ""
	if activeOnly {
		filter = " AND state = 'active'"
	}
	res, err := conn.Query(ctx, fmt.Sprintf(sessionsSQL, filter))
	if err != nil {
		return nil, err
	}
	d := &sessionsData{Target: conn.Label(), Counts: map[string]int{}}
	for _, r := range res.Rows {
		if len(r) < 9 {
			continue
		}
		sr := sessionRow{
			PID: r[0], User: nz(r[1]), Addr: r[2], DB: nz(r[3]), State: nz(r[4]),
			Wait: nz(r[5]), Query: r[6], QuerySec: atof(r[7]), XactSec: atof(r[8]),
		}
		d.Rows = append(d.Rows, sr)
		d.Counts[sr.State]++
		d.Total++
	}
	return d, nil
}

func renderSessions(d *sessionsData, activeOnly bool) string {
	var b strings.Builder
	title := "会话概览"
	if activeOnly {
		title = "活跃会话"
	}
	b.WriteString("# 🔗 " + title + " · " + d.Target + "\n\n")
	b.WriteString(fmt.Sprintf("> 共 %d 个会话(已排除自身连接)。\n\n", d.Total))

	if d.Total > 0 {
		b.WriteString("## 状态分布\n\n```\n")
		for _, st := range sessionStateOrder(d.Counts) {
			c := d.Counts[st]
			frac := float64(c) / float64(d.Total)
			b.WriteString(barLine(truncDisp(st, 22), fmt.Sprintf("%d", c), frac, 16, 22, 4, fmt.Sprintf("%.0f%%", 100*frac)) + "\n")
		}
		b.WriteString("```\n\n")
	}

	b.WriteString("## 会话明细\n\n```\n")
	if len(d.Rows) == 0 {
		b.WriteString("无会话。\n")
	} else {
		cols := []tableColumn{
			{Header: "PID"},
			{Header: "用户", Max: 12},
			{Header: "库", Max: 10},
			{Header: "状态", Max: 24},
			{Header: "等待", Max: 14},
			{Header: "时长", Right: true},
			{Header: "SQL", Max: 40},
		}
		shown := d.Rows
		more := 0
		if len(shown) > 30 {
			more = len(shown) - 30
			shown = shown[:30]
		}
		var rows [][]string
		longIdle := false
		for _, s := range shown {
			st := s.State
			if s.State == "idle in transaction" && s.XactSec > idleInTxWarnSec {
				st = s.State + " (!)" // ASCII marker — emoji not allowed in aligned cells
				longIdle = true
			}
			rows = append(rows, []string{s.PID, s.User, s.DB, st, s.Wait, humanSecs(s.QuerySec), s.Query})
		}
		b.WriteString(asciiTable(cols, rows))
		if more > 0 {
			b.WriteString(fmt.Sprintf("… 另有 %d 个会话\n", more))
		}
		if longIdle {
			b.WriteString("(!) = 事务中空闲 >10min,可能持锁/阻塞 vacuum\n")
		}
	}
	b.WriteString("```\n")
	return b.String()
}

func sessionsDigest(d *sessionsData, activeOnly bool) string {
	var b strings.Builder
	scope := "全部"
	if activeOnly {
		scope = "活跃"
	}
	b.WriteString(fmt.Sprintf("%s会话概览已直接渲染给用户:共 %d 个", scope, d.Total))
	if len(d.Counts) > 0 {
		var parts []string
		for _, st := range sessionStateOrder(d.Counts) {
			parts = append(parts, fmt.Sprintf("%s %d", st, d.Counts[st]))
		}
		b.WriteString("(" + strings.Join(parts, " · ") + ")")
	}
	n := 0
	for _, s := range d.Rows {
		if s.State == "idle in transaction" && s.XactSec > idleInTxWarnSec {
			n++
		}
	}
	if n > 0 {
		b.WriteString(fmt.Sprintf(";⚠️ %d 个长事务中空闲(>10min,可能阻塞 vacuum/持锁)", n))
	}
	b.WriteString("。勿复述表格;可一句话点评,或用 /locks、/longtx 深入。")
	return b.String()
}

// sessionStateOrder lists states in a stable diagnostic order (active first,
// then in-transaction, idle, then any others alphabetically).
func sessionStateOrder(counts map[string]int) []string {
	pref := []string{"active", "idle in transaction", "idle in transaction (aborted)", "idle", "fastpath function call"}
	var out []string
	seen := map[string]bool{}
	for _, st := range pref {
		if _, ok := counts[st]; ok {
			out = append(out, st)
			seen[st] = true
		}
	}
	var extra []string
	for st := range counts {
		if !seen[st] {
			extra = append(extra, st)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}
