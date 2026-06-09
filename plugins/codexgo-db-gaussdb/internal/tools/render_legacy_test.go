package tools

import (
	"strings"
	"testing"
)

// TestRenderTableReportMultilineCell is the regression guard for the /slowsql
// JSON bug: a query cell containing newlines must be collapsed so the table
// stays aligned (and is never emitted as raw JSON).
func TestRenderTableReportMultilineCell(t *testing.T) {
	r := TableReport{
		Title: "慢 SQL", Target: "x",
		Note:    "test",
		Params:  map[string]string{"limit": "20", "threshold_ms": "1000"},
		Columns: []string{"unique_sql_id", "query", "calls", "avg_ms"},
		Rows: [][]string{
			{"1389787684", "SELECT DISTINCT c.id\n  FROM customers c,\n  orders o\nWHERE c.id = o.cid", "1", "1140545.52"},
			{"697864226", "SELECT 1", "3", "2270.91"},
		},
		RowCount: 2,
	}
	out := renderTableReport(r)
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatal("renderTableReport emitted JSON")
	}
	for _, want := range []string{"# 慢 SQL · x", "unique_sql_id", "1389787684", "threshold_ms=1000"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	// the multi-line query must be on ONE table line (no stray newline inside table)
	if strings.Contains(out, "FROM customers c,\n") {
		t.Errorf("multiline cell not collapsed:\n%s", out)
	}
	assertTablesAligned(t, out)
}

func TestCleanCellAndNumeric(t *testing.T) {
	if got := cleanCell("a\n  b\t c\r\nd"); got != "a b c d" {
		t.Errorf("cleanCell = %q", got)
	}
	for _, s := range []string{"123", "1140545.52", "50.0%", "-12", "1,000"} {
		if !looksNumeric(s) {
			t.Errorf("looksNumeric(%q) should be true", s)
		}
	}
	for _, s := range []string{"abc", "1389787684x", ""} {
		if looksNumeric(s) {
			t.Errorf("looksNumeric(%q) should be false", s)
		}
	}
}

func TestRenderAsh(t *testing.T) {
	r := AshReport{
		Target: "x",
		Distribution: TableReport{
			Columns: []string{"wait_type", "wait_event", "sessions", "pct"},
			Rows:    [][]string{{"CPU", "On CPU", "4", "100.0"}},
		},
		ActiveSessions: TableReport{
			Columns: []string{"pid", "db_user", "run_sec", "query"},
			Rows:    [][]string{{"123", "app", "5.0", "SELECT 1"}},
		},
	}
	out := renderAsh(r)
	for _, want := range []string{"活跃会话采样(ASH)", "等待分布", "CPU·On CPU", "活动会话明细"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderAsh missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderExplain(t *testing.T) {
	r := PlanReport{
		Target: "x", SQL: "SELECT * FROM orders",
		Plan:   []string{"Seq Scan on orders  (cost=0.00..100.00 rows=5000)"},
		Issues: []PlanIssue{{Kind: "seq_scan", Detail: "全表扫描: orders", Suggestion: "建索引"}},
	}
	out := renderExplain(r)
	for _, want := range []string{"执行计划", "估算计划", "```sql", "Seq Scan on orders", "⚠️ 风险点", "全表扫描: orders"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderExplain missing %q\n%s", want, out)
		}
	}
}

func TestRenderIndexHealth(t *testing.T) {
	r := IndexHealthReport{
		Target:  "x",
		Summary: map[string]int{"unused": 38, "invalid": 0, "duplicate": 1, "bloat": 13},
		Unused:  TableReport{Title: "未使用索引", Columns: []string{"table_name", "index_name", "idx_scan", "size"}, Rows: [][]string{{"public.t", "t_idx", "0", "12 MB"}}},
		Invalid: TableReport{Title: "失效/未就绪索引"},
	}
	out := renderIndexHealth(r)
	for _, want := range []string{"索引健康", "总览", "未使用索引", "t_idx", "✅ 无"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderIndexHealth missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderSQLFetch(t *testing.T) {
	r := SQLFetchResult{SQLID: "123", Query: "SELECT 1", Source: "statement_history", Schema: "public", HasLiterals: true}
	out := renderSQLFetch(r)
	for _, want := range []string{"SQL 解析 · sql_id 123", "含字面量", "statement_history", "```sql"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderSQLFetch missing %q\n%s", want, out)
		}
	}
	r2 := SQLFetchResult{SQLID: "9", Query: "SELECT ?", Source: "statement", Placeholders: 1}
	if !strings.Contains(renderSQLFetch(r2), "占位符") {
		t.Errorf("placeholder status missing")
	}
}
