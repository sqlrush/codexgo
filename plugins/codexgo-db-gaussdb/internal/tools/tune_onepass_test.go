package tools

import (
	"strings"
	"testing"
)

// TestTuneEvidenceWithTemplate verifies the single-pass sqltune payload embeds
// the deterministic evidence AND instructs the model to tie each fix to a [Pn]
// hotspot (the fix for pass-2 analysis being disconnected from pass-1 hotspots).
func TestTuneEvidenceWithTemplate(t *testing.T) {
	r := &TuneReport{
		Target:       "dbaa:gauss_local",
		EffectiveSQL: "SELECT * FROM orders WHERE customer_id = 5",
		Resolved:     SQLFetchResult{Query: "SELECT * FROM orders WHERE customer_id = 5", Source: "inline"},
	}
	out := tuneEvidenceWithTemplate(r)
	for _, want := range []string{
		"实测证据",         // embeds renderTuneReport
		"SQL 调优报告",     // renderTuneReport title
		"针对哪个 [Pn] 热点", // ties fixes to hotspots (the disconnect fix)
		"根因分析",
		"【实测】", "【AI推断】", // verified vs inferred labelling
		"不要调用其它工具", // single pass
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tuneEvidenceWithTemplate missing %q", want)
		}
	}
}
