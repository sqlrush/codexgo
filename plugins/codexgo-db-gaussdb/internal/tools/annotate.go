package tools

import (
	"regexp"
	"strings"
)

// SQL-text anti-pattern detection (sqltune parity #6, SQL side). These are
// deterministic, parse-light heuristics over the SQL text — complementing the
// plan-level detectPlanIssues (explain.go). Each finding is a PlanIssue with a
// concrete suggestion so the model reasons over precise, pre-found problems.

var (
	// func wrapping an indexed column in a predicate: UPPER(col) / LOWER(col) /
	// TO_CHAR(col, ...) / CAST(col ...) on the left of a comparison/LIKE.
	reFuncOnColumn = regexp.MustCompile(`(?i)\b(upper|lower|to_char|to_date|cast|coalesce|substr|trim)\s*\(\s*[a-z_][a-z0-9_.]*`)
	reLeadingLike  = regexp.MustCompile(`(?i)like\s+'%`)
	reNotIn        = regexp.MustCompile(`(?i)\bnot\s+in\s*\(\s*select`)
	reInSelect     = regexp.MustCompile(`(?i)[^t]\bin\s*\(\s*select`)
	reDistinct     = regexp.MustCompile(`(?i)select\s+distinct`)
	reGroupBy      = regexp.MustCompile(`(?i)\bgroup\s+by\b`)
	reCommaJoin    = regexp.MustCompile(`(?i)\bfrom\b[^;]*?,[^;]*?\bwhere\b`)
	reOrLike       = regexp.MustCompile(`(?i)\bor\b`)
)

// detectSQLIssues scans the SQL text for common anti-patterns.
func detectSQLIssues(sql string) []PlanIssue {
	var out []PlanIssue
	low := strings.ToLower(sql)

	if reFuncOnColumn.MatchString(sql) {
		out = append(out, PlanIssue{
			Kind:       "function_on_column",
			Detail:     "谓词对列套了函数(UPPER/TO_CHAR/CAST 等)",
			Suggestion: "列上套函数会使普通索引失效;改为对常量侧变形(如日期用范围 col>=x AND col<y),或建表达式索引",
		})
	}
	if reLeadingLike.MatchString(sql) {
		out = append(out, PlanIssue{
			Kind:       "leading_wildcard_like",
			Detail:     "LIKE '%...' 前导通配符",
			Suggestion: "前导 % 无法走 B-tree 索引;改前缀匹配 'x%',或用 pg_trgm GIN 索引(USING gin(col gin_trgm_ops))",
		})
	}
	if reNotIn.MatchString(sql) {
		out = append(out, PlanIssue{
			Kind:       "not_in_subquery",
			Detail:     "NOT IN (子查询)",
			Suggestion: "NOT IN 遇子查询含 NULL 会整体返回空且难优化;改写为 NOT EXISTS",
		})
	}
	if reInSelect.MatchString(sql) && !reNotIn.MatchString(sql) {
		out = append(out, PlanIssue{
			Kind:       "in_subquery",
			Detail:     "IN (子查询)",
			Suggestion: "可改写为 EXISTS 或半连接 JOIN,通常计划更稳定",
		})
	}
	if reDistinct.MatchString(low) && reGroupBy.MatchString(low) {
		out = append(out, PlanIssue{
			Kind:       "distinct_with_groupby",
			Detail:     "SELECT DISTINCT 同时带 GROUP BY",
			Suggestion: "GROUP BY 已保证分组唯一,DISTINCT 多半冗余,徒增一次排序去重,可去掉",
		})
	}
	if reCommaJoin.MatchString(low) {
		out = append(out, PlanIssue{
			Kind:       "implicit_join",
			Detail:     "逗号隐式连接(FROM a, b WHERE ...)",
			Suggestion: "改显式 JOIN ... ON 提升可读性(对内连接而言性能与隐式等价,优化器一视同仁,非性能问题)",
		})
	}
	return dedupPlanIssues(out)
}
