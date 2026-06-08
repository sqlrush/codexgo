package tools

import (
	"fmt"
	"sort"
	"strings"
)

// Deterministic SQL Tuning Report renderer. Ports opendb's renderFallbackReport
// to a self-contained markdown report the plugin produces itself — so every
// FACT in it (plan shape, costs, hotspots, schema, verified candidates) is
// deterministic and accurate, never LLM-invented.
//
// Two rendering rules driven by the codexgo TUI:
//   - The TUI does NOT render markdown tables (| a | b |) — they collapse into a
//     garbled line. So all tabular data is emitted as fixed-width text blocks
//     inside ``` fences, which the TUI shows verbatim (matches opendb's look).
//   - Section headers use markdown ##, short prose uses plain lines.
//
// The model's job afterwards is to PRESENT this report (optionally adding a CBO
// narrative and extra rewrites) — but any rewrite it proposes must be re-verified
// via this tool, and any number it cites must come from this report. Facts here
// are 【实测】; anything the model infers must be marked 【AI推断】.

const hotspotLimit = 12

// reportMarker is a [Pn] tag bound to a plan node.
type reportMarker struct {
	ref    string // "[P1]"
	node   *PlanNode
	reason string
	score  float64
}

// renderTuneReport renders the full deterministic report as markdown.
func renderTuneReport(r *TuneReport) string {
	var b strings.Builder

	b.WriteString("# SQL 调优报告\n\n")
	b.WriteString("> 本报告由插件确定性渲染:执行计划 / 代价热点 / 表索引统计 / 候选验证均为真实采集,标【实测】;模型补充的根因或改写须标【AI推断】并经校验。\n\n")

	// Build [Pn] markers from plan hotspots (shared by §2 and §3).
	markers := buildReportMarkers(r.PlanTree)
	byNode := map[*PlanNode]reportMarker{}
	for _, m := range markers {
		byNode[m.node] = m
	}

	renderInputSQL(&b, r)
	renderPlanSection(&b, r, byNode, markers)
	renderEvidenceSection(&b, r, markers)
	renderCBOSection(&b, r)
	renderPlansSection(&b, r)
	renderRejectedSection(&b, r)
	renderUncertaintySection(&b, r)

	return b.String()
}

// ---- §1 输入 SQL ----

func renderInputSQL(b *strings.Builder, r *TuneReport) {
	b.WriteString("## 1. 输入 SQL\n\n")
	sql := r.EffectiveSQL
	if strings.TrimSpace(sql) == "" {
		sql = r.Resolved.Query
	}
	if r.Resolved.Source != "" {
		b.WriteString(fmt.Sprintf("来源: %s", r.Resolved.Source))
		if r.Resolved.Schema != "" {
			b.WriteString(" · schema: " + r.Resolved.Schema)
		}
		if len(r.BindFills) > 0 {
			b.WriteString(fmt.Sprintf(" · 已回填 %d 个占位符(样例值,仅供 EXPLAIN)", len(r.BindFills)))
		}
		b.WriteString("\n\n")
	}
	b.WriteString("```sql\n")
	b.WriteString(strings.TrimSpace(sql))
	b.WriteString("\n```\n\n")
}

// ---- §2 执行计划 ----

func renderPlanSection(b *strings.Builder, r *TuneReport, byNode map[*PlanNode]reportMarker, markers []reportMarker) {
	if r.PlanTree == nil || r.PlanTree.Root == nil {
		return
	}
	b.WriteString("## 2. 执行计划\n\n")
	b.WriteString(fmt.Sprintf("Total cost: %.2f", r.PlanTree.TotalCost))
	if r.PlanTree.HasAnalyze {
		b.WriteString(" · 含 ANALYZE 实测")
		if r.PlanTree.ExecutionTime > 0 {
			b.WriteString(fmt.Sprintf(" · 执行 %.1fms", r.PlanTree.ExecutionTime))
		}
	} else {
		b.WriteString(" · 估算计划(未执行查询)")
	}
	b.WriteString("\n\n```plan\n")
	renderPlanNodeText(b, r.PlanTree.Root, byNode, 0)
	b.WriteString("```\n\n")

	if len(markers) > 0 {
		b.WriteString("标注说明:\n\n```\n")
		for _, m := range markers {
			rel := "-"
			if m.node.Relation != "" {
				rel = m.node.Relation
			}
			b.WriteString(fmt.Sprintf("%s %s on %s — %s\n", m.ref, m.node.Operator, rel, m.reason))
		}
		b.WriteString("```\n\n")
	}
}

// renderPlanNodeText renders the tree with 2-space indent, "- " child marker and
// [Pn] tags. Details (Filter/Cond/Sort Key) are shown for marked nodes or depth<=1.
func renderPlanNodeText(b *strings.Builder, n *PlanNode, byNode map[*PlanNode]reportMarker, depth int) {
	if n == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	prefix := ""
	if depth > 0 {
		prefix = "- "
	}
	tag := ""
	if m, ok := byNode[n]; ok {
		tag = m.ref + " "
	}
	b.WriteString(indent + prefix + tag + planNodeSummary(n) + "\n")

	_, marked := byNode[n]
	if marked || depth <= 1 {
		for _, d := range planNodeDetails(n) {
			b.WriteString(indent + "    " + d + "\n")
		}
	}
	for _, c := range n.Children {
		renderPlanNodeText(b, c, byNode, depth+1)
	}
}

func planNodeSummary(n *PlanNode) string {
	parts := []string{n.Operator}
	if n.Relation != "" {
		rel := n.Relation
		if n.Alias != "" && n.Alias != n.Relation {
			rel += " " + n.Alias
		}
		parts = append(parts, "on "+rel)
	}
	parts = append(parts, fmt.Sprintf("cost=%.0f", n.TotalCost))
	if n.PlanRows > 0 {
		parts = append(parts, fmt.Sprintf("rows=%d", n.PlanRows))
	}
	if n.ActualRows > 0 {
		parts = append(parts, fmt.Sprintf("actual_rows=%d", n.ActualRows))
	}
	return strings.Join(parts, " ")
}

func planNodeDetails(n *PlanNode) []string {
	var details []string
	for _, item := range []struct{ label, value string }{
		{"Filter", n.Filter},
		{"Index Cond", n.IndexCond},
		{"Hash Cond", n.HashCond},
		{"Join Filter", n.JoinFilter},
	} {
		if v := strings.TrimSpace(item.value); v != "" {
			details = append(details, item.label+": "+truncate(v, 160))
		}
	}
	if len(n.SortKey) > 0 {
		details = append(details, "Sort Key: "+truncate(strings.Join(n.SortKey, ", "), 160))
	}
	if n.SortSpaceType != "" && !strings.EqualFold(n.SortSpaceType, "Memory") {
		details = append(details, fmt.Sprintf("Sort: method=%s space=%s %dKB", n.SortMethod, n.SortSpaceType, n.SortSpaceUsed))
	}
	return details
}

// ---- §3 关键证据 ----

func renderEvidenceSection(b *strings.Builder, r *TuneReport, markers []reportMarker) {
	b.WriteString("## 3. 关键证据\n\n")

	// 代价热点
	if len(markers) > 0 {
		b.WriteString("### 代价热点\n\n```\n")
		for i, m := range markers {
			rel := "-"
			if m.node.Relation != "" {
				rel = m.node.Relation
			}
			b.WriteString(fmt.Sprintf("%d/%d %s\n", i+1, len(markers), m.ref))
			b.WriteString(fmt.Sprintf("  算子 : %s\n", m.node.Operator))
			b.WriteString(fmt.Sprintf("  对象 : %s\n", rel))
			b.WriteString(fmt.Sprintf("  cost : %.0f\n", m.node.TotalCost))
			b.WriteString(fmt.Sprintf("  说明 : %s\n", m.reason))
			if i < len(markers)-1 {
				b.WriteString("\n")
			}
		}
		b.WriteString("```\n\n")
	}

	// 表 / 索引 / 统计信息
	blocks := renderSchemaBlocks(r.Schema)
	if blocks != "" {
		b.WriteString("### 表 / 索引 / 统计信息\n\n```\n")
		b.WriteString(blocks)
		b.WriteString("```\n\n")
	}
}

// renderSchemaBlocks joins the Tables/Indexes/Stats sections per table into
// fixed-width blocks (no markdown table).
func renderSchemaBlocks(sc SchemaContext) string {
	tables := indexRowsBy(sc.Tables, "table_name")
	if len(tables) == 0 {
		return ""
	}
	idxByTable := groupRowsBy(sc.Indexes, "table_name")
	statsByTable := indexRowsBy(sc.Stats, "table_name")

	// Stable order: as they appear in Tables (already size-desc).
	var order []string
	seen := map[string]bool{}
	for _, row := range sc.Tables.Rows {
		name := cell(sc.Tables.Columns, row, "table_name")
		if name != "" && !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
	}

	var b strings.Builder
	for i, full := range order {
		short := full
		if dot := strings.LastIndexByte(full, '.'); dot >= 0 {
			short = full[dot+1:]
		}
		trow := tables[full]
		b.WriteString(fmt.Sprintf("%d/%d %s\n", i+1, len(order), short))
		b.WriteString(fmt.Sprintf("  行数 : %s\n", cellOr(sc.Tables.Columns, trow, "est_rows", "-")))
		b.WriteString(fmt.Sprintf("  大小 : %s\n", cellOr(sc.Tables.Columns, trow, "total_size", "-")))
		b.WriteString("  索引 : " + formatIndexLine(sc.Indexes.Columns, idxByTable[full]) + "\n")
		b.WriteString("  统计 : " + formatStatsLine(sc.Stats.Columns, statsByTable[full]) + "\n")
		if i < len(order)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func formatIndexLine(cols []string, rows [][]string) string {
	if len(rows) == 0 {
		return "未采集到索引"
	}
	var parts []string
	for _, row := range rows {
		name := cell(cols, row, "index_name")
		scans := cell(cols, row, "scans")
		seg := name
		if scans != "" {
			seg += fmt.Sprintf("(scans=%s)", scans)
		}
		parts = append(parts, seg)
		if len(parts) >= 4 {
			break
		}
	}
	if len(rows) > 4 {
		parts = append(parts, fmt.Sprintf("另 %d 个", len(rows)-4))
	}
	return strings.Join(parts, "; ")
}

func formatStatsLine(cols []string, row []string) string {
	if len(row) == 0 {
		return "未采集到统计"
	}
	live := cell(cols, row, "live_rows")
	dead := cell(cols, row, "dead_rows")
	last := cell(cols, row, "last_analyze")
	return fmt.Sprintf("live=%s dead=%s last_analyze=%s", nz(live), nz(dead), nz(last))
}

// ---- §4 CBO 分析(确定性摘要) ----

func renderCBOSection(b *strings.Builder, r *TuneReport) {
	b.WriteString("## 4. CBO 分析\n\n")
	b.WriteString(deterministicCBO(r) + "\n\n")
	b.WriteString("> 【实测】以上为执行计划事实摘要。模型可补充根因叙述(为何走此计划、是否可 sargable 等),但须标【AI推断】,且引用的 cost/rows 必须与上方执行计划一致。\n\n")
}

func deterministicCBO(r *TuneReport) string {
	if r.PlanTree == nil || r.PlanTree.Root == nil {
		return "未取得可用执行计划,无法生成 CBO 摘要。"
	}
	hot := topRelationNodes(r.PlanTree.Root, 5)
	if len(hot) == 0 {
		return fmt.Sprintf("执行计划 total_cost=%.2f;未识别到明确高成本关系节点。", r.PlanTree.TotalCost)
	}
	var parts []string
	for _, n := range hot {
		name := n.Operator
		if n.Relation != "" {
			name += " on " + n.Relation
		}
		parts = append(parts, fmt.Sprintf("%s cost=%.0f rows=%d", name, n.TotalCost, n.PlanRows))
	}
	issues := ""
	if kinds := distinctIssueKinds(r); kinds != "" {
		issues = "检测到的反模式: " + kinds + "。"
	}
	return fmt.Sprintf("确定性 CBO 摘要: total_cost=%.2f;高成本关系节点 %s。%s优先检查这些节点的过滤列/连接列索引与统计信息。",
		r.PlanTree.TotalCost, strings.Join(parts, ";"), issues)
}

// ---- §5 优化方案 ----

func renderPlansSection(b *strings.Builder, r *TuneReport) {
	b.WriteString("## 5. 优化方案\n\n")

	n := 0
	for _, c := range r.Candidates {
		if !candidateAccepted(c) {
			continue // rejected ones go to §6
		}
		n++
		renderOnePlan(b, n, c)
	}

	// gs_index_advise as an engine-sourced index plan.
	if len(r.IndexAdvice.Rows) > 0 {
		n++
		b.WriteString(fmt.Sprintf("### 方案 %d: 索引建议(引擎 gs_index_advise)\n\n", n))
		b.WriteString("依据: openGauss 内置索引顾问输出(需结合读写比与现有索引去重)\n\n```\n")
		for _, row := range r.IndexAdvice.Rows {
			b.WriteString(strings.TrimRight(strings.Join(row, "  "), " ") + "\n")
		}
		b.WriteString("```\n\n")
	}

	// SQL anti-patterns as actionable findings.
	if len(r.SQLIssues) > 0 {
		b.WriteString("### SQL 反模式(确定性检测)\n\n```\n")
		for _, is := range r.SQLIssues {
			b.WriteString(fmt.Sprintf("- %s: %s → %s\n", is.Kind, is.Detail, is.Suggestion))
		}
		b.WriteString("```\n\n")
	}

	if n == 0 && len(r.SQLIssues) == 0 {
		b.WriteString("未生成可直接采纳的确定性方案;请结合上方代价热点 / 反模式与模型补充的改写(须经校验)。\n\n")
	}
}

func renderOnePlan(b *strings.Builder, n int, c RewriteCandidate) {
	b.WriteString(fmt.Sprintf("### 方案 %d: %s\n\n", n, candidateTitle(c)))
	if note := strings.TrimSpace(c.Note); note != "" {
		b.WriteString("依据: " + note + "\n\n")
	}
	b.WriteString("```sql\n" + strings.TrimSpace(c.SQL) + "\n```\n\n")
	b.WriteString(verdictLine(c) + "\n\n")
}

// verdictLine renders the deterministic verification verdict for a candidate.
func verdictLine(c RewriteCandidate) string {
	var parts []string
	if c.BeforeCost != nil && c.AfterCost != nil {
		ratio := 0.0
		if c.CostRatio != nil {
			ratio = *c.CostRatio
		}
		parts = append(parts, fmt.Sprintf("【实测】cost %.0f → %.0f (%.3f×)", *c.BeforeCost, *c.AfterCost, ratio))
	}
	switch {
	case c.Equivalent == "yes":
		parts = append(parts, "等价性: 抽样通过 ✓")
	case c.Equivalent == "no":
		parts = append(parts, "等价性: 不通过 ✗(语义已变,需人工复核)")
	case strings.HasPrefix(c.Equivalent, "inconclusive"):
		parts = append(parts, "等价性: "+c.Equivalent)
	case strings.HasPrefix(c.Equivalent, "unverified"), strings.HasPrefix(c.Equivalent, "skipped"):
		parts = append(parts, "等价性: 未校验(传 verify_equiv=true 可校验)")
	}
	if len(parts) == 0 {
		return "【实测】未获取验证数据。"
	}
	return strings.Join(parts, " · ")
}

// ---- §6 模型候选被拒绝(调试) ----

func renderRejectedSection(b *strings.Builder, r *TuneReport) {
	var rejected []RewriteCandidate
	for _, c := range r.Candidates {
		if !candidateAccepted(c) {
			rejected = append(rejected, c)
		}
	}
	if len(rejected) == 0 {
		return
	}
	b.WriteString("## 6. 候选被拒 / 待确认(调试)\n\n```\n")
	for _, c := range rejected {
		b.WriteString(fmt.Sprintf("- %s: %s\n", candidateTitle(c), firstLine(nz(c.Note))))
		b.WriteString("  原因: " + rejectReason(c) + "\n")
	}
	b.WriteString("```\n\n")
}

// ---- §不确定的点(确定性规则) ----

func renderUncertaintySection(b *strings.Builder, r *TuneReport) {
	var notes []string
	for _, is := range r.SQLIssues {
		switch is.Kind {
		case "function_on_column":
			notes = append(notes, "存在列上函数(如 UPPER/TO_CHAR/CAST),普通 B-tree 索引会失效;openGauss 对表达式索引/GIN 支持因版本而异,落地前需确认。")
		case "leading_wildcard_like":
			notes = append(notes, "LIKE 前导通配符('%...')无法走 B-tree 索引;考虑全文检索或改写过滤。")
		}
	}
	if len(r.BindFills) > 0 {
		notes = append(notes, "执行计划基于回填的样例值;真实业务取值不同会改变选择率与计划,等价校验结论仅对样例值成立。")
	}
	if len(notes) == 0 {
		return
	}
	notes = dedupStrings(notes)
	b.WriteString("## 不确定的点\n\n")
	for _, n := range notes {
		b.WriteString("- " + n + "\n")
	}
	b.WriteString("\n> 模型如补充其它不确定点,请标【AI推断】。\n")
}

// ---- markers / hotspots ----

// buildReportMarkers scores plan nodes and assigns [P1..Pn] to the top-N
// hotspots (score desc). Ports opendb's buildPlanHotspots scoring.
func buildReportMarkers(plan *PlanInfo) []reportMarker {
	if plan == nil || plan.Root == nil {
		return nil
	}
	var items []reportMarker
	walkPlan(plan.Root, func(n *PlanNode) {
		if !nodeWorthAnnotating(n) {
			return
		}
		score := n.TotalCost
		reason := planMarkerReason(n)
		if strings.Contains(strings.ToLower(n.Operator), "seq scan") && n.PlanRows > 10000 {
			score *= 1.3
			reason = "大行数顺序扫描,优先核对过滤列/连接列索引与统计信息"
		}
		if n.SortSpaceType != "" && !strings.EqualFold(n.SortSpaceType, "Memory") {
			score *= 1.2
			reason = "排序落盘,检查 work_mem、排序键索引或 LIMIT 下推"
		}
		if n.ActualRows > 0 && n.PlanRows > 0 {
			ratio := float64(n.ActualRows) / float64(n.PlanRows)
			if ratio > 10 || ratio < 0.1 {
				score *= 1.5
				reason = "估算行数与实际行数偏差明显,优先修复统计信息"
			}
		}
		items = append(items, reportMarker{node: n, reason: reason, score: score})
	})
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].node.Operator < items[j].node.Operator
	})
	if len(items) > hotspotLimit {
		items = items[:hotspotLimit]
	}
	for i := range items {
		items[i].ref = fmt.Sprintf("[P%d]", i+1)
	}
	return items
}

// nodeWorthAnnotating gates which nodes can become hotspots. Ports opendb's
// isPlanNodeWorthAnnotating + isAccessPathWorthTuning thresholds.
func nodeWorthAnnotating(n *PlanNode) bool {
	op := strings.ToLower(n.Operator)
	if strings.Contains(op, "seq scan") {
		return n.TotalCost >= 100 || n.PlanRows >= 1000
	}
	if strings.Contains(op, "bitmap heap") {
		return n.TotalCost >= 1000
	}
	return strings.Contains(op, "sort") || strings.Contains(op, "hash join") || strings.Contains(op, "nested loop")
}

// planMarkerReason maps an operator to its diagnostic sentence. Ports opendb's
// planMarkerReason switch verbatim.
func planMarkerReason(n *PlanNode) string {
	op := strings.ToLower(n.Operator)
	switch {
	case strings.Contains(op, "seq scan"):
		return "高成本顺序扫描,优先检查过滤列/连接列索引与统计信息"
	case strings.Contains(op, "bitmap heap"):
		return "Bitmap Heap 访问成本高,检查索引覆盖度与过滤选择性"
	case strings.Contains(op, "sort"):
		return "排序节点成本高,检查排序键索引、LIMIT 下推或 work_mem"
	case strings.Contains(op, "hash join"):
		return "Hash Join 成本高,检查构建端行数、连接列索引和统计信息"
	case strings.Contains(op, "nested loop"):
		return "Nested Loop 成本高,检查内层是否可用连接列索引"
	default:
		return "该节点与优化方案涉及的表或算子相关"
	}
}

// topRelationNodes returns the top-N relation nodes by cost (worth-tuning gated).
func topRelationNodes(root *PlanNode, limit int) []*PlanNode {
	var nodes []*PlanNode
	walkPlan(root, func(n *PlanNode) {
		if n.Relation != "" && nodeWorthAnnotating(n) {
			nodes = append(nodes, n)
		}
	})
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].TotalCost > nodes[j].TotalCost })
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	return nodes
}

// ---- candidate helpers ----

// candidateAccepted reports whether a candidate is a ready-to-apply plan.
// Accepted = cost improved (ratio >= 1.3, i.e. >=30%) AND not proven non-equivalent.
func candidateAccepted(c RewriteCandidate) bool {
	if c.Equivalent == "no" {
		return false
	}
	if c.CostRatio == nil {
		return false
	}
	return *c.CostRatio >= 1.3
}

func rejectReason(c RewriteCandidate) string {
	if c.Equivalent == "no" {
		return "等价性不通过(语义已变,需人工复核)"
	}
	if c.CostRatio == nil {
		return "缺少 cost 验证"
	}
	if *c.CostRatio < 1.3 {
		return fmt.Sprintf("cost 改善不足 30%%,当前 %.3f×", *c.CostRatio)
	}
	return "待确认"
}

func candidateTitle(c RewriteCandidate) string {
	switch c.Rule {
	case "remove_redundant_distinct":
		return "SQL 改写(移除冗余 DISTINCT)"
	case "caller_supplied":
		return "SQL 改写(模型提供)"
	default:
		if c.Rule != "" {
			return "SQL 改写(" + c.Rule + ")"
		}
		return "优化建议"
	}
}

func distinctIssueKinds(r *TuneReport) string {
	seen := map[string]bool{}
	var kinds []string
	for _, is := range r.SQLIssues {
		if !seen[is.Kind] {
			seen[is.Kind] = true
			kinds = append(kinds, is.Kind)
		}
	}
	for _, is := range r.PlanIssues {
		if !seen[is.Kind] {
			seen[is.Kind] = true
			kinds = append(kinds, is.Kind)
		}
	}
	return strings.Join(kinds, ", ")
}

// ---- small utilities ----

// cell returns the value at the named column for a row, or "".
func cell(cols, row []string, name string) string {
	for i, c := range cols {
		if c == name && i < len(row) {
			return row[i]
		}
	}
	return ""
}

func cellOr(cols, row []string, name, dflt string) string {
	if v := cell(cols, row, name); v != "" {
		return v
	}
	return dflt
}

// indexRowsBy maps the first row per key value.
func indexRowsBy(tr TableReport, key string) map[string][]string {
	out := map[string][]string{}
	for _, row := range tr.Rows {
		k := cell(tr.Columns, row, key)
		if k != "" {
			if _, ok := out[k]; !ok {
				out[k] = row
			}
		}
	}
	return out
}

// groupRowsBy groups all rows per key value.
func groupRowsBy(tr TableReport, key string) map[string][][]string {
	out := map[string][][]string{}
	for _, row := range tr.Rows {
		k := cell(tr.Columns, row, key)
		if k != "" {
			out[k] = append(out[k], row)
		}
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func nz(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
