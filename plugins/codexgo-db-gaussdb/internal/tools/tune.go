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
// gathers deterministically (the "evidence"): structured plan with [Pn] cost
// hotspots, schema/index/stats, anti-patterns, engine index advice, and
// cost+equivalence-verified mechanical candidates. sqltune (single pass) feeds it
// to the model with a format reference; the model authors the optimization report.
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

// buildTuneReport does all deterministic collection for a SQL id/text: resolve,
// search_path pin, bind backfill, structured plan, schema/runtime, index advisor,
// mechanical candidates. Shared by both tuning passes.
func buildTuneReport(ctx context.Context, conn *db.Conn, sqlOrID string, analyze, verifyEquiv bool, candidate string) (*TuneReport, error) {
	input := strings.TrimSpace(sqlOrID)
	if input == "" {
		return nil, fmt.Errorf("sql_or_id is required")
	}
	report := &TuneReport{Target: conn.Label(), Dimensions: tuneDimensions}

	// 1) Resolve SQL (by id -> sqlfetch, else literal).
	rawSQL := input
	if isLikelySQLID(input) {
		report.Resolved = resolveSQL(ctx, conn, input)
		if report.Resolved.Query == "" {
			return nil, fmt.Errorf("sql id %s not found in statement_history/statement; pass full SQL text instead", input)
		}
		rawSQL = report.Resolved.Query
	} else {
		report.Resolved = SQLFetchResult{Query: rawSQL, Source: "inline", Placeholders: countPlaceholders(rawSQL), HasLiterals: countPlaceholders(rawSQL) == 0}
	}
	if !isReadOnlySQL(stripLeadingExplain(rawSQL)) {
		return nil, fmt.Errorf("refusing to tune a non-read statement (only SELECT/WITH supported)")
	}
	rawSQL = stripTrailingSemicolon(rawSQL)

	// Pin search_path to the schema owning the query's tables so EXPLAIN + the
	// schema/index evidence all agree (cross-schema same-name resolution).
	if sch := bestSchema(ctx, conn, extractTableNames(rawSQL)); sch != "" {
		_ = conn.SetSearchPath(ctx, sch)
		report.Resolved.Schema = sch
	}

	// 2) Bind backfill so the SQL is EXPLAIN-able. EffectiveSQL is always set (==
	// rawSQL when there are no placeholders) so pass 2 can verify rewrites against it.
	effSQL, fills := substituteBinds(rawSQL)
	report.EffectiveSQL = effSQL
	report.BindFills = fills
	if len(fills) > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("已回填 %d 个占位符(仅用于让 EXPLAIN 成功,值为样例,不代表真实业务取值)。", len(fills)))
	}

	// 3) SQL-text anti-patterns.
	report.SQLIssues = detectSQLIssues(rawSQL)

	// 4) Structured plan tree + plan cost + text plan + plan-level anti-patterns.
	if info, err := collectPlan(ctx, conn, effSQL, analyze); err != nil {
		report.Warnings = append(report.Warnings, "计划树采集失败: "+firstLine(err.Error()))
	} else {
		report.PlanTree = info
		report.Analyzed = info.HasAnalyze
		if info.Root != nil {
			cost := info.Root.TotalCost
			report.PlanCost = &cost
		}
	}
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
	if report.PlanTree != nil {
		treeIssues := detectPlanTreeIssues(report.PlanTree.Root, report.PlanTree.HasAnalyze)
		merged := append(append([]PlanIssue(nil), report.PlanIssues...), treeIssues...)
		report.PlanIssues = dedupPlanIssues(merged)
	}

	// 5) EXPLAIN PERFORMANCE (executes; opt-in).
	if analyze {
		if perf, err := explainPerformance(ctx, conn, effSQL); err != nil {
			report.Warnings = append(report.Warnings, "EXPLAIN PERFORMANCE 失败: "+firstLine(err.Error()))
		} else {
			report.Performance = perf
		}
	}

	// 6/7/8) Context: views, schema, runtime.
	tables := extractTableNames(rawSQL)
	if vr, ok := collectViewDefs(ctx, conn, tables); ok {
		report.Views = vr
	}
	report.Schema = collectSchema(ctx, conn, tables)
	report.Runtime = collectRuntime(ctx, conn)

	// 9) Engine index advisor.
	report.IndexAdvice = indexAdvice(ctx, conn, effSQL, report)

	// 10/11/12) Mechanical candidates (+ optional caller-supplied) verified.
	cands := generateCandidates(effSQL)
	if strings.TrimSpace(candidate) != "" {
		candSQL, _ := substituteBinds(stripTrailingSemicolon(strings.TrimSpace(candidate)))
		cands = append(cands, RewriteCandidate{Rule: "caller_supplied", SQL: candSQL, Note: "调用方/模型提供的改写"})
	}
	for i := range cands {
		verifyCandidate(ctx, conn, effSQL, &cands[i], verifyEquiv)
	}
	report.Candidates = cands

	return report, nil
}

// registerSQLTune is single-pass SQL tuning: the plugin gathers verified
// evidence (structured plan with [P1..Pn] cost-hotspot tags, schema/index/stats,
// anti-patterns, engine index advice, cost+equivalence-VERIFIED mechanical
// rewrite candidates) and hands it to the model with a format reference; the
// model writes ONE optimization report tying each fix to a [Pn] hotspot. No
// rigid schema, no second tool call — the model can deepen the analysis on top
// of the format.
func registerSQLTune(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "sqltune",
		Description: "SQL tuning (single pass) for a query or unique SQL id. The plugin deterministically gathers EVIDENCE — structured execution plan with [P1..Pn] cost-hotspot tags, cost hotspots, table/index/stats, anti-patterns, engine index advice, and mechanically-generated rewrite candidates that are cost+equivalence VERIFIED — and returns it with a format reference. Based on this evidence, write ONE complete optimization report directly to the user: root-cause analysis tied to each [Pn] hotspot; rewrites/indexes (each stating WHICH [Pn] it addresses); expected effect. Verified candidates are 【实测】; your own new rewrites are 【AI推断】 (advise test-env EXPLAIN + equivalence check). Do NOT call other tools. Args: sql_or_id (required); analyze (bool: EXECUTE for real rows/timings, default false). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"sql_or_id": strProp("full SQL text, or a unique SQL id to resolve"),
			"analyze":   boolProp("EXECUTE the query for real plan-tree actual rows/timings (graded EXPLAIN ANALYZE); default false = estimated plan only"),
		}, "sql_or_id"),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			SQLOrID string `json:"sql_or_id"`
			Analyze bool   `json:"analyze"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		// verifyEquiv=true so the mechanical candidates carry cost + equivalence
		// verdicts in the evidence the model reasons over.
		report, err := buildTuneReport(ctx, conn, a.SQLOrID, a.Analyze, true, "")
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		// Evidence (incl. SQL + [Pn]-annotated execution plan + verified candidates)
		// goes to BOTH the user (rendered deterministically, so the plan + [Pn] are
		// always present and accurate) AND the model (to reason over). A separate
		// assistant-only instruction asks the model to layer its root-cause +
		// optimization analysis on top, tying each fix to a [Pn] hotspot.
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderTuneReport(report), "user", "assistant"),
			mcp.TextContentFor(tuneAnalysisInstruction(report), "assistant"),
		}}, nil
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
