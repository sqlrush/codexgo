package tools

import (
	"strings"
	"testing"
)

// TestTuneAnalysisInstruction verifies the one-round infer+verify guidance:
// first call (no model rewrites) tells the model to re-call sqltune with
// candidates; once verified candidates are present it tells the model to write
// the report (实测/推测), not to verify again.
func TestTuneAnalysisInstruction(t *testing.T) {
	out := tuneAnalysisInstruction(&TuneReport{Target: "x"})
	for _, want := range []string{
		"勿重复", "[P1..Pn] 热点", "## 根因分析", "## 优化方案",
		"再次调用 sqltune", "candidates",
		"【实测】", "【推测】",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("first-call instruction missing %q", want)
		}
	}
	out2 := tuneAnalysisInstruction(&TuneReport{Target: "x", Candidates: []RewriteCandidate{{Rule: "model_rewrite", SQL: "SELECT 1"}}})
	if !strings.Contains(out2, "已在上面") || strings.Contains(out2, "再次调用 sqltune") {
		t.Errorf("verified-state instruction wrong:\n%s", out2)
	}
}

// TestRenderTuneReportHasPlanSection verifies the deterministic evidence report
// keeps input SQL before the cost-hotspot evidence (stable section ordering).
func TestRenderTuneReportHasPlanSection(t *testing.T) {
	out := renderTuneReport(&TuneReport{
		Target:       "x",
		EffectiveSQL: "SELECT * FROM orders WHERE id = 1",
		Resolved:     SQLFetchResult{Query: "SELECT * FROM orders WHERE id = 1", Source: "inline"},
	})
	iSQL := strings.Index(out, "## 1. 输入 SQL")
	iEvi := strings.Index(out, "## 3. 关键证据")
	if iSQL < 0 || iEvi < 0 || iSQL > iEvi {
		t.Errorf("expected 输入SQL before 关键证据\n%s", out)
	}
}
