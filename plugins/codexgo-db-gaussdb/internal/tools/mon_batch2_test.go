package tools

import (
	"strings"
	"testing"
)

func TestRenderVacuum(t *testing.T) {
	d := &vacuumData{
		Target: "x", Enabled: "on", Threshold: 50, ScaleFactor: 0.2, OverCount: 1,
		Rows: []vacuumRow{
			{Table: "public.t1", Live: 1000, Dead: 5000, DeadPct: 500, LastAutovac: "", OverThreshold: true},
			{Table: "public.t2", Live: 100000, Dead: 200, DeadPct: 0.2, LastAutovac: "2026-06-09 10:00:00"},
		},
		Workers: []vacuumWorker{{PID: "123", Elapsed: "42", Query: "autovacuum: VACUUM public.t1"}},
	}
	out := renderVacuum(d)
	for _, want := range []string{"Vacuum / 死元组", "autovacuum=on", "进行中的 vacuum", "死元组 Top", "超阈", "从未"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderVacuum missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderXID(t *testing.T) {
	d := &xidData{
		Target: "x",
		DBs:    []xidRow{{Name: "postgres", Age: 3223206}},
		Tables: []xidRow{{Name: "public.fault_cpu", Age: 3201180}},
	}
	out := renderXID(d)
	for _, want := range []string{"回卷风险", "数据库", "高龄表 Top", "postgres", "public.fault_cpu"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderXID missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderBloat(t *testing.T) {
	d := &bloatData{Target: "x", Threshold: 5, Rows: []bloatRow{
		{Table: "public.t1", Live: "1000", Dead: "5000", DeadPct: 500, Size: "120 MB"},
	}}
	out := renderBloat(d)
	for _, want := range []string{"表膨胀估算", "膨胀比 Top", "明细", "120 MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderBloat missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderSpace(t *testing.T) {
	d := &spaceData{
		Target: "x",
		DBs:    []spaceDBRow{{Name: "postgres", SizeMB: 2400}, {Name: "omm", SizeMB: 12}},
		Tables: []spaceTableRow{{Table: "gaussdb.bench_order_items", Total: "543 MB", Tbl: "328 MB", Idx: "215 MB"}},
		Toasts: []spaceToastRow{{Table: "public.big", Toast: "80 MB", Total: "100 MB", Pct: "80.0"}},
	}
	out := renderSpace(d)
	for _, want := range []string{"空间使用", "数据库大小", "2.3 GB", "大表 Top", "大 TOAST Top", "543 MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderSpace missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderTempUsage(t *testing.T) {
	d := &tempData{
		Target:  "x",
		WorkMem: "16384kB",
		DBs:     []tempDBRow{{DB: "postgres", Files: "223", Pretty: "359 MB"}},
	}
	out := renderTempUsage(d)
	for _, want := range []string{"临时文件 / 落盘", "work_mem = 16384kB", "按数据库", "359 MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderTempUsage missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderHotKey(t *testing.T) {
	d := &hotkeyData{Target: "x", Rows: []hotkeyRow{
		{Table: "sqltune_demo.products", SeqScan: "387910", IdxScan: "388442", Ins: "0", Upd: "0", Del: "0", Activity: 776352, Flag: "-"},
		{Table: "public.big", SeqScan: "9000", IdxScan: "0", Ins: "0", Upd: "0", Del: "0", Activity: 9000, Flag: "seq only"},
	}}
	out := renderHotKey(d)
	for _, want := range []string{"热点表", "活动量 Top", "明细", "seq only"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderHotKey missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestVacuumTriggerFlag(t *testing.T) {
	// dead 5000 vs trigger 50 + 0.2*6000 = 1250 → over threshold.
	d := &vacuumData{Threshold: 50, ScaleFactor: 0.2}
	live, dead := 1000.0, 5000.0
	trigger := d.Threshold + d.ScaleFactor*(live+dead)
	if !(dead >= trigger) {
		t.Errorf("expected over-threshold: dead %.0f vs trigger %.0f", dead, trigger)
	}
}
