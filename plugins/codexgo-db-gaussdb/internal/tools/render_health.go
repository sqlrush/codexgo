package tools

import (
	"fmt"
	"sort"
	"strings"
)

// Health rendering helpers, shared by the multi-dimension diagnosis renderer
// (render_diagnose.go): the score bar, the category overview table, the
// per-dimension bar chart, and the category/status helpers. All output uses the
// runewidth-aligned ASCII primitives so it renders aligned in codexgo.

// healthCategoryOrder is the display order of health modules.
var healthCategoryOrder = []string{"实例", "连接", "内存", "维护", "高可用"}

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
