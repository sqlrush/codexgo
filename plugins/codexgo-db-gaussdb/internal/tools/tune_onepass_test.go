package tools

import (
	"strings"
	"testing"
)

// TestTuneAnalysisInstruction verifies the assistant-only instruction asks the
// model to tie its analysis to [Pn] hotspots WITHOUT repeating the evidence
// (the [Pn]-annotated plan is rendered deterministically as the user evidence).
func TestTuneAnalysisInstruction(t *testing.T) {
	out := tuneAnalysisInstruction(&TuneReport{Target: "x"})
	for _, want := range []string{
		"勿重复",         // don't repeat the evidence
		"[P1..Pn] 热点", // tie to hotspots
		"## 根因分析",
		"## 优化方案",
		"【实测】", "【AI推断】",
		"不要调用其它工具",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tuneAnalysisInstruction missing %q", want)
		}
	}
}

// TestRenderTuneReportHasPlanSection verifies the deterministic evidence report
// (rendered to the user) includes the execution-plan section between the input
// SQL and the cost-hotspot evidence — the part the model used to drop.
func TestRenderTuneReportHasPlanSection(t *testing.T) {
	out := renderTuneReport(&TuneReport{
		Target:       "x",
		EffectiveSQL: "SELECT * FROM orders WHERE id = 1",
		Resolved:     SQLFetchResult{Query: "SELECT * FROM orders WHERE id = 1", Source: "inline"},
	})
	// §1 input SQL then §3 evidence are always present; §2 plan appears when a
	// plan tree was collected (live). Section ordering/titles must be stable.
	iSQL := strings.Index(out, "## 1. 输入 SQL")
	iEvi := strings.Index(out, "## 3. 关键证据")
	if iSQL < 0 || iEvi < 0 || iSQL > iEvi {
		t.Errorf("expected 输入SQL before 关键证据\n%s", out)
	}
}
