package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /locks — lock waits + blocking chain, merging opendb's locks + blocktree.
// The pg_locks self-join keys on transactionid; the blocking-chain query adds
// `kl.granted` (CRITICAL: without it an N-waiter pile-up explodes into N×(N-1)
// rows). Optimization over opendb: one tool renders BOTH the flat wait table and
// the ASCII blocking tree, and points at the root blocker for /kill.

const locksSQL = `SELECT
  blocked.pid AS blocked_pid,
  COALESCE(blocked.usename,'-') AS blocked_user,
  LEFT(regexp_replace(COALESCE(blocked.query,''), E'\\s+', ' ', 'g'), 80) AS blocked_query,
  blocker.pid AS blocker_pid,
  COALESCE(blocker.usename,'-') AS blocker_user,
  LEFT(regexp_replace(COALESCE(blocker.query,''), E'\\s+', ' ', 'g'), 80) AS blocker_query,
  CASE WHEN blocked.waiting THEN 'Lock' ELSE '-' END AS wait_type,
  CASE WHEN blocked.waiting THEN 'lock_wait' WHEN blocked.enqueue <> '' THEN blocked.enqueue ELSE '-' END AS wait_event
FROM pg_locks bl
JOIN pg_stat_activity blocked ON blocked.pid = bl.pid
JOIN pg_locks kl ON kl.transactionid = bl.transactionid AND kl.pid <> bl.pid AND kl.granted
JOIN pg_stat_activity blocker ON blocker.pid = kl.pid
WHERE NOT bl.granted`

type locksData struct {
	Target  string
	Rows    [][]string
	Roots   []*blockNode
	Blocked int
}

func registerLocks(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "locks",
		Description: "Show GaussDB/openGauss lock waits AND the blocking-chain tree (pg_locks self-join on transactionid with granted holder). Renders a flat wait table + a CJK-safe ASCII blocking tree directly to the user, and identifies the root blocker pid (candidate for /kill). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		d, err := collectLocks(ctx, conn)
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderLocks(d), "user"),
			mcp.TextContentFor(locksDigest(d), "assistant"),
		}}, nil
	})
}

func collectLocks(ctx context.Context, conn *db.Conn) (*locksData, error) {
	res, err := conn.Query(ctx, locksSQL)
	if err != nil {
		return nil, err
	}
	d := &locksData{Target: conn.Label(), Rows: res.Rows}
	d.Roots, d.Blocked = buildBlockTree(res.Rows)
	return d, nil
}

func renderLocks(d *locksData) string {
	var b strings.Builder
	b.WriteString("# 🔒 锁等待与阻塞链 · " + d.Target + "\n\n")
	if len(d.Rows) == 0 {
		b.WriteString("✅ 当前无锁等待 / 阻塞链。\n")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("> %d 条等待边 · %d 个被阻塞会话 · %d 条阻塞链。\n\n", len(d.Rows), d.Blocked, len(d.Roots)))

	b.WriteString("## 阻塞链\n\n```\n")
	b.WriteString(renderBlockTree(d.Roots))
	b.WriteString("```\n\n")

	b.WriteString("## 等待明细\n\n```\n")
	cols := []tableColumn{
		{Header: "被阻塞pid"},
		{Header: "等待"},
		{Header: "阻塞源pid"},
		{Header: "被阻塞SQL", Max: 44},
	}
	var rows [][]string
	for _, r := range d.Rows {
		if len(r) < 8 {
			continue
		}
		rows = append(rows, []string{r[0], r[7], r[3], r[2]})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n")
	if len(d.Roots) > 0 {
		var pids []string
		for _, r := range d.Roots {
			pids = append(pids, r.PID)
		}
		b.WriteString("> 根阻塞源 pid:" + strings.Join(pids, ", ") + " —— 确认其长事务/未提交后,可用 /kill 释放。\n")
	}
	return b.String()
}

func locksDigest(d *locksData) string {
	if len(d.Rows) == 0 {
		return "锁诊断已渲染给用户:当前无锁等待 / 阻塞链。可一句话确认。"
	}
	var pids []string
	for _, r := range d.Roots {
		pids = append(pids, r.PID)
	}
	return fmt.Sprintf("锁诊断已渲染给用户:%d 条等待边、%d 个被阻塞会话、%d 条阻塞链,根阻塞源 pid %s。勿复述;可点评严重度,或建议核查根阻塞源会话(长事务/未提交)。",
		len(d.Rows), d.Blocked, len(d.Roots), strings.Join(pids, ", "))
}
