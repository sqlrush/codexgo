package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
)

// Plan cost extraction (sqltune parity #4) and EXPLAIN PERFORMANCE (#5).

// planTotalCost runs EXPLAIN (FORMAT JSON) and returns the top node's total
// cost. Plan-only (no ANALYZE), so it does NOT execute the query — safe even for
// very slow statements. Returns nil when unavailable.
func planTotalCost(ctx context.Context, conn *db.Conn, sql string) (*float64, error) {
	res, err := conn.Query(ctx, fmt.Sprintf("EXPLAIN (FORMAT JSON, COSTS TRUE) %s", stripLeadingExplain(sql)))
	if err != nil {
		return nil, err
	}
	if len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
		return nil, fmt.Errorf("empty explain json")
	}
	// openGauss may return the JSON across rows; join them.
	var b strings.Builder
	for _, row := range res.Rows {
		if len(row) > 0 {
			b.WriteString(row[0])
		}
	}
	cost, ok := parseTopTotalCost(b.String())
	if !ok {
		return nil, fmt.Errorf("could not find Total Cost in explain json")
	}
	return &cost, nil
}

// parseTopTotalCost extracts Plan.Total Cost from an EXPLAIN FORMAT JSON document
// ([{"Plan":{"Total Cost":N,...}}]).
func parseTopTotalCost(s string) (float64, bool) {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &arr); err != nil || len(arr) == 0 {
		return 0, false
	}
	planRaw, ok := arr[0]["Plan"]
	if !ok {
		return 0, false
	}
	var plan map[string]json.RawMessage
	if err := json.Unmarshal(planRaw, &plan); err != nil {
		return 0, false
	}
	tc, ok := plan["Total Cost"]
	if !ok {
		return 0, false
	}
	var cost float64
	if err := json.Unmarshal(tc, &cost); err != nil {
		return 0, false
	}
	return cost, true
}

// explainPerformance runs openGauss `EXPLAIN PERFORMANCE` (per-operator actual
// timing/rows). It EXECUTES the query, so it is gated by the caller (only on
// explicit request). Returns the raw text.
func explainPerformance(ctx context.Context, conn *db.Conn, sql string) (string, error) {
	res, err := conn.Query(ctx, fmt.Sprintf("EXPLAIN PERFORMANCE %s", stripLeadingExplain(sql)))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, row := range res.Rows {
		if len(row) > 0 {
			b.WriteString(row[0])
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}
