package tools

import (
	"fmt"
	"strings"
)

// Deterministic multi-dimension diagnosis report renderer. Assembles the overview
// (reusing scoreBar / renderOverviewTable / renderDimBars) + cross-dimension核心问题
// + per-dimension evidence modules (waits / slow SQL / locks / dead tuples /
// indexes), all via the runewidth-aligned ASCII primitives. Every figure 实测.
//
// In the single-pass design this report is NOT rendered directly to the user;
// it is handed to the model (with accurate numbers) as the evidence + format
// reference, and the model produces ONE final report from it (see
// diagEvidenceWithTemplate). Keeping the deterministic render means the numbers
// the model copies are already correct.

func renderDiagnosisReport(d *DiagnosisData) string {
	var b strings.Builder
	r := &d.Health
	b.WriteString("# 🩺 数据库诊断 · " + r.Target + "\n\n")
	b.WriteString("> 多维实测(health · 等待 · 慢SQL · 锁 · 死元组 · 索引);评级由阈值确定性判定,标【实测】。\n\n")

	// ── 健康总览 ──
	b.WriteString("## 健康总览\n\n```\n")
	b.WriteString(scoreBar(r.Score, r.Grade) + "\n")
	if r.Version != "" {
		ver := strings.TrimLeft(r.Version, "( ")
		if i := strings.Index(ver, ") "); i > 0 {
			ver = ver[:i]
		}
		b.WriteString("版本  " + truncDisp(ver, 44) + "\n")
	}
	b.WriteString("\n" + renderOverviewTable(r))
	b.WriteString("```\n\n")

	// ── 维度健康度 ──
	b.WriteString("## 维度健康度\n\n```\n")
	b.WriteString(renderDimBars(r))
	b.WriteString("```\n\n")

	// ── 核心问题(跨维聚合)──
	probs := detectDiagProblems(d)
	b.WriteString("## 核心问题\n\n")
	if len(probs) == 0 {
		b.WriteString("✅ 多维扫描未发现 WARN / FAIL 级问题。\n\n")
	} else {
		b.WriteString("```\n")
		b.WriteString(renderDiagProblems(probs))
		b.WriteString("```\n\n")
	}

	// ── 证据模块 ──
	renderWaitSection(&b, d)
	renderSlowSQLSection(&b, d)
	renderLockSection(&b, d)
	renderDeadTopSection(&b, d)
	renderIdxSection(&b, d)

	// ── 诊断边界 ──
	b.WriteString("## 诊断边界\n\n")
	b.WriteString("- 慢SQL / Top SQL 为历史累计统计;当前快照若无活跃阻塞,非在线故障【实测】\n")
	if len(d.Warnings) > 0 {
		b.WriteString("- 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}

// renderDiagProblems renders the cross-dimension problem table + suggestion list.
func renderDiagProblems(ps []diagProblem) string {
	cols := []tableColumn{
		{Header: "级别"},
		{Header: "维度"},
		{Header: "问题", Max: 18},
		{Header: "证据", Max: 40},
	}
	var rows [][]string
	for _, p := range ps {
		rows = append(rows, []string{ratingText(p.Severity), p.Dim, p.Title, p.Evidence})
	}
	var b strings.Builder
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("\n建议:\n")
	for i, p := range ps {
		b.WriteString(fmt.Sprintf("%d. [%s] %s → %s\n", i+1, p.Dim, p.Title, p.Hint))
	}
	return b.String()
}

func renderWaitSection(b *strings.Builder, d *DiagnosisData) {
	if len(d.Waits) == 0 {
		b.WriteString("## 等待事件分布(当前快照)\n\n```\n无活跃会话(空载)。\n```\n\n")
		return
	}
	b.WriteString("## 等待事件分布(当前快照)\n\n```\n")
	for _, w := range d.Waits {
		label := truncDisp(w.Type+"·"+w.Event, 18)
		b.WriteString(barLine(label, fmt.Sprintf("%d", w.Count), w.Pct/100, 20, 18, 3, fmt.Sprintf("%.0f%%", w.Pct)) + "\n")
	}
	b.WriteString("```\n\n")
}

func renderSlowSQLSection(b *strings.Builder, d *DiagnosisData) {
	if len(d.SlowSQL) == 0 {
		b.WriteString("## Top 慢 SQL\n\n```\n无 >1s 的慢 SQL。\n```\n\n")
		return
	}
	cols := []tableColumn{
		{Header: "SQL_ID"},
		{Header: "平均ms", Right: true},
		{Header: "调用", Right: true},
		{Header: "总秒", Right: true},
		{Header: "命中%", Right: true},
	}
	var rows [][]string
	for _, s := range d.SlowSQL {
		rows = append(rows, []string{s.SQLID, s.AvgMS, s.Calls, s.TotalSec, s.CacheHit})
	}
	b.WriteString("## Top 慢 SQL\n\n```\n")
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n> 用 sqltune 对某条 SQL_ID 深度调优(自动校验改写/索引)。\n\n")
}

func renderLockSection(b *strings.Builder, d *DiagnosisData) {
	if len(d.LockChain) == 0 {
		b.WriteString("## 锁阻塞\n\n```\n当前无锁阻塞链。\n```\n\n")
		return
	}
	cols := []tableColumn{
		{Header: "被阻塞pid"},
		{Header: "阻塞源pid"},
		{Header: "被阻塞SQL", Max: 40},
	}
	var rows [][]string
	for _, l := range d.LockChain {
		rows = append(rows, []string{l.BlockedPID, l.BlockerPID, l.Query})
	}
	b.WriteString("## 锁阻塞\n\n```\n")
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n")
}

func renderDeadTopSection(b *strings.Builder, d *DiagnosisData) {
	if len(d.DeadTop) == 0 {
		return
	}
	cols := []tableColumn{
		{Header: "表", Max: 24},
		{Header: "死元组", Right: true},
		{Header: "存活", Right: true},
		{Header: "死占比%", Right: true},
	}
	var rows [][]string
	for _, t := range d.DeadTop {
		rows = append(rows, []string{t.Table, t.Dead, t.Live, t.Pct})
	}
	b.WriteString("## 死元组 Top 表\n\n```\n")
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n")
}

func renderIdxSection(b *strings.Builder, d *DiagnosisData) {
	b.WriteString(fmt.Sprintf("## 索引概况\n\n```\n未使用索引 %d 个 · 大索引(>10MB) %d 个\n```\n\n", d.IdxUnused, d.IdxLarge))
}

// diagEvidenceWithTemplate hands the model the deterministically-rendered,
// number-accurate 6-dimension evidence (via renderDiagnosisReport) PLUS a soft
// format reference and an instruction to produce ONE complete diagnosis report
// directly to the user. Single-pass: the model copies the accurate numbers and
// adds the analysis (root-cause causal chains + prioritized actions) in its own
// reply — no second tool call, no rigid schema, room to improvise.
func diagEvidenceWithTemplate(d *DiagnosisData) string {
	var b strings.Builder
	b.WriteString("【任务】下面是插件确定性采集并渲染的 6 维实测证据(数字 100% 准确)。请你以它为基础,直接产出一份完整、浑然一体的数据库诊断报告给用户 —— 一轮写完,不要调用其它工具,也不要复述本说明。\n\n")
	b.WriteString("=== 实测证据(数字与表格可直接引用,不得改动)===\n\n")
	b.WriteString(renderDiagnosisReport(d))
	b.WriteString("\n=== 在上面证据基础上发挥 ===\n")
	b.WriteString("- 总览与证据表格:保留或精炼,数字以上面为准,严禁编造或改动;\n")
	b.WriteString("- 补充【根因分析】:每条给 现象 → 中间机制 → 根因 的因果链,尽量跨维关联(如 死元组堆积 ↔ autovacuum 滞后 ↔ 表膨胀 ↔ 慢SQL;命中率低 ↔ 大表全扫 ↔ IO);\n")
	b.WriteString("- 补充【优化方案】:按 P0/P1/P2 优先级,每条给可执行 SQL/命令 + 风险 + 前置检查 + 回滚;\n")
	b.WriteString("- 标注:实测数据标【实测】,你的推断标【AI推断】,便于人工复核;\n")
	b.WriteString("- 上面的章节/表格只是格式参考,结构、详略、emoji 可按你的判断自由调整;\n")
	b.WriteString("- 中文输出,markdown 即可(codexgo 会渲染表格,优先用 markdown 表格)。\n")
	return b.String()
}
