package tools

import (
	"strings"
	"testing"
)

// assertTablesAligned scans rendered output for contiguous runs of asciiTable
// lines (start with '+' or '|') and asserts every line in a run has identical
// display width — the load-bearing guard for CJK alignment in codexgo's TUI.
func assertTablesAligned(t *testing.T, out string) {
	t.Helper()
	var run []string
	flush := func() {
		if len(run) < 2 {
			run = nil
			return
		}
		want := dispWidth(run[0])
		for i, ln := range run {
			if got := dispWidth(ln); got != want {
				t.Errorf("table misaligned: line %d width %d != %d\n%q\n%q", i, got, want, run[0], ln)
			}
		}
		run = nil
	}
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "+") || strings.HasPrefix(ln, "|") {
			run = append(run, ln)
		} else {
			flush()
		}
	}
	flush()
}

func TestRenderSessions(t *testing.T) {
	d := &sessionsData{
		Target: "dbaa:gauss_local",
		Counts: map[string]int{"active": 2, "idle": 3, "idle in transaction": 1},
		Total:  6,
		Rows: []sessionRow{
			{PID: "101", User: "app", DB: "postgres", State: "active", Wait: "-", Query: "SELECT 1", QuerySec: 3},
			{PID: "102", User: "app", DB: "postgres", State: "idle in transaction", Wait: "-", Query: "UPDATE t SET x=1", QuerySec: 5, XactSec: 900},
		},
	}
	out := renderSessions(d, false)
	for _, want := range []string{"会话概览", "状态分布", "会话明细", "active", "idle in transaction (!)", "事务中空闲 >10min"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderSessions missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)

	dig := sessionsDigest(d, false)
	if !strings.Contains(dig, "长事务中空闲") {
		t.Errorf("digest should flag long idle-in-tx: %s", dig)
	}
}

func TestBuildBlockTree(t *testing.T) {
	// A(1) blocks B(2); B(2) blocks C(3). Root = 1, victims = {2,3}.
	rows := [][]string{
		{"2", "u", "q2", "1", "u", "q1", "Lock", "lock_wait"},
		{"3", "u", "q3", "2", "u", "q2", "Lock", "lock_wait"},
	}
	roots, victims := buildBlockTree(rows)
	if len(roots) != 1 || roots[0].PID != "1" {
		t.Fatalf("want single root pid 1, got %+v", roots)
	}
	if victims != 2 {
		t.Errorf("want 2 victims, got %d", victims)
	}
	tree := renderBlockTree(roots)
	for _, want := range []string{"pid 1", "+- pid 2", "+- pid 3"} {
		if !strings.Contains(tree, want) {
			t.Errorf("tree missing %q\n%s", want, tree)
		}
	}
}

func TestBuildBlockTreeCycleSafe(t *testing.T) {
	// Cycle: 1 blocks 2, 2 blocks 1. renderBlockTree must terminate.
	rows := [][]string{
		{"2", "u", "q2", "1", "u", "q1", "Lock", "lock_wait"},
		{"1", "u", "q1", "2", "u", "q2", "Lock", "lock_wait"},
	}
	roots, _ := buildBlockTree(rows)
	out := renderBlockTree(roots) // must not infinite-loop
	if !strings.Contains(out, "循环引用") {
		t.Errorf("cycle should be marked: %s", out)
	}
}

func TestRenderLocksEmpty(t *testing.T) {
	d := &locksData{Target: "x"}
	out := renderLocks(d)
	if !strings.Contains(out, "无锁等待") {
		t.Errorf("empty locks should say no waits: %s", out)
	}
}

func TestRenderLocks(t *testing.T) {
	d := &locksData{Target: "x", Rows: [][]string{
		{"2", "u", "UPDATE t", "1", "u", "BEGIN", "Lock", "lock_wait"},
	}}
	d.Roots, d.Blocked = buildBlockTree(d.Rows)
	out := renderLocks(d)
	for _, want := range []string{"阻塞链", "等待明细", "根阻塞源 pid:1", "+- pid 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderLocks missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderLWLocks(t *testing.T) {
	d := &lwlocksData{Target: "x", Total: 7, Rows: []lwlockRow{
		{Thread: "WALWriter", Status: "acquire lwlock", Event: "WALWriteLock", Waiters: "5", SampleTID: "111", WaiterN: 5},
		{Thread: "worker", Status: "wait io", Event: "DataFileRead", Waiters: "2", SampleTID: "222", WaiterN: 2},
	}}
	out := renderLWLocks(d)
	for _, want := range []string{"轻量锁", "争用分布", "WALWriteLock", "明细"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderLWLocks missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderLongTx(t *testing.T) {
	d := &longtxData{Target: "x", Rows: []longtxRow{
		{PID: "1", User: "app", State: "active", XactSec: 2000, Query: "SELECT pg_sleep(9999)", Sev: longTxSeverity(2000)},
		{PID: "2", User: "app", State: "idle in transaction", XactSec: 400, Query: "UPDATE t", Sev: longTxSeverity(400)},
		{PID: "3", User: "app", State: "active", XactSec: 30, Query: "SELECT 1", Sev: longTxSeverity(30)},
	}}
	out := renderLongTx(d)
	for _, want := range []string{"长事务", "严重", "警告", "事务时长"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderLongTx missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
	if longTxSeverity(2000) != statusFail || longTxSeverity(400) != statusWarn || longTxSeverity(30) != statusOK {
		t.Errorf("severity grading wrong")
	}
}
