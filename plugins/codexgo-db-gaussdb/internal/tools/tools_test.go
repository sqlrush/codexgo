package tools

import "testing"

func TestScoreAndGrade(t *testing.T) {
	tests := []struct {
		name      string
		c         healthCounts
		wantScore int
		wantGrade string
	}{
		{"all ok", healthCounts{OK: 8}, 100, "优"},
		{"one warn", healthCounts{OK: 7, Warn: 1}, 94, "良"},
		{"two warn still 良", healthCounts{OK: 6, Warn: 2}, 88, "良"},
		{"four warn drops to 警告", healthCounts{OK: 4, Warn: 4}, 76, "警告"},
		{"one fail forces danger", healthCounts{OK: 7, Fail: 1}, 82, "危险"},
		{"floor at zero", healthCounts{Fail: 10}, 0, "危险"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, grade := scoreAndGrade(tt.c)
			if score != tt.wantScore || grade != tt.wantGrade {
				t.Fatalf("scoreAndGrade(%+v) = (%d,%q), want (%d,%q)",
					tt.c, score, grade, tt.wantScore, tt.wantGrade)
			}
		})
	}
}

func TestLevel(t *testing.T) {
	tests := []struct {
		v, warn, fail float64
		want          string
	}{
		{10, 80, 95, statusOK},
		{85, 80, 95, statusWarn},
		{96, 80, 95, statusFail},
		{80, 80, 95, statusWarn}, // boundary is inclusive
		{95, 80, 95, statusFail},
	}
	for _, tt := range tests {
		if got := level(tt.v, tt.warn, tt.fail); got != tt.want {
			t.Errorf("level(%v,%v,%v)=%q want %q", tt.v, tt.warn, tt.fail, got, tt.want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		sec  int64
		want string
	}{
		{30, "0m"},
		{90, "1m"},
		{3661, "1h 1m"},
		{90061, "1d 1h"},
	}
	for _, tt := range tests {
		if got := humanDuration(tt.sec); got != tt.want {
			t.Errorf("humanDuration(%d)=%q want %q", tt.sec, got, tt.want)
		}
	}
}

func TestCountPlaceholders(t *testing.T) {
	tests := []struct {
		q    string
		want int
	}{
		{"SELECT 1", 0},
		{"SELECT * FROM t WHERE id = ?", 1},
		{"SELECT * FROM t WHERE a=$1 AND b=$2", 2},
		{"SELECT * FROM t WHERE a=:1", 1},
		{"SELECT '?' AS lit", 0}, // ? inside string literal ignored
		{"SELECT * FROM t WHERE x='a$1b' AND y=?", 1},
	}
	for _, tt := range tests {
		if got := countPlaceholders(tt.q); got != tt.want {
			t.Errorf("countPlaceholders(%q)=%d want %d", tt.q, got, tt.want)
		}
	}
}

func TestIsLikelySQLID(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"123456789", true},
		{"-123", true},
		{"SELECT 1", false},
		{"", false},
		{"abc", false},
		{"12 34", false},
	}
	for _, tt := range tests {
		if got := isLikelySQLID(tt.s); got != tt.want {
			t.Errorf("isLikelySQLID(%q)=%v want %v", tt.s, got, tt.want)
		}
	}
}

func TestIsReadOnlySQL(t *testing.T) {
	readOnly := []string{
		"SELECT * FROM t",
		"  select 1",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"TABLE t",
		"VALUES (1)",
		"SHOW max_connections",
	}
	for _, s := range readOnly {
		if !isReadOnlySQL(s) {
			t.Errorf("isReadOnlySQL(%q)=false, want true", s)
		}
	}
	writes := []string{
		"INSERT INTO t VALUES (1)",
		"update t set a=1",
		"DELETE FROM t",
		"DROP TABLE t",
		"TRUNCATE t",
		"CALL proc()",
		"VACUUM t",
	}
	for _, s := range writes {
		if isReadOnlySQL(s) {
			t.Errorf("isReadOnlySQL(%q)=true, want false", s)
		}
	}
}

func TestStripLeadingExplain(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"SELECT 1", "SELECT 1"},
		{"EXPLAIN SELECT 1", "SELECT 1"},
		{"explain (analyze true) SELECT 1", "SELECT 1"},
		{"EXPLAIN  (FORMAT JSON)  SELECT * FROM t", "SELECT * FROM t"},
	}
	for _, tt := range tests {
		if got := stripLeadingExplain(tt.in); got != tt.want {
			t.Errorf("stripLeadingExplain(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestDetectPlanIssues(t *testing.T) {
	plan := []string{
		"Nested Loop  (cost=0.00..100.00 rows=10 width=8)",
		"  ->  Seq Scan on orders  (cost=0.00..50.00 rows=1000 width=8)",
		"  ->  Seq Scan on orders  (cost=0.00..50.00 rows=1000 width=8)", // dup table
		"  ->  Sort  (cost=10.00..10.50 rows=200 width=8)",
		"        Sort Key: orders.id", // detail line — must NOT count as a sort issue
	}
	issues := detectPlanIssues(plan)
	kinds := map[string]int{}
	for _, it := range issues {
		kinds[it.Kind]++
	}
	if kinds["seq_scan"] != 1 {
		t.Errorf("expected 1 deduped seq_scan issue, got %d", kinds["seq_scan"])
	}
	if kinds["nested_loop_seq_scan"] != 1 {
		t.Errorf("expected nested_loop_seq_scan issue, got %d", kinds["nested_loop_seq_scan"])
	}
	if kinds["sort"] != 1 {
		t.Errorf("expected sort issue, got %d", kinds["sort"])
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		v, def, max, want int
	}{
		{0, 20, 100, 20},
		{-5, 20, 100, 20},
		{50, 20, 100, 50},
		{500, 20, 100, 100},
	}
	for _, tt := range tests {
		if got := clampLimit(tt.v, tt.def, tt.max); got != tt.want {
			t.Errorf("clampLimit(%d,%d,%d)=%d want %d", tt.v, tt.def, tt.max, got, tt.want)
		}
	}
}

func TestTopSQLSortsWhitelist(t *testing.T) {
	for _, k := range []string{"el", "ae", "ex", "lr", "rw"} {
		if _, ok := topSQLSorts[k]; !ok {
			t.Errorf("sort key %q missing from whitelist", k)
		}
	}
	if _, ok := topSQLSorts["; DROP TABLE t"]; ok {
		t.Error("injection key unexpectedly present in whitelist")
	}
}
