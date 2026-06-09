package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /replication — streaming replication status (primary or standby, auto-
// detected) plus replication slots, merging opendb's replication + slots +
// logicalslots. On a standalone instance it reports "no standbys".

const replPrimarySQL = `SELECT
  pid, COALESCE(usename,'-'), COALESCE(client_addr::text,'-'), state,
  COALESCE(sender_sent_location::text,'-')   AS sent_lsn,
  COALESCE(receiver_flush_location::text,'-') AS flush_lsn,
  COALESCE(receiver_replay_location::text,'-') AS replay_lsn,
  COALESCE(sync_state,'-')
FROM pg_stat_replication
ORDER BY client_addr`

const replSlotsSQL = `SELECT
  slot_name, slot_type, active::text, COALESCE(restart_lsn::text,'-')
FROM pg_replication_slots
ORDER BY slot_name`

type replData struct {
	Target    string
	InRecovry bool
	Replicas  [][]string
	Slots     [][]string
	Warnings  []string
}

func registerReplication(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "replication",
		Description: "Show GaussDB/openGauss streaming replication status — auto-detects primary vs standby (pg_is_in_recovery). On a primary, lists each standby with sent/flush/replay LSN and sync_state (pg_stat_replication); also shows replication slots (pg_replication_slots). Renders tables directly to the user. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectReplication(ctx, conn)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderReplication(d), "user"),
			mcp.TextContentFor(monDigest("复制", len(d.Replicas), "勿复述;可点评主备同步/延迟与复制槽是否健康"), "assistant"),
		}}, nil
	})
}

func collectReplication(ctx context.Context, conn *db.Conn) *replData {
	d := &replData{Target: conn.Label()}
	if v, err := conn.QueryScalar(ctx, "SELECT pg_is_in_recovery()::text"); err == nil {
		d.InRecovry = strings.EqualFold(strings.TrimSpace(v), "true") || v == "t"
	}
	if res, err := conn.Query(ctx, replPrimarySQL); err != nil {
		d.Warnings = append(d.Warnings, "复制状态采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 8 {
				continue
			}
			d.Replicas = append(d.Replicas, []string{r[0], r[2], r[3], r[4], r[5], r[6], r[7]})
		}
	}
	if res, err := conn.Query(ctx, replSlotsSQL); err == nil {
		for _, r := range res.Rows {
			if len(r) < 4 {
				continue
			}
			d.Slots = append(d.Slots, r[:4])
		}
	}
	return d
}

func renderReplication(d *replData) string {
	var b strings.Builder
	b.WriteString("# 🔁 流复制 · " + d.Target + "\n\n")
	role := "主库 / 单机(Primary)"
	if d.InRecovry {
		role = "备库(Standby)"
	}
	b.WriteString("> 角色:" + role + "\n\n")

	b.WriteString("## 备库连接\n\n")
	if len(d.Replicas) == 0 {
		b.WriteString("```\n无备库连接(单机部署,或备库未连接)。\n```\n\n")
	} else {
		b.WriteString("```\n")
		cols := []tableColumn{
			{Header: "PID", Max: 18},
			{Header: "客户端", Max: 18},
			{Header: "状态"},
			{Header: "发送LSN", Max: 16},
			{Header: "刷新LSN", Max: 16},
			{Header: "回放LSN", Max: 16},
			{Header: "同步"},
		}
		b.WriteString(asciiTable(cols, d.Replicas))
		b.WriteString("```\n\n")
	}

	b.WriteString("## 复制槽\n\n```\n")
	if len(d.Slots) == 0 {
		b.WriteString("无复制槽。\n")
	} else {
		cols := []tableColumn{
			{Header: "槽名", Max: 30},
			{Header: "类型"},
			{Header: "活跃"},
			{Header: "restart_lsn", Max: 18},
		}
		b.WriteString(asciiTable(cols, d.Slots))
	}
	b.WriteString("```\n\n")
	b.WriteString("> 关注:备库 sync_state(同步/异步)、回放延迟;inactive 复制槽会滞留 WAL,需清理。\n")
	if len(d.Warnings) > 0 {
		b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}
