package tools

import (
	"strings"
	"testing"
)

func TestRenderParams(t *testing.T) {
	d := &paramsData{Target: "x", Pattern: "work_mem", Rows: []paramRow{
		{Name: "work_mem", Value: "16384 kB", Category: "mem", Mutability: "动态", Desc: "sort memory"},
		{Name: "shared_buffers", Value: "4GB", Category: "mem", Mutability: "需重启", Desc: "shared buffers"},
	}}
	out := renderParams(d)
	for _, want := range []string{"参数", "work_mem", "动态", "需重启", "生效"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderParams missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
	if mutability("postmaster") != "需重启" || mutability("sighup") != "reload" || mutability("user") != "动态" {
		t.Errorf("mutability mapping wrong")
	}
}

func TestRenderSQLCount(t *testing.T) {
	d := &sqlCountData{Target: "x", Rows: []sqlCountRow{
		{User: "omm", Sel: "6790", Ins: "1", Upd: "0", Del: "0", DDL: "1", DCL: "7", AvgSelMS: "502.59", MaxSelMS: "1140545.52", Total: 6791},
	}}
	out := renderSQLCount(d)
	for _, want := range []string{"SQL 类型计数", "omm", "6790", "平均SEL"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderSQLCount missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderTableInfo(t *testing.T) {
	d := &tableInfoData{
		Schema: "sqltune_demo", Table: "orders",
		Stats:   []kv{{"总大小", "40 MB"}, {"存活行", "500000"}},
		Columns: [][]string{{"order_id", "integer", "NOT NULL", ""}, {"status", "character varying(20)", "", "'new'"}},
		Indexes: [][]string{{"orders_pkey", "PK", "0.0 MB", "CREATE UNIQUE INDEX ..."}},
	}
	out := renderTableInfo(d)
	for _, want := range []string{"表结构 · sqltune_demo.orders", "大小 / 统计", "列", "order_id", "索引", "orders_pkey"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderTableInfo missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestParseQualified(t *testing.T) {
	cases := []struct {
		in, sch, tbl string
		ok           bool
	}{
		{"sqltune_demo.orders", "sqltune_demo", "orders", true},
		{"orders", "public", "orders", true},
		{"bad name", "", "", false},
		{"a.b.c", "", "", false}, // second dot makes table "b.c" → invalid ident
		{"orders; DROP", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		sch, tbl, err := parseQualified(c.in)
		if c.ok && (err != nil || sch != c.sch || tbl != c.tbl) {
			t.Errorf("parseQualified(%q) = %q,%q,%v; want %q,%q,ok", c.in, sch, tbl, err, c.sch, c.tbl)
		}
		if !c.ok && err == nil {
			t.Errorf("parseQualified(%q) should have errored", c.in)
		}
	}
}

func TestRenderIndexAdvise(t *testing.T) {
	d := &indexAdviseData{Target: "x", SQL: "SELECT * FROM orders WHERE customer_id=5", Rows: [][]string{{"sqltune_demo", "orders", "customer_id"}}}
	out := renderIndexAdvise(d)
	for _, want := range []string{"索引建议", "引擎推荐", "customer_id"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderIndexAdvise missing %q\n%s", want, out)
		}
	}
}

func TestPerfSnapCompare(t *testing.T) {
	store := []perfSnapshot{
		{ID: 1, Unix: 1000, Metrics: map[string]float64{"xact_commit": 100, "blks_hit": 900, "blks_read": 100}},
		{ID: 2, Unix: 1010, Metrics: map[string]float64{"xact_commit": 200, "blks_hit": 1900, "blks_read": 100}},
	}
	a, c, ok := pickCompare(store, "")
	if !ok || a.ID != 1 || c.ID != 2 {
		t.Fatalf("pickCompare default = %d,%d,%v; want 1,2,true", a.ID, c.ID, ok)
	}
	out := renderPerfCompare(store, "")
	for _, want := range []string{"性能对比 · #1 → #2", "事务提交", "区间命中率"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderPerfCompare missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
	// xact_commit delta = 100 over 10s → 10/s
	if !strings.Contains(out, "10.00") {
		t.Errorf("expected 10.00/s rate:\n%s", out)
	}
}

func TestPerfSnapCompareInsufficient(t *testing.T) {
	out := renderPerfCompare([]perfSnapshot{{ID: 1, Unix: 1}}, "")
	if !strings.Contains(out, "至少两个快照") {
		t.Errorf("single snapshot compare should ask for more:\n%s", out)
	}
}
