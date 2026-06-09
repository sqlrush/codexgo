package tools

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// Renderers for the original statement/diagnostic tools (ash / explain /
// indexhealth / sqlfetch / wdranalyze), each following the per-command UI plan
// in docs/OPENDB-COMMAND-MIGRATION.zh-CN.md — NOT a generic table. They render
// to audience=user (direct) + a terse assistant digest, matching the monitoring
// tools. (slowsql / topsql / planhistory / wdr are plain tables → tableResult.)

// --- ash: wait distribution bars + active-session table ---------------------

func ashResult(r AshReport) (mcp.CallToolResult, error) {
	return mcp.CallToolResult{Content: []mcp.ContentItem{
		mcp.TextContentFor(renderAsh(r), "user"),
		mcp.TextContentFor("ASH(等待分布 + 活动会话)已渲染给用户。勿复述;可点评瓶颈类型(On CPU=算力,Lock/等待=阻塞)并下钻 topsql/locks。", "assistant"),
	}}, nil
}

func renderAsh(r AshReport) string {
	var b strings.Builder
	b.WriteString("# 🔥 活跃会话采样(ASH) · " + r.Target + "\n\n")

	b.WriteString("## 等待分布(当前快照)\n\n")
	d := r.Distribution
	if len(d.Rows) == 0 {
		b.WriteString("```\n当前无活跃会话(空载)。\n```\n\n")
	} else {
		if d.Note != "" {
			b.WriteString("> " + d.Note + "\n\n")
		}
		b.WriteString("```\n")
		for _, row := range d.Rows {
			if len(row) < 4 {
				continue
			}
			label := truncDisp(cleanCell(row[0])+"·"+cleanCell(row[1]), 24)
			pct := atof(row[3])
			b.WriteString(barLine(label, cleanCell(row[2]), pct/100, 16, 24, 4, fmt.Sprintf("%.0f%%", pct)) + "\n")
		}
		b.WriteString("```\n\n")
	}

	if len(r.ActiveSessions.Rows) > 0 {
		b.WriteString("## 活动会话明细\n\n")
		if r.ActiveSessions.Note != "" {
			b.WriteString("> " + r.ActiveSessions.Note + "\n\n")
		}
		b.WriteString("```\n" + renderColsRows(r.ActiveSessions.Columns, r.ActiveSessions.Rows) + "```\n\n")
	}
	if r.Note != "" {
		b.WriteString("> " + r.Note + "\n")
	}
	return b.String()
}

// --- explain: SQL + plan tree + risk highlights -----------------------------

func explainResult(r PlanReport) (mcp.CallToolResult, error) {
	digest := fmt.Sprintf("执行计划已渲染给用户(%d 个风险点)。勿复述;可解读瓶颈算子,或用 sqltune 深度调优。", len(r.Issues))
	return mcp.CallToolResult{Content: []mcp.ContentItem{
		mcp.TextContentFor(renderExplain(r), "user"),
		mcp.TextContentFor(digest, "assistant"),
	}}, nil
}

func renderExplain(r PlanReport) string {
	var b strings.Builder
	mode := "估算计划(未执行查询)"
	if r.Analyzed {
		mode = "实测计划(EXPLAIN ANALYZE,已执行)"
	}
	b.WriteString("# 🔎 执行计划 · " + r.Target + "\n\n> " + mode + "\n\n")
	b.WriteString("## SQL\n\n```sql\n" + strings.TrimSpace(r.SQL) + "\n```\n\n")
	b.WriteString("## 计划\n\n```plan\n")
	if len(r.Plan) == 0 {
		b.WriteString("(无计划输出)\n")
	} else {
		for _, line := range r.Plan {
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("```\n\n")
	if len(r.Issues) > 0 {
		b.WriteString("## ⚠️ 风险点\n\n")
		for _, is := range r.Issues {
			b.WriteString("- **" + is.Detail + "** → " + is.Suggestion + "\n")
		}
		b.WriteString("\n")
	}
	if r.Note != "" {
		b.WriteString("> " + r.Note + "\n")
	}
	return b.String()
}

// --- indexhealth: summary + 4 sections --------------------------------------

func indexHealthResult(r IndexHealthReport) (mcp.CallToolResult, error) {
	return mcp.CallToolResult{Content: []mcp.ContentItem{
		mcp.TextContentFor(renderIndexHealth(r), "user"),
		mcp.TextContentFor("索引健康(未使用/失效/重复/大索引)已渲染给用户。勿复述;可点评清理优先级。", "assistant"),
	}}, nil
}

func renderIndexHealth(r IndexHealthReport) string {
	var b strings.Builder
	b.WriteString("# 🗂️ 索引健康 · " + r.Target + "\n\n")
	b.WriteString("## 总览\n\n```\n")
	b.WriteString(kvBlock([]kv{
		{"未使用索引", strconv.Itoa(r.Summary["unused"])},
		{"失效/未就绪", strconv.Itoa(r.Summary["invalid"])},
		{"重复索引", strconv.Itoa(r.Summary["duplicate"])},
		{"大索引(>10MB)", strconv.Itoa(r.Summary["bloat"])},
	}))
	b.WriteString("```\n\n")
	for _, sec := range []TableReport{r.Unused, r.Invalid, r.Duplicate, r.Bloat} {
		b.WriteString(renderIndexHealthSection(sec))
	}
	for _, n := range r.Notes {
		b.WriteString("> " + n + "\n")
	}
	return b.String()
}

func renderIndexHealthSection(sec TableReport) string {
	var b strings.Builder
	b.WriteString("## " + nzTitle(sec.Title) + "\n\n")
	if sec.Note != "" {
		b.WriteString("> " + sec.Note + "\n\n")
	}
	if len(sec.Rows) == 0 {
		b.WriteString("✅ 无\n\n")
		return b.String()
	}
	b.WriteString("```\n" + renderColsRows(sec.Columns, sec.Rows) + "```\n\n")
	return b.String()
}

// --- sqlfetch: provenance panel + SQL block ---------------------------------

func sqlFetchResult(r SQLFetchResult) (mcp.CallToolResult, error) {
	return mcp.CallToolResult{Content: []mcp.ContentItem{
		mcp.TextContentFor(renderSQLFetch(r), "user"),
		mcp.TextContentFor(fmt.Sprintf("sql_id %s 已解析(来源 %s,占位符 %d)。勿复述;可对其调 sqltune 调优。", r.SQLID, r.Source, r.Placeholders), "assistant"),
	}}, nil
}

func renderSQLFetch(r SQLFetchResult) string {
	var b strings.Builder
	b.WriteString("# 🔍 SQL 解析 · sql_id " + r.SQLID + "\n\n")
	status := "✅ 含字面量,可直接 EXPLAIN"
	if !r.HasLiterals {
		status = fmt.Sprintf("⚠️ 含 %d 个占位符,EXPLAIN 前需回填真实值", r.Placeholders)
	}
	kvs := []kv{{"来源", nz(r.Source)}, {"schema", nz(r.Schema)}, {"状态", status}}
	if r.StartTime != "" {
		kvs = append(kvs, kv{"采样时间", r.StartTime})
	}
	b.WriteString("```\n" + kvBlock(kvs) + "```\n\n")
	b.WriteString("## SQL\n\n```sql\n" + strings.TrimSpace(r.Query) + "\n```\n")
	if r.Note != "" {
		b.WriteString("\n> " + r.Note + "\n")
	}
	return b.String()
}

// --- wdranalyze: LLM-driven (model analyzes the engine WDR text) -------------

func wdrAnalyzeResult(r WDRAnalyzeReport) (mcp.CallToolResult, error) {
	// Evidence = the engine-generated WDR report. openGauss returns it as HTML, so
	// parse it into readable text tables (raw HTML is useless to read). Rendered to
	// BOTH the user (slash shows real content) and the model (to analyze).
	var ev strings.Builder
	ev.WriteString(fmt.Sprintf("# 📈 WDR 报告 · snap %d → %d\n\n", r.BeginSnap, r.EndSnap))
	if len(r.Warnings) > 0 {
		ev.WriteString("> " + strings.Join(r.Warnings, " · ") + "\n\n")
	}
	if secs := parseWDRHTML(r.ReportText); len(secs) > 0 {
		ev.WriteString(renderWDRSections(secs))
	} else {
		// not HTML (or unparsable) — fall back to the raw text in a fence.
		ev.WriteString("```\n" + strings.TrimSpace(r.ReportText) + "\n```\n")
	}

	instr := "上面是引擎生成的 WDR 原文(已直接展示给用户)。请勿复述原文,在其后产出 markdown 分析:\n" +
		"## 工作负载画像\n## 风险发现(按严重度分级)\n## Top SQL(并对最重的 SQL 调 sqltune 下钻)\n结论标注需人工复核。中文输出,不要调用其它工具。"

	return mcp.CallToolResult{Content: []mcp.ContentItem{
		mcp.TextContentFor(ev.String(), "user", "assistant"),
		mcp.TextContentFor(instr, "assistant"),
	}}, nil
}
