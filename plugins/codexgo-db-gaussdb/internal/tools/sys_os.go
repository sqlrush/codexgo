package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /os — OS-level metrics of the DATABASE host, read from openGauss's
// pv_os_run_info() view (load, cpu, memory, etc.). The plugin runs on the
// client, not the DB host, so there is no /proc fallback — if the view is
// unavailable (permissions / not openGauss) it degrades with a clear message.

const osRunInfoSQL = `SELECT name, value FROM pv_os_run_info() ORDER BY name`

type osData struct {
	Target string
	Rows   [][]string
	Err    string
}

func registerOS(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "os",
		Description: "Show OS-level metrics of the GaussDB/openGauss DATABASE host (load average, CPU, physical memory, etc.) from the pv_os_run_info() view. Renders a metric table directly to the user. Requires MONADMIN/SYSADMIN to read the view; degrades gracefully if unavailable. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectOS(ctx, conn)
		n := len(d.Rows)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderOS(d), "user"),
			mcp.TextContentFor(monDigest("主机指标", n, "勿复述;可点评负载/内存/CPU"), "assistant"),
		}}, nil
	})
}

func collectOS(ctx context.Context, conn *db.Conn) *osData {
	d := &osData{Target: conn.Label()}
	res, err := conn.Query(ctx, osRunInfoSQL)
	if err != nil {
		d.Err = firstLine(err.Error())
		return d
	}
	for _, r := range res.Rows {
		if len(r) < 2 {
			continue
		}
		if strings.TrimSpace(r[1]) == "" {
			continue
		}
		d.Rows = append(d.Rows, []string{r[0], r[1]})
	}
	return d
}

func renderOS(d *osData) string {
	var b strings.Builder
	b.WriteString("# 🖥️ 主机 OS 指标 · " + d.Target + "\n\n")
	if d.Err != "" {
		b.WriteString("⚠️ 无法读取 pv_os_run_info():" + d.Err + "\n\n")
		b.WriteString("> 该视图需在 DB 端授予 MONADMIN/SYSADMIN 权限;codexgo 在客户端运行,不读取本机 /proc(本机≠DB主机)。\n")
		return b.String()
	}
	if len(d.Rows) == 0 {
		b.WriteString("pv_os_run_info() 未返回数据。\n")
		return b.String()
	}
	b.WriteString("```\n")
	cols := []tableColumn{
		{Header: "指标", Max: 36},
		{Header: "值", Max: 30},
	}
	b.WriteString(asciiTable(cols, d.Rows))
	b.WriteString("```\n")
	return b.String()
}
