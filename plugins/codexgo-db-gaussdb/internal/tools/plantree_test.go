package tools

import (
	"strings"
	"testing"
	"time"
)

func TestDecideAnalyzeTiers(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		wantAna bool
		wantTO  time.Duration
	}{
		{"short", "SELECT 1", true, 30 * time.Second},
		{"99-lines", strings.Repeat("x\n", 98) + "x", true, 30 * time.Second},    // 99 lines: last 30s tier
		{"100-lines", strings.Repeat("x\n", 99) + "x", true, 60 * time.Second},   // 100 lines: first 60s tier
		{"medium", strings.Repeat("x\n", 149) + "x", true, 60 * time.Second},     // 150 lines
		{"499-lines", strings.Repeat("x\n", 498) + "x", true, 60 * time.Second},  // 499 lines: last ANALYZE tier
		{"500-lines", strings.Repeat("x\n", 499) + "x", false, 30 * time.Second}, // 500 lines: first plain tier
		{"large", strings.Repeat("x\n", 600) + "x", false, 30 * time.Second},     // 601 lines
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ana, to := decideAnalyze(c.sql)
			if ana != c.wantAna || to != c.wantTO {
				t.Errorf("decideAnalyze(%s)=%v,%v want %v,%v", c.name, ana, to, c.wantAna, c.wantTO)
			}
		})
	}
}

func TestParsePlanTree(t *testing.T) {
	js := `[{"Plan":{"Node Type":"Aggregate","Total Cost":29165.79,"Startup Cost":100.0,"Plan Rows":1,
	  "Plans":[{"Node Type":"Sort","Total Cost":29000.0,"Plan Rows":500,
	    "Sort Method":"external merge","Sort Space Type":"Disk","Sort Space Used":20480,
	    "Actual Rows":500,"Actual Loops":1,"Sort Key":["o.id"],
	    "Plans":[{"Node Type":"Seq Scan","Relation Name":"orders","Alias":"o","Total Cost":12000.0,"Plan Rows":500}]}]},
	  "Planning Time":0.5,"Execution Time":1234.5}]`
	info, err := parsePlanTree(js, true)
	if err != nil {
		t.Fatalf("parsePlanTree error: %v", err)
	}
	if info.Root == nil || info.Root.Operator != "Aggregate" {
		t.Fatalf("root operator = %+v", info.Root)
	}
	if info.TotalCost != 29165.79 {
		t.Errorf("total cost = %v want 29165.79", info.TotalCost)
	}
	if info.PlanningTime != 0.5 || info.ExecutionTime != 1234.5 {
		t.Errorf("timings = %v/%v want 0.5/1234.5", info.PlanningTime, info.ExecutionTime)
	}
	if !info.HasAnalyze {
		t.Error("HasAnalyze should be true")
	}
	sort := info.Root.Children
	if len(sort) != 1 || sort[0].Operator != "Sort" {
		t.Fatalf("child[0] = %+v", sort)
	}
	if sort[0].SortSpaceType != "Disk" || sort[0].SortSpaceUsed != 20480 {
		t.Errorf("sort space = %s/%d", sort[0].SortSpaceType, sort[0].SortSpaceUsed)
	}
	if len(sort[0].SortKey) != 1 || sort[0].SortKey[0] != "o.id" {
		t.Errorf("sort key = %v", sort[0].SortKey)
	}
	leaf := sort[0].Children
	if len(leaf) != 1 || leaf[0].Operator != "Seq Scan" || leaf[0].Relation != "orders" {
		t.Fatalf("leaf = %+v", leaf)
	}
}

func TestParsePlanTreeBadInput(t *testing.T) {
	if _, err := parsePlanTree("not json", false); err == nil {
		t.Error("expected error on non-json")
	}
	if _, err := parsePlanTree("[]", false); err == nil {
		t.Error("expected error on empty array")
	}
}

func TestDetectPlanTreeIssues(t *testing.T) {
	root := &PlanNode{
		Operator: "Hash Join", TotalCost: 50000, HashCond: "a.id = b.id",
		Children: []*PlanNode{
			{Operator: "Sort", SortMethod: "external merge", SortSpaceType: "Disk", SortSpaceUsed: 8192},
			{Operator: "Seq Scan", Relation: "big", PlanRows: 1, ActualRows: 50000, ActualLoops: 1},
		},
	}
	kinds := map[string]bool{}
	for _, i := range detectPlanTreeIssues(root, true) {
		kinds[i.Kind] = true
	}
	for _, want := range []string{"sort_spill", "expensive_hash", "row_estimate_skew"} {
		if !kinds[want] {
			t.Errorf("missing tree issue %q (got %v)", want, kinds)
		}
	}
	// Without ANALYZE, row skew must NOT fire even if actual fields are present.
	noAna := map[string]bool{}
	for _, i := range detectPlanTreeIssues(root, false) {
		noAna[i.Kind] = true
	}
	if noAna["row_estimate_skew"] {
		t.Error("row_estimate_skew fired without ANALYZE")
	}
	// In-memory sort must NOT be flagged as a spill.
	clean := &PlanNode{Operator: "Sort", SortSpaceType: "Memory"}
	if iss := detectPlanTreeIssues(clean, false); len(iss) != 0 {
		t.Errorf("in-memory sort wrongly flagged: %+v", iss)
	}
}
