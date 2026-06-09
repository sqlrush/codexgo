package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// registerWDR wires the WDR (Workload Diagnosis Report) tools: wdr lists
// snapshots, wdranalyze generates a report between two snapshots and returns
// it as structured material for codexgo's LLM to analyze.
func registerWDR(s *mcp.Server, conn *db.Conn) {
	registerWDRList(s, conn)
	registerWDRAnalyze(s, conn)
}

// --- wdr (list snapshots) ----------------------------------------------

func registerWDRList(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "wdr",
		Description: "List available WDR snapshots from snapshot.snapshot (id, window start/end, window seconds), newest first. Use the snapshot ids with wdranalyze to generate and analyze a report. Args: limit (default 20, max 100). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"limit": intProp("max snapshots (default 20, max 100)"),
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
		limit := clampLimit(a.Limit, 20, 100)
		res, err := conn.Query(ctx, fmt.Sprintf(`SELECT
  snapshot_id,
  start_ts::text AS start_ts,
  end_ts::text AS end_ts,
  EXTRACT(EPOCH FROM end_ts - start_ts)::int AS window_sec
FROM snapshot.snapshot
ORDER BY snapshot_id DESC
LIMIT %d`, limit))
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("list WDR snapshots failed (是否已开启 enable_wdr_snapshot?): %w", err)
		}
		report := tableReport(
			"WDR 快照列表", conn.Label(),
			"挑选相邻或跨峰值的两个 snapshot_id,交给 wdranalyze 生成并分析报告。快照默认每小时一个、保留 8 天。",
			map[string]string{"limit": strconv.Itoa(limit)},
			res,
		)
		return tableResult(report)
	})
}

// --- wdranalyze (generate + return report) -----------------------------

// WDRAnalyzeReport carries the generated WDR report text plus the window it
// covers. codexgo's LLM analyzes the report_text (findings, top SQL, advice) and
// can chain sqltune on the top SQL — the analysis is NOT done in the plugin
// (model-agnostic, vs opendb's embedded synthesis pipeline).
type WDRAnalyzeReport struct {
	Target     string   `json:"target"`
	BeginSnap  int64    `json:"begin_snapshot"`
	EndSnap    int64    `json:"end_snapshot"`
	Detail     string   `json:"detail"`
	ReportText string   `json:"report_text"`
	Note       string   `json:"note"`
	Warnings   []string `json:"warnings,omitempty"`
}

func registerWDRAnalyze(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "wdranalyze",
		Description: "Generate a WDR report between two snapshots and return its full text for you (the model) to analyze: characterize the workload, extract risk findings, and list top SQL (then drill with sqltune). Args: begin (snapshot id), end (snapshot id) — omit both to use the two latest snapshots; detail one of summary(default)|all. It generates the report deterministically and does NOT itself call an LLM. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"begin":  intProp("begin snapshot id (omit to auto-pick second-latest)"),
			"end":    intProp("end snapshot id (omit to auto-pick latest)"),
			"detail": strProp("report detail: summary (default) | all"),
		}),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			Begin  int64  `json:"begin"`
			End    int64  `json:"end"`
			Detail string `json:"detail"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		detail := strings.ToLower(strings.TrimSpace(a.Detail))
		if detail != "all" {
			detail = "summary"
		}

		report := WDRAnalyzeReport{Target: conn.Label(), Detail: detail}

		// Auto-pick the two latest snapshots when not specified.
		if a.Begin == 0 || a.End == 0 {
			b, e, err := latestSnapshotPair(ctx, conn)
			if err != nil {
				return mcp.CallToolResult{}, err
			}
			a.Begin, a.End = b, e
			report.Warnings = append(report.Warnings, fmt.Sprintf("未指定快照,自动选用最近两个: %d → %d", b, e))
		}
		if a.Begin >= a.End {
			return mcp.CallToolResult{}, fmt.Errorf("begin snapshot (%d) must be earlier than end (%d)", a.Begin, a.End)
		}
		report.BeginSnap, report.EndSnap = a.Begin, a.End

		text, err := generateWDR(ctx, conn, a.Begin, a.End, detail)
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		report.ReportText = text
		report.Note = "report_text 为引擎生成的 WDR 原文;请据此输出:工作负载画像、风险发现(分级)、Top SQL 列表,并对 Top SQL 调 sqltune 下钻。提醒结论需人工复核。"
		return wdrAnalyzeResult(report)
	})
}

// latestSnapshotPair returns the (second-latest, latest) snapshot ids.
func latestSnapshotPair(ctx context.Context, conn *db.Conn) (int64, int64, error) {
	res, err := conn.Query(ctx, `SELECT snapshot_id FROM snapshot.snapshot ORDER BY snapshot_id DESC LIMIT 2`)
	if err != nil {
		return 0, 0, fmt.Errorf("read snapshots failed (是否已开启 WDR?): %w", err)
	}
	if len(res.Rows) < 2 {
		return 0, 0, fmt.Errorf("need at least 2 WDR snapshots, found %d", len(res.Rows))
	}
	latest, _ := strconv.ParseInt(strings.TrimSpace(res.Rows[0][0]), 10, 64)
	prev, _ := strconv.ParseInt(strings.TrimSpace(res.Rows[1][0]), 10, 64)
	return prev, latest, nil
}

// generateWDR tries the known generate_wdr_report signatures (schema-qualified
// and bare) and returns the concatenated report text.
func generateWDR(ctx context.Context, conn *db.Conn, begin, end int64, detail string) (string, error) {
	variants := []string{
		fmt.Sprintf("SELECT pg_catalog.generate_wdr_report(%d::bigint, %d::bigint, '%s', 'cluster', '')", begin, end, detail),
		fmt.Sprintf("SELECT generate_wdr_report(%d::bigint, %d::bigint, '%s', 'cluster', '')", begin, end, detail),
	}
	var lastErr error
	for _, q := range variants {
		res, err := conn.Query(ctx, q)
		if err != nil {
			lastErr = err
			continue
		}
		var b strings.Builder
		for _, row := range res.Rows {
			if len(row) > 0 {
				b.WriteString(row[0])
				b.WriteByte('\n')
			}
		}
		if b.Len() > 0 {
			return b.String(), nil
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("generate_wdr_report failed (检查 WDR 配置与权限): %w", lastErr)
	}
	return "", fmt.Errorf("generate_wdr_report returned empty report")
}
