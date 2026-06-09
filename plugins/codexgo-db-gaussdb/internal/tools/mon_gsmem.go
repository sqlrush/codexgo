package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /gsmem — engine memory overview, merging opendb's gsmem + sessionmem. Shows
// shared_buffers, cache hit ratio (graded), engine memory by type (gs_total_
// memory_detail), and the Top memory-consuming sessions.

const gsTotalMemSQL = `SELECT memorytype, memorymbytes
FROM gs_total_memory_detail
ORDER BY memorymbytes DESC`

const gsBufferHitSQL = `SELECT
  COALESCE(sum(blks_hit),0) AS hits,
  COALESCE(sum(blks_read),0) AS reads,
  CASE WHEN sum(blks_hit) + sum(blks_read) > 0
       THEN ROUND(100.0 * sum(blks_hit) / (sum(blks_hit) + sum(blks_read)), 2)
       ELSE 100 END AS hit_ratio
FROM pg_stat_database`

const gsSessionMemSQL = `SELECT
  sessid,
  SPLIT_PART(sessid, '.', 2) AS pid,
  ROUND(SUM(usedsize)/1048576::numeric, 2) AS used_mb,
  ROUND(SUM(totalsize)/1048576::numeric, 2) AS total_mb
FROM gs_session_memory_detail
GROUP BY sessid
ORDER BY SUM(totalsize) DESC
LIMIT 20`

type memTypeRow struct {
	Type string
	MB   float64
}
type sessMemRow struct{ Sessid, PID, UsedMB, TotalMB string }

type gsmemData struct {
	Target    string
	SharedBuf string
	HitRatio  float64
	Types     []memTypeRow
	Sessions  []sessMemRow
	Warnings  []string
}

func registerGSMem(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "gsmem",
		Description: "Show GaussDB/openGauss engine memory: shared_buffers, cache hit ratio (graded), engine memory by type (gs_total_memory_detail), and the Top memory-consuming sessions (gs_session_memory_detail). Renders panels + bars + a session table directly to the user. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectGSMem(ctx, conn)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderGSMem(d), "user"),
			mcp.TextContentFor(monDigest("内存", len(d.Types), fmt.Sprintf("命中率 %.2f%%;勿复述,可点评内存/命中率是否健康", d.HitRatio)), "assistant"),
		}}, nil
	})
}

func collectGSMem(ctx context.Context, conn *db.Conn) *gsmemData {
	d := &gsmemData{Target: conn.Label()}
	if res, err := conn.Query(ctx, "SELECT pg_size_pretty(setting::bigint * 8192) FROM pg_settings WHERE name='shared_buffers'"); err == nil && len(res.Rows) > 0 {
		d.SharedBuf = res.Rows[0][0]
	}
	if res, err := conn.Query(ctx, gsBufferHitSQL); err == nil && len(res.Rows) > 0 && len(res.Rows[0]) >= 3 {
		d.HitRatio = atof(res.Rows[0][2])
	}
	if res, err := conn.Query(ctx, gsTotalMemSQL); err != nil {
		d.Warnings = append(d.Warnings, "引擎内存采集失败: "+firstLine(err.Error()))
	} else {
		for _, r := range res.Rows {
			if len(r) < 2 {
				continue
			}
			d.Types = append(d.Types, memTypeRow{Type: r[0], MB: atof(r[1])})
		}
	}
	if res, err := conn.Query(ctx, gsSessionMemSQL); err == nil {
		for _, r := range res.Rows {
			if len(r) < 4 {
				continue
			}
			d.Sessions = append(d.Sessions, sessMemRow{Sessid: r[0], PID: r[1], UsedMB: r[2], TotalMB: r[3]})
		}
	}
	return d
}

func renderGSMem(d *gsmemData) string {
	var b strings.Builder
	b.WriteString("# 🧠 引擎内存 · " + d.Target + "\n\n")

	b.WriteString("## 概览\n\n```\n")
	b.WriteString(kvBlock([]kv{
		{"shared_buffers", nz(d.SharedBuf)},
		{"缓存命中率", fmt.Sprintf("%.2f%%  (%s)", d.HitRatio, hitGrade(d.HitRatio))},
	}))
	b.WriteString("```\n\n")

	if len(d.Types) > 0 {
		b.WriteString("## 引擎内存(按类型)\n\n```\n")
		max := d.Types[0].MB
		if max <= 0 {
			max = 1
		}
		for i, r := range d.Types {
			if i >= 10 {
				break
			}
			b.WriteString(barLine(truncDisp(r.Type, 28), prettyMB(r.MB), r.MB/max, 16, 28, 10, "") + "\n")
		}
		b.WriteString("```\n\n")
	}

	if len(d.Sessions) > 0 {
		b.WriteString("## Top 会话内存\n\n```\n")
		cols := []tableColumn{
			{Header: "会话", Max: 24},
			{Header: "PID", Max: 18},
			{Header: "已用MB", Right: true},
			{Header: "总MB", Right: true},
		}
		var rows [][]string
		for _, r := range d.Sessions {
			rows = append(rows, []string{r.Sessid, r.PID, r.UsedMB, r.TotalMB})
		}
		b.WriteString(asciiTable(cols, rows))
		b.WriteString("```\n\n")
	}

	if len(d.Warnings) > 0 {
		b.WriteString("> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}

// hitGrade grades a cache hit ratio.
func hitGrade(pct float64) string {
	switch {
	case pct >= 99:
		return "优"
	case pct >= 95:
		return "良"
	case pct >= 90:
		return "偏低"
	default:
		return "过低"
	}
}
