package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /indexadvise — index recommendations for a single SQL statement, via
// openGauss's built-in advisor gs_index_advise. Read-only (advisory only — it
// does not create anything). For full query tuning, use sqltune.

type indexAdviseData struct {
	Target string
	SQL    string
	Rows   [][]string
	Err    string
}

func registerIndexAdvise(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "indexadvise",
		Description: "Recommend indexes for a single SQL statement using GaussDB/openGauss's built-in advisor gs_index_advise. Advisory only (creates nothing). Renders the recommendations directly to the user. Arg sql required. For full query tuning (rewrites + verification), use sqltune instead. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"sql": strProp("the SQL statement to get index recommendations for"),
		}, "sql"),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			SQL string `json:"sql"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		sql := strings.TrimSpace(a.SQL)
		if sql == "" {
			return mcp.CallToolResult{}, fmt.Errorf("sql is required")
		}
		d := collectIndexAdvise(ctx, conn, sql)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderIndexAdvise(d), "user"),
			mcp.TextContentFor(monDigest("索引建议", len(d.Rows), "勿复述;结合现有索引与读写比判断是否落地"), "assistant"),
		}}, nil
	})
}

func collectIndexAdvise(ctx context.Context, conn *db.Conn, sql string) *indexAdviseData {
	d := &indexAdviseData{Target: conn.Label(), SQL: sql}
	escaped := strings.ReplaceAll(stripTrailingSemicolon(stripLeadingExplain(sql)), "'", "''")
	res, err := conn.Query(ctx, "SELECT * FROM gs_index_advise('"+escaped+"')")
	if err != nil {
		d.Err = firstLine(err.Error())
		return d
	}
	for _, r := range res.Rows {
		d.Rows = append(d.Rows, r)
	}
	return d
}

func renderIndexAdvise(d *indexAdviseData) string {
	var b strings.Builder
	b.WriteString("# 🧭 索引建议 · " + d.Target + "\n\n")
	b.WriteString("```sql\n" + strings.TrimSpace(d.SQL) + "\n```\n\n")
	if d.Err != "" {
		b.WriteString("⚠️ gs_index_advise 不可用或失败:" + d.Err + "\n\n")
		b.WriteString("> 可改用 sqltune 做完整调优(改写 + 索引 + 自动校验)。\n")
		return b.String()
	}
	if len(d.Rows) == 0 {
		b.WriteString("引擎未给出索引建议(可能已有合适索引,或查询无需索引)。\n")
		return b.String()
	}
	b.WriteString("## 引擎推荐\n\n```\n")
	for i, r := range d.Rows {
		b.WriteString(strings.TrimRight(strings.Join(r, "  "), " "))
		b.WriteString("\n")
		if i > 30 {
			break
		}
	}
	b.WriteString("```\n\n> 需结合现有索引去重、读写比与维护成本再落地;建索引建议 CONCURRENTLY 并在测试环境 EXPLAIN 验证收益。\n")
	return b.String()
}
