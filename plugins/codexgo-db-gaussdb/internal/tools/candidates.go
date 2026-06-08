package tools

import (
	"regexp"
	"strings"
)

// Deterministic rewrite candidates (sqltune parity #10). Conservative,
// mechanical rewrites only — each is then verified (cost + equivalence) so a
// no-op or semantics-changing candidate is caught rather than recommended. The
// model can additionally pass its own rewrite via the `candidate` arg to have it
// verified the same way.

var reSelectDistinct = regexp.MustCompile(`(?i)\bselect\s+distinct\s+`)

// generateCandidates returns mechanical rewrite candidates for the SQL.
func generateCandidates(sql string) []RewriteCandidate {
	var out []RewriteCandidate
	low := strings.ToLower(sql)

	// Redundant DISTINCT: SELECT DISTINCT ... GROUP BY ... — the GROUP BY already
	// yields distinct grouping rows, so the outer DISTINCT is usually redundant.
	// Verification (equivalence) confirms before we recommend.
	if reSelectDistinct.MatchString(low) && reGroupBy.MatchString(low) {
		if rewritten, ok := removeFirstDistinct(sql); ok {
			out = append(out, RewriteCandidate{
				Rule: "remove_redundant_distinct",
				SQL:  rewritten,
				Note: "GROUP BY 已保证分组唯一,移除外层冗余 DISTINCT(省一次排序去重)",
			})
		}
	}
	return out
}

// removeFirstDistinct removes only the first (outermost) "SELECT DISTINCT",
// leaving any DISTINCT inside subqueries untouched.
func removeFirstDistinct(sql string) (string, bool) {
	loc := reSelectDistinct.FindStringIndex(sql)
	if loc == nil {
		return sql, false
	}
	// Replace just this occurrence with "SELECT ".
	return sql[:loc[0]] + "SELECT " + sql[loc[1]:], true
}
