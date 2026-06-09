package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// PlanReport is the structured EXPLAIN result: the plan text plus deterministic
// issue flags. opendb pre-renders the plan; returning the lines AND a structured
// issue list lets codexgo highlight problems and the LLM reason precisely.
type PlanReport struct {
	Target   string      `json:"target"`
	Analyzed bool        `json:"analyzed"`
	SQL      string      `json:"sql"`
	Plan     []string    `json:"plan"`
	Issues   []PlanIssue `json:"issues"`
	Note     string      `json:"note,omitempty"`
}

// PlanIssue is one heuristic finding in the plan.
type PlanIssue struct {
	Kind       string `json:"kind"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
}

// writePrefixes are leading keywords we refuse to EXPLAIN (defensive: EXPLAIN
// without ANALYZE is read-only, but EXPLAIN ANALYZE executes the statement, so a
// write would mutate data).
var writePrefixes = []string{
	"insert", "update", "delete", "merge", "truncate", "drop",
	"alter", "create", "grant", "revoke", "vacuum", "analyze", "call",
}

func registerExplain(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "explain",
		Description: "Return the execution plan for a SELECT/WITH query. Args: sql (required), analyze (bool, default false — true actually runs the query for real row counts/timings, refused for write statements). Flags Seq Scan / Sort / Nested-Loop issues. Read-only for non-analyze.",
		InputSchema: jsonObjSchema(map[string]any{
			"sql":     strProp("the SQL to explain (SELECT/WITH only)"),
			"analyze": boolProp("run EXPLAIN ANALYZE for real timings (default false)"),
		}, "sql"),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			SQL     string `json:"sql"`
			Analyze bool   `json:"analyze"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		sql := stripLeadingExplain(strings.TrimSpace(a.SQL))
		if sql == "" {
			return mcp.CallToolResult{}, fmt.Errorf("sql is required")
		}
		if !isReadOnlySQL(sql) {
			return mcp.CallToolResult{}, fmt.Errorf("refusing to explain a non-read statement (only SELECT/WITH allowed)")
		}

		mode := "ANALYZE false, BUFFERS false"
		if a.Analyze {
			mode = "ANALYZE true, BUFFERS true"
		}
		res, err := conn.Query(ctx, fmt.Sprintf("EXPLAIN (%s, FORMAT TEXT) %s", mode, sql))
		if err != nil {
			return mcp.CallToolResult{}, fmt.Errorf("explain failed: %w", err)
		}

		report := PlanReport{Target: conn.Label(), Analyzed: a.Analyze, SQL: sql}
		for _, row := range res.Rows {
			if len(row) > 0 {
				report.Plan = append(report.Plan, row[0])
			}
		}
		report.Issues = detectPlanIssues(report.Plan)
		if len(report.Issues) == 0 {
			report.Note = "未发现常见低效算子;若仍慢,结合 planhistory 看是否计划回退或统计信息陈旧。"
		}
		return explainResult(report)
	})
}

// stripLeadingExplain removes a user-supplied leading EXPLAIN (with optional
// parenthesized options) so we control the EXPLAIN form.
func stripLeadingExplain(sql string) string {
	low := strings.ToLower(sql)
	if !strings.HasPrefix(low, "explain") {
		return sql
	}
	rest := strings.TrimSpace(sql[len("explain"):])
	if strings.HasPrefix(rest, "(") {
		if i := strings.IndexByte(rest, ')'); i >= 0 {
			return strings.TrimSpace(rest[i+1:])
		}
	}
	return rest
}

// stripTrailingSemicolon removes trailing whitespace and statement-terminating
// semicolons. SQL resolved from statement_history often ends in ';', which is
// fine as a top-level statement but breaks when the SQL is wrapped in a
// subquery — e.g. the equivalence-hash sample does `FROM (<sql>) sub`, and a
// ';' inside the parentheses is a syntax error.
func stripTrailingSemicolon(sql string) string {
	s := strings.TrimRight(sql, " \t\r\n")
	for strings.HasSuffix(s, ";") {
		s = strings.TrimRight(s[:len(s)-1], " \t\r\n")
	}
	return s
}

// isReadOnlySQL reports whether the statement is a SELECT/WITH read query.
func isReadOnlySQL(sql string) bool {
	low := strings.ToLower(strings.TrimSpace(sql))
	for _, w := range writePrefixes {
		if strings.HasPrefix(low, w) {
			return false
		}
	}
	return strings.HasPrefix(low, "select") || strings.HasPrefix(low, "with") ||
		strings.HasPrefix(low, "(") || strings.HasPrefix(low, "table") ||
		strings.HasPrefix(low, "values") || strings.HasPrefix(low, "show")
}

// detectPlanIssues scans plan lines for common inefficiencies (Seq Scan, Sort,
// nested loop over seq scan). Mirrors opendb's heuristics, returned structured.
// EXPLAIN TEXT indents child nodes with a "->" marker, so each line is reduced
// to its node label before matching.
func detectPlanIssues(plan []string) []PlanIssue {
	var issues []PlanIssue
	seqScanTables := map[string]bool{}
	hasNestedLoop := false
	for _, line := range plan {
		label := planNodeLabel(line)
		low := strings.ToLower(label)
		switch {
		case strings.HasPrefix(low, "seq scan on "):
			tbl := planSeqScanTable(label)
			if tbl != "" && !seqScanTables[tbl] {
				seqScanTables[tbl] = true
				issues = append(issues, PlanIssue{
					Kind:       "seq_scan",
					Detail:     "全表扫描: " + tbl,
					Suggestion: "为 WHERE/JOIN 过滤列建立索引,避免顺序扫描大表",
				})
			}
		case isSortOperator(low):
			issues = append(issues, PlanIssue{
				Kind:       "sort",
				Detail:     "存在排序算子: " + label,
				Suggestion: "若为 ORDER BY/GROUP BY,可考虑匹配排序的索引以消除显式排序",
			})
		case strings.Contains(low, "nested loop"):
			hasNestedLoop = true
		}
	}
	if hasNestedLoop && len(seqScanTables) > 0 {
		issues = append(issues, PlanIssue{
			Kind:       "nested_loop_seq_scan",
			Detail:     "嵌套循环连接驱动了全表扫描",
			Suggestion: "为连接列建立索引,或检查统计信息是否导致选错连接方式(应为 Hash/Merge Join)",
		})
	}
	return dedupPlanIssues(issues)
}

// planNodeLabel reduces a plan line to its node label: trims indentation and a
// leading "->" child marker.
func planNodeLabel(line string) string {
	l := strings.TrimSpace(line)
	l = strings.TrimPrefix(l, "->")
	return strings.TrimSpace(l)
}

// isSortOperator distinguishes a real Sort operator node ("Sort  (cost=...)")
// from detail lines like "Sort Key:" / "Sort Method:" which also begin "sort".
func isSortOperator(low string) bool {
	if !strings.HasPrefix(low, "sort") {
		return false
	}
	if strings.HasPrefix(low, "sort key") || strings.HasPrefix(low, "sort method") {
		return false
	}
	return strings.Contains(low, "(cost") || low == "sort"
}

func planSeqScanTable(line string) string {
	low := strings.ToLower(line)
	const marker = "seq scan on "
	i := strings.Index(low, marker)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[i+len(marker):])
	if j := strings.IndexAny(rest, " ("); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func dedupPlanIssues(in []PlanIssue) []PlanIssue {
	seen := map[string]bool{}
	var out []PlanIssue
	for _, it := range in {
		key := it.Kind + "|" + it.Detail
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out
}
