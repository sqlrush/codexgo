package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /bgworker — background thread status from openGauss's pg_thread_wait_status,
// plus archiver failure count. Useful for spotting stuck/abnormal background
// threads (checkpointer, walwriter, bgwriter, autovacuum launcher, …).

const bgworkerSQL = `SELECT
  thread_name,
  COUNT(*) AS count,
  STRING_AGG(DISTINCT wait_status, ', ') AS wait_statuses
FROM pg_thread_wait_status
WHERE thread_name IS NOT NULL
  AND thread_name NOT IN ('gsql', 'WorkerSession', 'workload', 'JDBC')
GROUP BY thread_name
ORDER BY thread_name`

type bgworkerRow struct{ Thread, Count, Statuses string }

type bgworkerData struct {
	Target     string
	Rows       []bgworkerRow
	ArchFailed string
	Warnings   []string
}

func registerBgWorker(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "bgworker",
		Description: "Show GaussDB/openGauss background thread status grouped by thread name with their wait statuses (pg_thread_wait_status), plus the WAL archiver failure count. Renders a table directly to the user; flags non-zero archiver failures. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectBgWorker(ctx, conn)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderBgWorker(d), "user"),
			mcp.TextContentFor(monDigest("后台进程", len(d.Rows), "勿复述;可点评是否有异常等待状态或归档失败"), "assistant"),
		}}, nil
	})
}

func collectBgWorker(ctx context.Context, conn *db.Conn) *bgworkerData {
	d := &bgworkerData{Target: conn.Label()}
	if res, err := conn.Query(ctx, bgworkerSQL); err != nil {
		d.Warnings = append(d.Warnings, "后台线程采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 3 {
				continue
			}
			d.Rows = append(d.Rows, bgworkerRow{Thread: r[0], Count: r[1], Statuses: r[2]})
		}
	}
	if v, err := conn.QueryScalar(ctx, "SELECT failed_count::text FROM pg_stat_archiver"); err == nil {
		d.ArchFailed = v
	}
	return d
}

func renderBgWorker(d *bgworkerData) string {
	var b strings.Builder
	b.WriteString("# ⚙️ 后台进程 · " + d.Target + "\n\n")
	if d.ArchFailed != "" && atof(d.ArchFailed) > 0 {
		b.WriteString(fmt.Sprintf("> ⚠️ WAL 归档失败累计 %s 次,检查 archive_command。\n\n", d.ArchFailed))
	}

	b.WriteString("```\n")
	if len(d.Rows) == 0 {
		b.WriteString("无后台线程等待状态数据。\n")
	} else {
		cols := []tableColumn{
			{Header: "线程", Max: 28},
			{Header: "数量", Right: true},
			{Header: "等待状态", Max: 46},
		}
		var rows [][]string
		for _, r := range d.Rows {
			rows = append(rows, []string{r.Thread, r.Count, r.Statuses})
		}
		b.WriteString(asciiTable(cols, rows))
	}
	b.WriteString("```\n")
	if len(d.Warnings) > 0 {
		b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}
