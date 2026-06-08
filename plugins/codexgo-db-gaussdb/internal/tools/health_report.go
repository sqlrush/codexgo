package tools

import (
	"context"
	"encoding/json"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// registerHealthReport is the one-pass deterministic health diagnosis: the
// plugin collects health metrics and RENDERS the full report itself (score,
// overview table, dimension bar chart, problems, suggestions) — every figure
// real, no LLM. The report goes to the user (audience=user) for direct render;
// a terse note goes to the model so it doesn't repeat the report.
func registerHealthReport(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "health_report",
		Description: "Full DATABASE HEALTH diagnosis report for the connected GaussDB/openGauss instance, deterministically rendered by the plugin (score + module overview + per-dimension bar chart + problems + fix suggestions). Every figure is real (not LLM-generated). The report is shown DIRECTLY to the user — present it as-is and do not repeat its content; you may add one short summary line. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		r := runHealth(ctx, conn)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderHealthReport(&r), "user"),
			mcp.TextContentFor("健康诊断报告已直接展示给用户(确定性渲染)。可一句话收尾,勿重复报告内容。", "assistant"),
		}}, nil
	})
}
