package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// TuneReport is the structured, largely *verified* tuning material the plugin
// gathers deterministically; codexgo's model then synthesizes the final advice.
// Beyond the basic plan, it now carries (sqltune parity, A-region):
//   - bind-variable backfill so normalized SQL is EXPLAIN-able (#2)
//   - view definitions (#3), plan cost + EXPLAIN PERFORMANCE (#4/#5)
//   - plan + SQL anti-pattern annotations (#6)
//   - schema + runtime context (#7/#8)
//   - engine index advice (gs_index_advise)
//   - mechanical rewrite candidates verified by cost diff + equivalence (#10/#11/#12)
type TuneReport struct {
	Target       string             `json:"target"`
	Resolved     SQLFetchResult     `json:"resolved"`
	BindFills    []BindFill         `json:"bind_fills,omitempty"`
	EffectiveSQL string             `json:"effective_sql,omitempty"`
	PlanCost     *float64           `json:"plan_cost,omitempty"`
	Plan         []string           `json:"plan,omitempty"`
	PlanTree     *PlanInfo          `json:"plan_tree,omitempty"`
	Performance  string             `json:"explain_performance,omitempty"`
	PlanIssues   []PlanIssue        `json:"plan_issues,omitempty"`
	SQLIssues    []PlanIssue        `json:"sql_issues,omitempty"`
	Views        TableReport        `json:"views,omitempty"`
	Schema       SchemaContext      `json:"schema"`
	Runtime      RuntimeContext     `json:"runtime"`
	IndexAdvice  TableReport        `json:"index_advice"`
	Candidates   []RewriteCandidate `json:"candidates,omitempty"`
	Analyzed     bool               `json:"analyzed"`
	Dimensions   []string           `json:"dimensions"`
	Note         string             `json:"note"`
	Warnings     []string           `json:"warnings,omitempty"`
}

var tuneDimensions = []string{
	"SQL 改写(等价逻辑、消除子查询/隐式转换)",
	"索引建议(结合 plan_issues / sql_issues / index_advice)",
	"查询 hint(连接方式/扫描方式/并行)",
	"表结构与统计信息(看 schema.stats 是否陈旧、缺索引)",
	"执行计划稳定性(对照 planhistory 是否回退)",
}

func registerSQLTune(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "sqltune",
		Description: "Deep SQL tuning material for a query or unique SQL id: resolves the SQL and backfills bind placeholders so it can be EXPLAINed; collects the structured plan tree + plan cost, plan/SQL anti-patterns, schema (tables/indexes/stats/FK), runtime waits/locks, view defs, and the engine index advisor; generates mechanical rewrite candidates and verifies them by plan-cost diff (and result equivalence when verify_equiv=true). Returns structured, mostly-verified material plus a 5-dimension checklist for you (the model) to synthesize the final plan — it does NOT call an LLM. By default it does NOT execute the query (estimated plan only). Args: sql_or_id (required); candidate (optional rewrite to verify); analyze (bool: EXECUTE the query for real plan-tree timings/rows via graded EXPLAIN ANALYZE + EXPLAIN PERFORMANCE); verify_equiv (bool: hash-compare candidate results, executes queries). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"sql_or_id":    strProp("full SQL text, or a unique SQL id to resolve"),
			"candidate":    strProp("optional: a rewritten SQL to verify (cost + equivalence) against the original"),
			"analyze":      boolProp("EXECUTE the query to get real plan-tree actual rows/timings (graded EXPLAIN ANALYZE, with timeout + fallback) and EXPLAIN PERFORMANCE; default false = estimated plan only, query not run"),
			"verify_equiv": boolProp("hash-compare candidate vs original results to confirm equivalence (executes both, bounded; default false)"),
		}, "sql_or_id"),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			SQLOrID     string `json:"sql_or_id"`
			Candidate   string `json:"candidate"`
			Analyze     bool   `json:"analyze"`
			VerifyEquiv bool   `json:"verify_equiv"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		input := strings.TrimSpace(a.SQLOrID)
		if input == "" {
			return mcp.CallToolResult{}, fmt.Errorf("sql_or_id is required")
		}

		report := TuneReport{
			Target:     conn.Label(),
			Dimensions: tuneDimensions,
			Note:       "确定性采集 + 校验后的调优素材;请据 plan_issues/sql_issues/schema/index_advice 与已验证的 candidates 给出可执行方案(改写 SQL + DDL),并提醒人工在测试环境复核。candidates.cost_ratio>1 且 equivalent=yes 才是可直接采纳的改写。",
		}

		// 1) Resolve SQL (by id -> sqlfetch, else literal).
		rawSQL := input
		if isLikelySQLID(input) {
			report.Resolved = resolveSQL(ctx, conn, input)
			if report.Resolved.Query == "" {
				return mcp.CallToolResult{}, fmt.Errorf("sql id %s not found in statement_history/statement; pass full SQL text instead", input)
			}
			rawSQL = report.Resolved.Query
		} else {
			report.Resolved = SQLFetchResult{Query: rawSQL, Source: "inline", Placeholders: countPlaceholders(rawSQL), HasLiterals: countPlaceholders(rawSQL) == 0}
		}

		if !isReadOnlySQL(stripLeadingExplain(rawSQL)) {
			return mcp.CallToolResult{}, fmt.Errorf("refusing to tune a non-read statement (only SELECT/WITH supported)")
		}
		// Drop any trailing ';' so the SQL can be wrapped in a subquery (EXPLAIN
		// tolerates it, but the equivalence-hash sample's `FROM (<sql>) sub` does not).
		rawSQL = stripTrailingSemicolon(rawSQL)

		// 2) Bind backfill so the SQL is EXPLAIN-able.
		effSQL, fills := substituteBinds(rawSQL)
		report.BindFills = fills
		if len(fills) > 0 {
			report.EffectiveSQL = effSQL
			report.Warnings = append(report.Warnings, fmt.Sprintf("已回填 %d 个占位符(仅用于让 EXPLAIN 成功,值为样例,不代表真实业务取值)。", len(fills)))
		}

		// 3) SQL-text anti-patterns (no DB needed).
		report.SQLIssues = detectSQLIssues(rawSQL)

		// 4) Structured plan tree (graded ANALYZE only when analyze=true; else
		//    plan-only estimates) + plan cost (from tree root) + text plan +
		//    plan-level anti-patterns (text-based and tree-based).
		if info, err := collectPlan(ctx, conn, effSQL, a.Analyze); err != nil {
			report.Warnings = append(report.Warnings, "计划树采集失败: "+firstLine(err.Error()))
		} else {
			report.PlanTree = info
			report.Analyzed = info.HasAnalyze // actual, not requested
			if info.Root != nil {
				cost := info.Root.TotalCost
				report.PlanCost = &cost
			}
		}
		// Text plan for human readability + text-based anti-pattern detection.
		if plan, err := conn.Query(ctx, fmt.Sprintf("EXPLAIN (ANALYZE false, BUFFERS false, FORMAT TEXT) %s", stripLeadingExplain(effSQL))); err != nil {
			report.Warnings = append(report.Warnings, "EXPLAIN TEXT 失败: "+firstLine(err.Error()))
		} else {
			for _, row := range plan.Rows {
				if len(row) > 0 {
					report.Plan = append(report.Plan, row[0])
				}
			}
			report.PlanIssues = detectPlanIssues(report.Plan)
		}
		// Merge structured-tree issues (sort spill / expensive hash / row skew).
		// Build a fresh slice (don't alias report.PlanIssues' backing array).
		if report.PlanTree != nil {
			treeIssues := detectPlanTreeIssues(report.PlanTree.Root, report.PlanTree.HasAnalyze)
			merged := append(append([]PlanIssue(nil), report.PlanIssues...), treeIssues...)
			report.PlanIssues = dedupPlanIssues(merged)
		}

		// 5) EXPLAIN PERFORMANCE (executes; opt-in).
		if a.Analyze {
			if perf, err := explainPerformance(ctx, conn, effSQL); err != nil {
				report.Warnings = append(report.Warnings, "EXPLAIN PERFORMANCE 失败: "+firstLine(err.Error()))
			} else {
				report.Performance = perf
			}
		}

		// 6/7/8) Context: tables, views, schema, runtime.
		tables := extractTableNames(rawSQL)
		if vr, ok := collectViewDefs(ctx, conn, tables); ok {
			report.Views = vr
		}
		report.Schema = collectSchema(ctx, conn, tables)
		report.Runtime = collectRuntime(ctx, conn)

		// 9) Engine index advisor.
		report.IndexAdvice = indexAdvice(ctx, conn, effSQL, &report)

		// 10/11/12) Candidates (mechanical + caller-supplied) verified by cost + equivalence.
		cands := generateCandidates(effSQL)
		if strings.TrimSpace(a.Candidate) != "" {
			candSQL, _ := substituteBinds(stripTrailingSemicolon(strings.TrimSpace(a.Candidate)))
			cands = append(cands, RewriteCandidate{Rule: "caller_supplied", SQL: candSQL, Note: "调用方/模型提供的改写"})
		}
		for i := range cands {
			verifyCandidate(ctx, conn, effSQL, &cands[i], a.VerifyEquiv)
		}
		report.Candidates = cands

		return jsonResult(report)
	})
}

// isLikelySQLID reports whether input looks like a unique SQL id rather than a
// SQL statement (all digits, optional leading minus, no whitespace).
func isLikelySQLID(s string) bool {
	if s == "" || strings.ContainsAny(s, " \t\n") {
		return false
	}
	for i, c := range s {
		if c == '-' && i == 0 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// indexAdvice runs gs_index_advise and returns its recommendations as a table.
func indexAdvice(ctx context.Context, conn *db.Conn, sql string, report *TuneReport) TableReport {
	escaped := strings.ReplaceAll(stripLeadingExplain(sql), "'", "''")
	res, err := conn.Query(ctx, fmt.Sprintf("SELECT * FROM gs_index_advise('%s')", escaped))
	if err != nil {
		report.Warnings = append(report.Warnings, "gs_index_advise 不可用或失败: "+firstLine(err.Error()))
		return TableReport{Title: "索引建议(gs_index_advise)", Target: conn.Label(),
			Note: "引擎索引顾问未返回结果,请基于 plan_issues/sql_issues 自行判断索引方案。"}
	}
	return tableReport("索引建议(gs_index_advise)", conn.Label(),
		"openGauss 内置索引顾问的推荐;需结合业务读写比与现有索引去重后再落地。", nil, res)
}
