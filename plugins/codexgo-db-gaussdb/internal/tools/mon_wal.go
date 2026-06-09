package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /wal — WAL, checkpoint and archiving status, merging opendb's wal +
// walsummary + checkpoint + backup. Optimization over opendb: a single panel
// view with the requested-vs-timed checkpoint ratio flagged (high ratio means
// max_wal_size is too small). The archiver query is best-effort (openGauss 5.0
// may not expose pg_stat_archiver).

const walSettingsSQL = `SELECT name, setting, COALESCE(unit,'') AS unit
FROM pg_settings
WHERE name IN ('wal_level','max_wal_size','min_wal_size','wal_buffers',
  'checkpoint_timeout','checkpoint_completion_target',
  'archive_mode','synchronous_commit','full_page_writes')
ORDER BY name`

const walCheckpointSQL = `SELECT
  checkpoints_timed, checkpoints_req,
  checkpoint_write_time::bigint AS write_ms,
  checkpoint_sync_time::bigint AS sync_ms,
  buffers_checkpoint, buffers_backend
FROM pg_stat_bgwriter`

const walArchiverSQL = `SELECT
  archived_count, COALESCE(last_archived_wal,'-'),
  failed_count, COALESCE(last_failed_wal,'-')
FROM pg_stat_archiver`

type walData struct {
	Target     string
	CurrentLSN string
	CkTimed    float64
	CkReq      float64
	WriteMS    string
	SyncMS     string
	Archiver   []kv
	Settings   [][]string
	Warnings   []string
}

func registerWAL(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "wal",
		Description: "Show GaussDB/openGauss WAL, checkpoint and archiving status (current LSN, pg_stat_bgwriter checkpoint timed/requested ratio + write/sync time, key WAL settings, best-effort pg_stat_archiver). Flags a high requested-checkpoint ratio (max_wal_size too small). Renders panels + settings table directly to the user. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectWAL(ctx, conn)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderWAL(d), "user"),
			mcp.TextContentFor(monDigest("WAL/检查点", len(d.Settings), "勿复述;若请求式检查点占比高,提示增大 max_wal_size"), "assistant"),
		}}, nil
	})
}

func collectWAL(ctx context.Context, conn *db.Conn) *walData {
	d := &walData{Target: conn.Label()}
	if v, err := conn.QueryScalar(ctx, "SELECT pg_current_xlog_location()::text"); err == nil {
		d.CurrentLSN = v
	} else {
		d.Warnings = append(d.Warnings, "当前 LSN 采集失败: "+firstLine(err.Error()))
	}
	if res, err := conn.Query(ctx, walCheckpointSQL); err == nil && len(res.Rows) > 0 && len(res.Rows[0]) >= 6 {
		r := res.Rows[0]
		d.CkTimed, d.CkReq = atof(r[0]), atof(r[1])
		d.WriteMS, d.SyncMS = r[2], r[3]
	}
	if res, err := conn.Query(ctx, walArchiverSQL); err == nil && len(res.Rows) > 0 && len(res.Rows[0]) >= 4 {
		r := res.Rows[0]
		d.Archiver = []kv{
			{"已归档", r[0]}, {"末次归档WAL", r[1]},
			{"失败次数", r[2]}, {"末次失败WAL", r[3]},
		}
	}
	if res, err := conn.Query(ctx, walSettingsSQL); err != nil {
		d.Warnings = append(d.Warnings, "WAL 参数采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 3 {
				continue
			}
			d.Settings = append(d.Settings, []string{r[0], strings.TrimSpace(r[1] + " " + r[2])})
		}
	}
	return d
}

func renderWAL(d *walData) string {
	var b strings.Builder
	b.WriteString("# 📒 WAL / 检查点 / 归档 · " + d.Target + "\n\n")

	b.WriteString("## WAL\n\n```\n")
	b.WriteString(kvBlock([]kv{{"当前 LSN", nz(d.CurrentLSN)}}))
	b.WriteString("```\n\n")

	total := d.CkTimed + d.CkReq
	reqPct := 0.0
	if total > 0 {
		reqPct = 100 * d.CkReq / total
	}
	b.WriteString("## 检查点\n\n```\n")
	flag := ""
	if reqPct > 30 {
		flag = "  ⚠️ 偏高(增大 max_wal_size)"
	}
	b.WriteString(kvBlock([]kv{
		{"定时触发", fmt.Sprintf("%.0f", d.CkTimed)},
		{"请求触发", fmt.Sprintf("%.0f", d.CkReq)},
		{"请求式占比", fmt.Sprintf("%.1f%%%s", reqPct, flag)},
		{"写时间(ms)", nz(d.WriteMS)},
		{"同步时间(ms)", nz(d.SyncMS)},
	}))
	b.WriteString("```\n\n")

	if len(d.Archiver) > 0 {
		b.WriteString("## 归档\n\n```\n")
		b.WriteString(kvBlock(d.Archiver))
		b.WriteString("```\n\n")
	}

	if len(d.Settings) > 0 {
		b.WriteString("## 关键参数\n\n```\n")
		cols := []tableColumn{{Header: "参数", Max: 30}, {Header: "值", Max: 30}}
		b.WriteString(asciiTable(cols, d.Settings))
		b.WriteString("```\n\n")
	}

	if len(d.Warnings) > 0 {
		b.WriteString("> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}
