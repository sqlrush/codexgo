package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// TableReport is the structured envelope every tabular tool returns. Beyond the
// raw columns/rows it carries Title, an interpretation Note, and the Params that
// were actually applied (thresholds, sort key, limit). The plugin renders it to
// an aligned ASCII table (audience=user) — opendb pre-renders a fixed text table
// on the server; carrying structure lets the plugin render consistently and the
// LLM still reason over real numbers via the assistant digest.
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
// Retained for the few model-facing payloads; tabular tools use tableResult.
func jsonResult(v any) (mcp.CallToolResult, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return mcp.CallToolResult{}, err
	}
	return mcp.CallToolResult{Content: []mcp.ContentItem{mcp.TextContent(string(out))}}, nil
}

// tableResult renders a TableReport to the user (aligned ASCII table) and hands
// the model a terse digest, matching the monitoring-tool render convention.
func tableResult(r TableReport) (mcp.CallToolResult, error) {
	return mcp.CallToolResult{Content: []mcp.ContentItem{
		mcp.TextContentFor(renderTableReport(r), "user"),
		mcp.TextContentFor(tableDigest(r), "assistant"),
	}}, nil
}

func tableDigest(r TableReport) string {
	return fmt.Sprintf("%s:%d 行已直接渲染给用户。勿复述表格;可一句话点评,或用 sqlfetch/sqltune 等下钻。", r.Title, r.RowCount)
}

// renderTableReport renders a TableReport as markdown: title + note + an aligned
// ASCII table inside a fenced block + an applied-params line.
func renderTableReport(r TableReport) string {
	var b strings.Builder
	b.WriteString("# " + nzTitle(r.Title))
	if r.Target != "" {
		b.WriteString(" · " + r.Target)
	}
	b.WriteString("\n\n")
	if r.Note != "" {
		b.WriteString("> " + r.Note + "\n\n")
	}
	if len(r.Rows) == 0 {
		b.WriteString("（无数据）\n")
	} else {
		b.WriteString("```\n")
		b.WriteString(renderColsRows(r.Columns, r.Rows))
		b.WriteString("```\n")
	}
	if len(r.Params) > 0 {
		b.WriteString("\n参数:" + paramsLine(r.Params) + "\n")
	}
	return b.String()
}

// renderColsRows turns raw columns + string rows into an aligned ASCII table.
// CRITICAL: cells are whitespace-collapsed first — a query/plan cell may contain
// newlines, which would otherwise break the table layout. Long text columns are
// width-capped; columns whose every value is numeric are right-aligned.
func renderColsRows(columns []string, rows [][]string) string {
	n := len(columns)
	cols := make([]tableColumn, n)
	for i, c := range columns {
		tc := tableColumn{Header: c}
		lc := strings.ToLower(c)
		if containsAny(lc, "query", "plan", "sql", "def", "conninfo", "command", "text") {
			tc.Max = 56
		}
		cols[i] = tc
	}
	// numeric detection per column (right-align numbers).
	for i := 0; i < n; i++ {
		allNum, any := true, false
		for _, row := range rows {
			if i >= len(row) {
				continue
			}
			v := strings.TrimSpace(row[i])
			if v == "" {
				continue
			}
			any = true
			if !looksNumeric(v) {
				allNum = false
				break
			}
		}
		if any && allNum {
			cols[i].Right = true
		}
	}
	clean := make([][]string, len(rows))
	for ri, row := range rows {
		cr := make([]string, n)
		for i := 0; i < n; i++ {
			if i < len(row) {
				cr[i] = cleanCell(row[i])
			}
		}
		clean[ri] = cr
	}
	return asciiTable(cols, clean)
}

// cleanCell collapses all whitespace (incl. newlines/tabs) to single spaces so a
// multi-line SQL/plan cell renders on one table line.
func cleanCell(s string) string { return strings.Join(strings.Fields(s), " ") }

func looksNumeric(s string) bool {
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	return err == nil
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func paramsLine(p map[string]string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+p[k])
	}
	return strings.Join(parts, " · ")
}

func nzTitle(s string) string {
	if strings.TrimSpace(s) == "" {
		return "结果"
	}
	return s
}
