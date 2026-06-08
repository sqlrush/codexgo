package tools

import (
	"fmt"
	"sort"
	"strings"
)

// Deterministic health-diagnosis report renderer (pass-1). Turns the structured
// HealthReport into a fixed-format markdown report using the runewidth-aligned
// ASCII primitives (asciiTable / barLine). Every figure is real【实测】.
//
// Visual elements (all verified to render aligned in codexgo): score bar,
// overview table, dimension bar chart, problem table, suggestion list.

// healthCategoryOrder is the display order of health modules.
var healthCategoryOrder = []string{"实例", "连接", "内存", "维护", "高可用"}

// healthAssistantSummary is the terse assistant-audience digest: the user
// already sees the rendered report, so the model just confirms it and can go
// deeper on follow-ups — it must not rebuild the report.
func healthAssistantSummary(r *HealthReport) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("健康诊断报告已直接展示给用户(确定性渲染),评分 %d/100 %s。", r.Score, r.Grade))
	if probs := healthProblems(r); len(probs) > 0 {
		var names []string
		for _, p := range probs {
			names = append(names, p.Name)
		}
		b.WriteString("需关注:" + strings.Join(names, "、") + "。")
	} else {
		b.WriteString("无 WARN/FAIL 级问题。")
	}
	b.WriteString("勿重复报告内容;可一句话收尾或按用户后续问题深入。")
	return b.String()
}

func renderHealthReport(r *HealthReport) string {
	var b strings.Builder
	b.WriteString("# 🩺 数据库健康诊断 · " + r.Target + "\n\n")
	b.WriteString("> 指标均为插件实测【实测】;评级由阈值确定性判定。\n\n")

	// ── 评分 + 总览表 ──
	b.WriteString("## 健康总览\n\n```\n")
	b.WriteString(scoreBar(r.Score, r.Grade) + "\n")
	if r.Version != "" {
		ver := strings.TrimLeft(r.Version, "( ")
		if i := strings.Index(ver, ") "); i > 0 {
			ver = ver[:i]
		}
		b.WriteString("版本  " + truncDisp(ver, 44) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(renderOverviewTable(r))
	b.WriteString("```\n\n")

	// ── 维度健康度对比 ──
	b.WriteString("## 维度健康度\n\n```\n")
	b.WriteString(renderDimBars(r))
	b.WriteString("```\n\n")

	// ── 核心问题 ──
	probs := healthProblems(r)
	b.WriteString("## 核心问题\n\n")
	if len(probs) == 0 {
		b.WriteString("✅ 未发现 WARN / FAIL 级问题。\n\n")
	} else {
		b.WriteString("```\n")
		b.WriteString(renderProblemTable(probs))
		b.WriteString("```\n\n")
	}

	// ── 修复建议 ──
	if len(probs) > 0 {
		b.WriteString("## 修复建议\n\n```\n")
		b.WriteString(renderSuggestions(probs))
		b.WriteString("```\n")
	}

	return b.String()
}

// scoreBar renders "评分  ███████░░░  94/100  良".
func scoreBar(score int, grade string) string {
	const w = 20
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	filled := score * w / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
	return fmt.Sprintf("评分  %s  %d/100  %s", bar, score, grade)
}

// renderOverviewTable groups items by category into 模块/评级/关键观测.
func renderOverviewTable(r *HealthReport) string {
	byCat := map[string][]HealthItem{}
	for _, it := range r.Items {
		byCat[it.Category] = append(byCat[it.Category], it)
	}
	cols := []tableColumn{
		{Header: "模块"},
		{Header: "评级"},
		{Header: "关键观测", Max: 38},
	}
	var rows [][]string
	for _, cat := range categoriesInOrder(byCat) {
		items := byCat[cat]
		rows = append(rows, []string{cat, ratingText(worstStatus(items)), observeSummary(items)})
	}
	return asciiTable(cols, rows)
}

// renderDimBars renders a per-category health-score bar chart.
func renderDimBars(r *HealthReport) string {
	byCat := map[string][]HealthItem{}
	for _, it := range r.Items {
		byCat[it.Category] = append(byCat[it.Category], it)
	}
	var b strings.Builder
	for _, cat := range categoriesInOrder(byCat) {
		score := dimScore(byCat[cat])
		tag := ""
		switch {
		case score <= 20:
			tag = "严重"
		case score < 100:
			tag = "警告"
		}
		b.WriteString(barLine(cat, fmt.Sprintf("%d", score), float64(score)/100, 20, 6, 4, tag) + "\n")
	}
	return b.String()
}

// renderProblemTable lists WARN/FAIL items.
func renderProblemTable(probs []HealthItem) string {
	cols := []tableColumn{
		{Header: "级别"},
		{Header: "检查项", Max: 22},
		{Header: "实测", Max: 20},
		{Header: "阈值", Max: 18},
	}
	var rows [][]string
	for _, p := range probs {
		// Plain text in the cell (NO emoji — its width varies by terminal and
		// would break box alignment); emoji is used in the suggestion list below.
		rows = append(rows, []string{ratingText(p.Status), p.Name, p.Value, p.Threshold})
	}
	return asciiTable(cols, rows)
}

// renderSuggestions renders a numbered suggestion list (worst first).
func renderSuggestions(probs []HealthItem) string {
	var b strings.Builder
	for i, p := range probs {
		s := p.Suggestion
		if strings.TrimSpace(s) == "" {
			s = "结合相关工具进一步排查"
		}
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n   → %s\n", i+1, severityText(p.Status), p.Name, s))
	}
	return b.String()
}

// --- helpers ---------------------------------------------------------------

func categoriesInOrder(byCat map[string][]HealthItem) []string {
	var out []string
	seen := map[string]bool{}
	for _, cat := range healthCategoryOrder {
		if len(byCat[cat]) > 0 {
			out = append(out, cat)
			seen[cat] = true
		}
	}
	// any category not in the fixed order, appended alphabetically
	var extra []string
	for cat := range byCat {
		if !seen[cat] {
			extra = append(extra, cat)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// worstStatus returns the most severe status among items.
func worstStatus(items []HealthItem) string {
	rank := map[string]int{statusOK: 0, statusUnknown: 1, statusWarn: 2, statusFail: 3}
	worst := statusOK
	for _, it := range items {
		if rank[it.Status] > rank[worst] {
			worst = it.Status
		}
	}
	return worst
}

func ratingText(status string) string {
	switch status {
	case statusFail:
		return "严重"
	case statusWarn:
		return "警告"
	case statusUnknown:
		return "未知"
	default:
		return "正常"
	}
}

func severityText(status string) string {
	switch status {
	case statusFail:
		return "🔴严重"
	case statusWarn:
		return "🟡警告"
	default:
		return status
	}
}

// observeSummary joins the most informative items for a category, each as
// "<short-name> <value>" so the observation is self-explaining.
func observeSummary(items []HealthItem) string {
	var parts []string
	for _, it := range items {
		v := strings.TrimSpace(it.Value)
		if v == "" || it.Status == statusUnknown {
			continue
		}
		parts = append(parts, shortName(it.Name)+" "+v)
		if len(parts) >= 2 {
			break
		}
	}
	return strings.Join(parts, " · ")
}

// shortName abbreviates a check name for the compact overview column.
func shortName(name string) string {
	switch name {
	case "运行时长":
		return "运行"
	case "连接使用率":
		return "连接"
	case "活动会话":
		return "活跃"
	case "事务中空闲(idle in tx)":
		return "空闲事务"
	case "缓存命中率":
		return "命中率"
	case "死元组总数":
		return "死元组"
	case "事务ID回卷风险":
		return "回卷"
	case "备库连接数":
		return "备库"
	default:
		return name
	}
}

// dimScore maps a category's worst status to a 0-100 health score.
func dimScore(items []HealthItem) int {
	switch worstStatus(items) {
	case statusFail:
		return 20
	case statusWarn:
		return 60
	case statusUnknown:
		return 50
	default:
		return 100
	}
}

// healthProblems returns WARN/FAIL items, most severe first.
func healthProblems(r *HealthReport) []HealthItem {
	var out []HealthItem
	for _, it := range r.Items {
		if it.Status == statusWarn || it.Status == statusFail {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return statusRank(out[i].Status) > statusRank(out[j].Status)
	})
	return out
}

func statusRank(s string) int {
	if s == statusFail {
		return 2
	}
	if s == statusWarn {
		return 1
	}
	return 0
}
