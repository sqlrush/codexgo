package tools

import (
	"strings"
	"testing"
)

func fptr(v float64) *float64 { return &v }

// TestVerdictBadge checks the adoption tag derived from the verification verdict.
func TestVerdictBadge(t *testing.T) {
	cases := []struct {
		name string
		c    RewriteCandidate
		want string
	}{
		{"adopt", RewriteCandidate{CostRatio: fptr(1.5), Equivalent: "yes"}, "✅【可直接采纳】"},
		{"semantic-change", RewriteCandidate{CostRatio: fptr(1.5), Equivalent: "no"}, "⚠️【需人工确认】"},
		{"inconclusive", RewriteCandidate{CostRatio: fptr(1.5), Equivalent: "inconclusive:空"}, "⚠️【需人工确认】"},
		{"no-improve", RewriteCandidate{CostRatio: fptr(1.0), Equivalent: "yes"}, "⚠️【需人工确认】"},
	}
	for _, c := range cases {
		if got := verdictBadge(c.c); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: verdictBadge=%q want prefix %q", c.name, got, c.want)
		}
	}
}

// TestRenderAnalysisReport checks the pass-2 report has the fixed sections and
// marks verified vs inferred content.
func TestRenderAnalysisReport(t *testing.T) {
	r := &TuneReport{
		IndexAdvice: TableReport{Columns: []string{"schema", "table", "cols"}, Rows: [][]string{{"sqltune_demo", "orders", "order_date"}}},
	}
	a := &AnalysisInput{RootCause: "函数列导致索引失效", Expected: "转为索引扫描"}
	verified := []VerifiedRewrite{{
		In:   AnalysisRewrite{Title: "去DISTINCT", SQL: "SELECT 1", Reasons: []string{"冗余"}},
		Cand: RewriteCandidate{BeforeCost: fptr(100), AfterCost: fptr(50), CostRatio: fptr(2.0), Equivalent: "yes"},
	}}
	md := renderAnalysisReport(r, a, verified)
	for _, want := range []string{"# 优化方案", "## 根因分析", "【AI推断】函数列", "去DISTINCT", "✅【可直接采纳】", "## 预期效果"} {
		if !strings.Contains(md, want) {
			t.Errorf("report missing %q\n%s", want, md)
		}
	}
}

func TestIndexAdviceMentionsTable(t *testing.T) {
	advice := TableReport{Columns: []string{"schema", "table", "cols"}, Rows: [][]string{{"sqltune_demo", "orders", "order_date"}}}
	if !indexAdviceMentionsTable(advice, "CREATE INDEX i ON orders(order_date)") {
		t.Error("should match orders")
	}
	if indexAdviceMentionsTable(advice, "CREATE INDEX i ON unrelated(x)") {
		t.Error("should not match unrelated table")
	}
}
