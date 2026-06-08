package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
)

// Structured EXPLAIN plan tree + graded ANALYZE (sqltune parity #4, brought to
// 1:1 with opendb). Two halves:
//
//  1. JSON plan tree — EXPLAIN (FORMAT JSON, COSTS, VERBOSE[, ANALYZE, BUFFERS,
//     TIMING]) parsed recursively into PlanNode, so the model sees the real plan
//     shape (operators, costs, estimated/actual rows, sort/hash details, depth)
//     instead of flat text lines.
//
//  2. Graded ANALYZE — whether to actually execute the query for real timings is
//     decided by SQL size, each tier with its own timeout, and any ANALYZE
//     failure (e.g. timeout) falls back to a plan-only EXPLAIN. Faithful to
//     opendb's decideAnalyze thresholds.
//
// Safety note (differs from opendb on purpose): opendb is an interactive tool and
// ANALYZEs by default. This plugin is driven by an LLM, so ANALYZE (which EXECUTES
// the query) only happens when the caller passes analyze=true. The grading logic
// itself is identical; analyze=true is the gate that turns it on.

// PlanNode is one operator in the execution plan tree. Field names mirror
// PostgreSQL/openGauss EXPLAIN JSON keys (which are stable across the PG family);
// JSON tags are snake_case for the model to read.
type PlanNode struct {
	Operator      string      `json:"operator"`
	Relation      string      `json:"relation,omitempty"`
	Alias         string      `json:"alias,omitempty"`
	StartupCost   float64     `json:"startup_cost"`
	TotalCost     float64     `json:"total_cost"`
	PlanRows      int64       `json:"plan_rows"`
	PlanWidth     int         `json:"plan_width,omitempty"`
	ActualStartup float64     `json:"actual_startup_ms,omitempty"`
	ActualTotal   float64     `json:"actual_total_ms,omitempty"`
	ActualRows    int64       `json:"actual_rows,omitempty"`
	ActualLoops   int64       `json:"actual_loops,omitempty"`
	SharedHit     int64       `json:"shared_hit_blocks,omitempty"`
	SharedRead    int64       `json:"shared_read_blocks,omitempty"`
	Filter        string      `json:"filter,omitempty"`
	JoinFilter    string      `json:"join_filter,omitempty"`
	HashCond      string      `json:"hash_cond,omitempty"`
	IndexCond     string      `json:"index_cond,omitempty"`
	SortKey       []string    `json:"sort_key,omitempty"`
	SortMethod    string      `json:"sort_method,omitempty"`
	SortSpaceType string      `json:"sort_space_type,omitempty"`
	SortSpaceUsed int64       `json:"sort_space_used,omitempty"`
	Children      []*PlanNode `json:"children,omitempty"`
}

// PlanInfo is the parsed plan tree plus top-level timings.
type PlanInfo struct {
	Root          *PlanNode `json:"root"`
	TotalCost     float64   `json:"total_cost"`
	PlanningTime  float64   `json:"planning_time_ms,omitempty"`
	ExecutionTime float64   `json:"execution_time_ms,omitempty"`
	HasAnalyze    bool      `json:"has_analyze"`
}

// decideAnalyze grades whether to run EXPLAIN ANALYZE and with what timeout,
// based on SQL size. Faithful to opendb's thresholds (DML is already refused
// upstream by isReadOnlySQL, so it is not re-checked here):
//
//	< 100 lines  -> ANALYZE, 30s
//	< 500 lines  -> ANALYZE, 60s
//	>= 500 lines -> plain,   30s
func decideAnalyze(sql string) (bool, time.Duration) {
	lines := strings.Count(sql, "\n") + 1
	switch {
	case lines < 100:
		return true, 30 * time.Second
	case lines < 500:
		return true, 60 * time.Second
	default:
		return false, 30 * time.Second
	}
}

// collectPlan gathers the structured plan tree. When allowAnalyze is true it
// applies the graded ANALYZE decision (executing the query, with the tier's
// timeout) and falls back to a plan-only EXPLAIN on any error/timeout. When
// allowAnalyze is false it never executes the query — only estimated costs/rows.
func collectPlan(ctx context.Context, conn *db.Conn, sql string, allowAnalyze bool) (*PlanInfo, error) {
	useAnalyze, timeout := decideAnalyze(sql)
	if allowAnalyze && useAnalyze {
		actx, cancel := context.WithTimeout(ctx, timeout)
		info, err := runExplainJSON(actx, conn, sql, true)
		cancel()
		if err == nil {
			return info, nil
		}
		// ANALYZE timed out or errored — fall through to plan-only.
	}
	// Plan-only fallback reuses the SQL-graded timeout (same tier as ANALYZE).
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return runExplainJSON(pctx, conn, sql, false)
}

// runExplainJSON runs one EXPLAIN (FORMAT JSON ...) and parses the tree. ANALYZE
// adds BUFFERS+TIMING for real per-operator timings/rows/blocks.
func runExplainJSON(ctx context.Context, conn *db.Conn, sql string, analyze bool) (*PlanInfo, error) {
	flags := "FORMAT JSON, COSTS TRUE, VERBOSE TRUE"
	if analyze {
		flags += ", ANALYZE TRUE, BUFFERS TRUE, TIMING TRUE"
	}
	res, err := conn.Query(ctx, fmt.Sprintf("EXPLAIN (%s) %s", flags, stripLeadingExplain(sql)))
	if err != nil {
		return nil, err
	}
	// openGauss may split the JSON document across rows; join them.
	var b strings.Builder
	for _, row := range res.Rows {
		if len(row) > 0 {
			b.WriteString(row[0])
		}
	}
	return parsePlanTree(b.String(), analyze)
}

// parsePlanTree parses an EXPLAIN FORMAT JSON document ([{"Plan":{...},
// "Planning Time":N,"Execution Time":N}]) into a PlanInfo.
func parsePlanTree(s string, analyze bool) (*PlanInfo, error) {
	var arr []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &arr); err != nil {
		return nil, fmt.Errorf("parse explain json: %w", err)
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("empty explain json")
	}
	top := arr[0]
	planMap, ok := top["Plan"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("no Plan node in explain json")
	}
	root := parsePlanNode(planMap)
	info := &PlanInfo{
		Root:          root,
		PlanningTime:  pgFloat(top, "Planning Time"),
		ExecutionTime: pgFloat(top, "Execution Time"),
		HasAnalyze:    analyze,
	}
	if root != nil {
		info.TotalCost = root.TotalCost
	}
	return info, nil
}

// parsePlanNode recursively converts one EXPLAIN JSON "Plan" map into a PlanNode.
func parsePlanNode(m map[string]any) *PlanNode {
	if m == nil {
		return nil
	}
	n := &PlanNode{
		Operator:      pgStr(m, "Node Type"),
		Relation:      pgStr(m, "Relation Name"),
		Alias:         pgStr(m, "Alias"),
		StartupCost:   pgFloat(m, "Startup Cost"),
		TotalCost:     pgFloat(m, "Total Cost"),
		PlanRows:      pgInt(m, "Plan Rows"),
		PlanWidth:     int(pgInt(m, "Plan Width")),
		ActualStartup: pgFloat(m, "Actual Startup Time"),
		ActualTotal:   pgFloat(m, "Actual Total Time"),
		ActualRows:    pgInt(m, "Actual Rows"),
		ActualLoops:   pgInt(m, "Actual Loops"),
		SharedHit:     pgInt(m, "Shared Hit Blocks"),
		SharedRead:    pgInt(m, "Shared Read Blocks"),
		Filter:        pgStr(m, "Filter"),
		JoinFilter:    pgStr(m, "Join Filter"),
		HashCond:      pgStr(m, "Hash Cond"),
		IndexCond:     pgStr(m, "Index Cond"),
		SortMethod:    pgStr(m, "Sort Method"),
		SortSpaceType: pgStr(m, "Sort Space Type"),
		SortSpaceUsed: pgInt(m, "Sort Space Used"),
	}
	if sk, ok := m["Sort Key"].([]any); ok {
		for _, s := range sk {
			if str, ok := s.(string); ok {
				n.SortKey = append(n.SortKey, str)
			}
		}
	}
	if children, ok := m["Plans"].([]any); ok {
		for _, c := range children {
			if cm, ok := c.(map[string]any); ok {
				if child := parsePlanNode(cm); child != nil && child.Operator != "" {
					n.Children = append(n.Children, child)
				}
			}
		}
	}
	return n
}

// detectPlanTreeIssues walks the parsed tree for issues only the structured plan
// reveals: sort spill to disk, expensive hash operators, and (ANALYZE only)
// estimate-vs-actual row skew. Returns structured PlanIssue findings.
func detectPlanTreeIssues(root *PlanNode, hasAnalyze bool) []PlanIssue {
	var issues []PlanIssue
	walkPlan(root, func(n *PlanNode) {
		op := strings.ToLower(n.Operator)

		// Sort spilling to disk (external merge) — SortSpaceType present and not "Memory".
		if strings.Contains(op, "sort") && n.SortSpaceType != "" && !strings.EqualFold(n.SortSpaceType, "Memory") {
			detail := fmt.Sprintf("排序下盘: %s 使用 %s", n.Operator,
				strings.TrimSpace(n.SortMethod+" "+n.SortSpaceType))
			if n.SortSpaceUsed > 0 {
				detail += fmt.Sprintf(" %dKB", n.SortSpaceUsed)
			}
			issues = append(issues, PlanIssue{
				Kind:       "sort_spill",
				Detail:     detail,
				Suggestion: "增大 work_mem,或加匹配排序的索引消除显式排序,避免外部归并下盘",
			})
		}

		// Expensive hash operator.
		if strings.Contains(op, "hash") && n.TotalCost >= 10000 {
			issues = append(issues, PlanIssue{
				Kind:       "expensive_hash",
				Detail:     fmt.Sprintf("高成本 Hash 算子: %s cost %.0f", n.Operator, n.TotalCost),
				Suggestion: "检查连接列索引与统计信息;评估能否减少 build 侧行数或改用 Merge/Index 连接",
			})
		}

		// Estimate-vs-actual row skew (ANALYZE only) — stale stats indicator.
		if hasAnalyze && n.ActualLoops > 0 && n.PlanRows > 0 && n.ActualRows > 0 {
			est, act := n.PlanRows, n.ActualRows
			if act > est*100 || est > act*100 {
				issues = append(issues, PlanIssue{
					Kind:       "row_estimate_skew",
					Detail:     fmt.Sprintf("%s 估算 %d 行 vs 实际 %d 行(偏差 >100x)", n.Operator, est, act),
					Suggestion: "统计信息可能陈旧:对相关表执行 ANALYZE 刷新;必要时建扩展统计信息",
				})
			}
		}
	})
	return issues
}

// walkPlan visits every node in the tree (pre-order).
func walkPlan(n *PlanNode, fn func(*PlanNode)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walkPlan(c, fn)
	}
}

// Type-tolerant accessors for EXPLAIN JSON maps (all JSON numbers decode as
// float64 through encoding/json, but be defensive).
func pgStr(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func pgFloat(m map[string]any, k string) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

func pgInt(m map[string]any, k string) int64 {
	switch v := m[k].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}
