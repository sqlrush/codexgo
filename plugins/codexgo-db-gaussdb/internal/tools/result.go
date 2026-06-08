package tools

import (
	"encoding/json"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// TableReport is the structured envelope every tabular tool returns. Beyond the
// raw columns/rows it carries Title, an interpretation Note, and the Params that
// were actually applied (thresholds, sort key, limit) — so codexgo renders a
// labelled panel and the LLM reasons over real numbers WITH context. opendb
// pre-renders a fixed text table on the server; carrying structure + guidance is
// the optimization (see OPTIMIZATIONS-OVER-OPENDB).
type TableReport struct {
	Title    string            `json:"title"`
	Target   string            `json:"target"`
	Note     string            `json:"note,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
	Columns  []string          `json:"columns"`
	Rows     [][]string        `json:"rows"`
	RowCount int               `json:"row_count"`
}

// tableReport builds a TableReport from a QueryResult.
func tableReport(title, target, note string, params map[string]string, res *db.QueryResult) TableReport {
	r := TableReport{
		Title:   title,
		Target:  target,
		Note:    note,
		Params:  params,
		Columns: res.Columns,
		Rows:    res.Rows,
	}
	r.RowCount = len(res.Rows)
	return r
}

// jsonResult marshals any structured value into a single MCP text content item.
// Returning structured JSON (not pre-rendered text) lets codexgo own the UI.
func jsonResult(v any) (mcp.CallToolResult, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return mcp.CallToolResult{}, err
	}
	return mcp.CallToolResult{Content: []mcp.ContentItem{mcp.TextContent(string(out))}}, nil
}

// markdownResult returns a single text content item carrying a pre-rendered
// markdown report (the deterministic SQL tuning report). Used when the plugin
// renders the final report itself rather than handing structured JSON to codexgo.
func markdownResult(md string) (mcp.CallToolResult, error) {
	return mcp.CallToolResult{Content: []mcp.ContentItem{mcp.TextContent(md)}}, nil
}
