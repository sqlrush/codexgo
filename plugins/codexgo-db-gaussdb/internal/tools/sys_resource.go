package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /resource — usage of the bounded server resources (connections, WAL senders,
// worker processes) against their configured limits, with usage bars and an
// over-80% warning. openGauss may not expose every worker GUC; missing ones
// render as N/A.

type resGauge struct {
	Label    string
	Cur, Max float64
	HasMax   bool
}

type resourceData struct {
	Target   string
	Gauges   []resGauge
	Limits   []kv
	Warnings []string
}

func registerResource(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "resource",
		Description: "Show GaussDB/openGauss usage of bounded resources against their limits: connections (vs max_connections), WAL senders (vs max_wal_senders), and worker-process limits. Renders usage bars + a limits panel directly to the user, flagging any resource over 80%. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectResource(ctx, conn)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderResource(d), "user"),
			mcp.TextContentFor(monDigest("资源限额", len(d.Gauges), "勿复述;可点评连接/发送器是否接近上限"), "assistant"),
		}}, nil
	})
}

func collectResource(ctx context.Context, conn *db.Conn) *resourceData {
	d := &resourceData{Target: conn.Label()}
	// connections
	if res, err := conn.Query(ctx, `SELECT
  (SELECT setting::int FROM pg_settings WHERE name='max_connections'),
  (SELECT count(*) FROM pg_stat_activity)`); err == nil && len(res.Rows) > 0 && len(res.Rows[0]) >= 2 {
		d.Gauges = append(d.Gauges, resGauge{Label: "连接数", Max: atof(res.Rows[0][0]), Cur: atof(res.Rows[0][1]), HasMax: true})
	} else if err != nil {
		d.Warnings = append(d.Warnings, "连接数采集失败: "+firstLine(err.Error()))
	}
	// WAL senders
	if res, err := conn.Query(ctx, `SELECT
  (SELECT setting::int FROM pg_settings WHERE name='max_wal_senders'),
  (SELECT count(*) FROM pg_stat_replication)`); err == nil && len(res.Rows) > 0 && len(res.Rows[0]) >= 2 {
		d.Gauges = append(d.Gauges, resGauge{Label: "WAL 发送器", Max: atof(res.Rows[0][0]), Cur: atof(res.Rows[0][1]), HasMax: true})
	}
	// worker / autovacuum limits (best-effort; some GUCs absent on openGauss)
	for _, g := range []struct{ name, label string }{
		{"max_worker_processes", "max_worker_processes"},
		{"max_parallel_workers", "max_parallel_workers"},
		{"autovacuum_max_workers", "autovacuum_max_workers"},
		{"max_prepared_transactions", "max_prepared_transactions"},
	} {
		v, err := conn.QueryScalar(ctx, "SELECT setting FROM pg_settings WHERE name='"+g.name+"'")
		val := "N/A"
		if err == nil && strings.TrimSpace(v) != "" {
			val = v
		}
		d.Limits = append(d.Limits, kv{g.label, val})
	}
	return d
}

func renderResource(d *resourceData) string {
	var b strings.Builder
	b.WriteString("# 📊 资源限额使用 · " + d.Target + "\n\n")

	if len(d.Gauges) > 0 {
		b.WriteString("## 使用率\n\n```\n")
		over := false
		for _, g := range d.Gauges {
			frac := 0.0
			if g.Max > 0 {
				frac = g.Cur / g.Max
			}
			suffix := fmt.Sprintf("%.0f/%.0f %.0f%%", g.Cur, g.Max, 100*frac)
			if frac > 0.8 {
				suffix += " ⚠️"
				over = true
			}
			b.WriteString(barLine(g.Label, "", frac, 20, 12, 0, suffix) + "\n")
		}
		b.WriteString("```\n")
		if over {
			b.WriteString("\n> ⚠️ 有资源使用率超 80%,排查连接泄漏/上调上限/引入连接池。\n")
		}
		b.WriteString("\n")
	}

	if len(d.Limits) > 0 {
		b.WriteString("## 并行/工作进程限额\n\n```\n")
		b.WriteString(kvBlock(d.Limits))
		b.WriteString("```\n\n")
	}

	if len(d.Warnings) > 0 {
		b.WriteString("> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}
