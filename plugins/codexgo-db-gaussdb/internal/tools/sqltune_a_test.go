package tools

import (
	"strings"
	"testing"
)

func TestSubstituteBindsRules(t *testing.T) {
	sql := "SELECT * FROM orders o WHERE TO_CHAR(o.order_date,?)=? AND p.price>? AND c.email LIKE ? AND c.id=? LIMIT ?"
	out, fills := substituteBinds(sql)
	if len(fills) != 6 {
		t.Fatalf("expected 6 fills, got %d", len(fills))
	}
	if strings.Contains(out, "?") {
		t.Errorf("placeholders remain: %s", out)
	}
	// spot-check key rules
	if !strings.Contains(out, "LIKE '%test%'") {
		t.Errorf("LIKE not filled with sample string: %s", out)
	}
	if !strings.Contains(out, "LIMIT 100") {
		t.Errorf("LIMIT not filled with 100: %s", out)
	}
	if !strings.Contains(out, "price>50") {
		t.Errorf("range op not filled with 50: %s", out)
	}
	if !strings.Contains(out, "c.id=1") {
		t.Errorf("int-column = not filled with 1: %s", out)
	}
}

func TestSubstituteBindsNoPlaceholders(t *testing.T) {
	sql := "SELECT 1 WHERE 'a?b' = 'a?b'" // ? inside string literal, not a placeholder
	out, fills := substituteBinds(sql)
	if len(fills) != 0 || out != sql {
		t.Errorf("string-literal ? wrongly treated as placeholder: out=%q fills=%d", out, len(fills))
	}
}

func TestDetectSQLIssues(t *testing.T) {
	sql := `SELECT DISTINCT c.id FROM customers c, orders o
WHERE UPPER(c.email) LIKE '%test%'
  AND c.id NOT IN (SELECT customer_id FROM orders WHERE status='x')
GROUP BY c.id`
	kinds := map[string]bool{}
	for _, i := range detectSQLIssues(sql) {
		kinds[i.Kind] = true
	}
	for _, want := range []string{"function_on_column", "leading_wildcard_like", "not_in_subquery", "distinct_with_groupby", "implicit_join"} {
		if !kinds[want] {
			t.Errorf("missing sql issue %q (got %v)", want, kinds)
		}
	}
}

func TestGenerateCandidatesDistinct(t *testing.T) {
	sql := "SELECT DISTINCT a, b FROM t GROUP BY a, b"
	cands := generateCandidates(sql)
	if len(cands) != 1 || cands[0].Rule != "remove_redundant_distinct" {
		t.Fatalf("expected 1 distinct-removal candidate, got %+v", cands)
	}
	if strings.Contains(strings.ToLower(cands[0].SQL), "distinct") {
		t.Errorf("DISTINCT not removed: %s", cands[0].SQL)
	}
	// no GROUP BY -> no candidate
	if c := generateCandidates("SELECT DISTINCT a FROM t"); len(c) != 0 {
		t.Errorf("DISTINCT without GROUP BY should not be a candidate: %+v", c)
	}
}

func TestRemoveFirstDistinctOnlyOuter(t *testing.T) {
	sql := "SELECT DISTINCT a FROM t WHERE a IN (SELECT DISTINCT x FROM u) GROUP BY a"
	out, ok := removeFirstDistinct(sql)
	if !ok {
		t.Fatal("expected a rewrite")
	}
	// only the first DISTINCT removed; the subquery DISTINCT remains
	if strings.Count(strings.ToLower(out), "distinct") != 1 {
		t.Errorf("should remove only the outer DISTINCT: %s", out)
	}
}

func TestParseTopTotalCost(t *testing.T) {
	js := `[{"Plan":{"Node Type":"Aggregate","Total Cost":29165.79,"Plans":[]}}]`
	c, ok := parseTopTotalCost(js)
	if !ok || c != 29165.79 {
		t.Errorf("parseTopTotalCost=%v,%v want 29165.79,true", c, ok)
	}
	if _, ok := parseTopTotalCost("not json"); ok {
		t.Error("expected false on non-json")
	}
}

func TestExtractTableNames(t *testing.T) {
	sql := "SELECT * FROM sqltune_demo.customers c JOIN orders o ON c.id=o.cid, regions r WHERE 1=1"
	names := extractTableNames(sql)
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, want := range []string{"customers", "orders"} {
		if !got[want] {
			t.Errorf("missing table %q (got %v)", want, names)
		}
	}
}
