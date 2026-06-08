package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// TuneReport is the structured tuning *material* — NOT a finished verdict. The
// plugin deterministically gathers the resolved SQL, plan, plan-issues and the
// engine's own index advice; codexgo's LLM then produces the 5-dimension tuning
// analysis. This decouples data-gathering (deterministic, model-agnostic) from
// reasoning (any backend), unlike opendb which bakes a single LLM into the skill
// (see OPTIMIZATIONS-OVER-OPENDB).
type TuneReport struct {
	Target      string         `json:"target"`
	Resolved    SQLFetchResult `json:"resolved"`
	Plan        []string       `json:"plan,omitempty"`
	PlanIssues  []PlanIssue    `json:"plan_issues,omitempty"`
	IndexAdvice TableReport    `json:"index_advice"`
	Analyzed    bool           `json:"analyzed"`
	Dimensions  []string       `json:"dimensions"` // checklist for the LLM
	Note        string         `json:"note"`
	Warnings    []string       `json:"warnings,omitempty"`
}

// tuneDimensions is the deterministic 5-axis checklist the LLM must cover, so the
// analysis is consistent regardless of which model codexgo is running.
var tuneDimensions = []string{
	"SQL 改写(等价逻辑、消除子查询/隐式转换)",
	"索引建议(结合 plan_issues 与 index_advice)",
	"查询 hint(连接方式/扫描方式/并行)",
	"表结构与统计信息(分区、数据类型、ANALYZE 时效)",
	"执行计划稳定性(对照 planhistory 是否回退)",
}

func registerSQLTune(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "sqltune",
		Description: "Gather SQL tuning material for a query or a unique SQL id: resolves the SQL, collects its execution plan, flags plan issues, and runs the engine index advisor (gs_index_advise). Returns structured material plus a 5-dimension checklist for you (the model) to synthesize concrete tuning advice — it does NOT itself call an LLM. Args: sql_or_id (required), analyze (bool, default false). Read-only.",
		InputSchema: jsonObjSchema(map[string]any{
			"sql_or_id": strProp("full SQL text, or a unique SQL id to resolve"),
			"analyze":   boolProp("run EXPLAIN ANALYZE for real timings (default false)"),
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
		input := strings.TrimSpace(a.SQLOrID)
		if input == "" {
			return mcp.CallToolResult{}, fmt.Errorf("sql_or_id is required")
		}

		report := TuneReport{
			Target:     conn.Label(),
			Analyzed:   a.Analyze,
			Dimensions: tuneDimensions,
			Note:       "以下为确定性采集的调优素材;请基于 plan_issues、index_advice 与五维清单给出可执行的优化建议(含改写后 SQL 与 DDL),并提醒人工在测试环境验证。",
		}

		// Resolve: a bare numeric token is treated as a SQL id, else literal SQL.
		sql := input
		if isLikelySQLID(input) {
			report.Resolved = resolveSQL(ctx, conn, input)
			if report.Resolved.Query == "" {
				return mcp.CallToolResult{}, fmt.Errorf("sql id %s not found; pass full SQL text instead", input)
			}
			sql = report.Resolved.Query
		} else {
			report.Resolved = SQLFetchResult{Query: sql, Source: "inline", HasLiterals: countPlaceholders(sql) == 0}
		}

		if !isReadOnlySQL(stripLeadingExplain(sql)) {
			return mcp.CallToolResult{}, fmt.Errorf("refusing to tune a non-read statement (only SELECT/WITH supported)")
		}
		if report.Resolved.Placeholders > 0 {
			report.Warnings = append(report.Warnings, "SQL 含占位符,无法直接 EXPLAIN;请用真实字面量替换后重试。")
		}

		// Plan (best-effort — placeholders or perms may block it).
		if report.Resolved.Placeholders == 0 {
			mode := "ANALYZE false, BUFFERS false"
			if a.Analyze {
				mode = "ANALYZE true, BUFFERS true"
			}
			plan, err := conn.Query(ctx, fmt.Sprintf("EXPLAIN (%s, FORMAT TEXT) %s", mode, stripLeadingExplain(sql)))
			if err != nil {
				report.Warnings = append(report.Warnings, "EXPLAIN 失败: "+firstLine(err.Error()))
			} else {
				for _, row := range plan.Rows {
					if len(row) > 0 {
						report.Plan = append(report.Plan, row[0])
					}
				}
				report.PlanIssues = detectPlanIssues(report.Plan)
			}
		}

		// Engine index advisor (openGauss gs_index_advise). Optional — may be
		// absent or need privileges.
		report.IndexAdvice = indexAdvice(ctx, conn, sql, &report)

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
	// gs_index_advise takes a single-quoted SQL literal; escape quotes.
	escaped := strings.ReplaceAll(stripLeadingExplain(sql), "'", "''")
	res, err := conn.Query(ctx, fmt.Sprintf("SELECT * FROM gs_index_advise('%s')", escaped))
	if err != nil {
		report.Warnings = append(report.Warnings, "gs_index_advise 不可用或失败: "+firstLine(err.Error()))
		return TableReport{Title: "索引建议(gs_index_advise)", Target: conn.Label(),
			Note: "引擎索引顾问未返回结果,请基于 plan_issues 自行判断索引方案。"}
	}
	return tableReport("索引建议(gs_index_advise)", conn.Label(),
		"openGauss 内置索引顾问的推荐;需结合业务读写比与现有索引去重后再落地。", nil, res)
}
