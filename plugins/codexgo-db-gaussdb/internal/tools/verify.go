package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
)

// Rewrite verification (sqltune parity #11 cost diff + #12 equivalence). Given a
// candidate rewrite, compare its planned cost to the original and (optionally)
// check result equivalence by hashing a bounded row sample. This turns "the
// model suggests a rewrite" into "the rewrite is verified cheaper AND equivalent"
// — catching no-op or wrong rewrites deterministically.

// RewriteCandidate is a candidate rewrite plus its verification verdict.
type RewriteCandidate struct {
	Rule       string   `json:"rule"`
	SQL        string   `json:"sql"`
	BeforeCost *float64 `json:"before_cost,omitempty"`
	AfterCost  *float64 `json:"after_cost,omitempty"`
	CostRatio  *float64 `json:"cost_ratio,omitempty"` // before/after; >1 means cheaper
	Equivalent string   `json:"equivalent,omitempty"` // yes | no | unverified:<reason> | skipped
	Note       string   `json:"note,omitempty"`
}

const equivTimeout = 30 * time.Second
const equivLimit = 1000

// verifyCandidate fills the candidate's cost diff and (when doEquiv) equivalence
// verdict against the original SQL. Both SQLs must already be EXPLAIN-able
// (placeholders substituted). Cost comparison is plan-only (cheap); equivalence
// EXECUTES both queries (bounded sample + timeout) so it is opt-in.
func verifyCandidate(ctx context.Context, conn *db.Conn, origSQL string, c *RewriteCandidate, doEquiv bool) {
	if bc, err := planTotalCost(ctx, conn, origSQL); err == nil {
		c.BeforeCost = bc
	}
	ac, err := planTotalCost(ctx, conn, c.SQL)
	if err != nil {
		c.Note = strings.TrimSpace(c.Note + " 候选无法 EXPLAIN: " + firstLine(err.Error()))
		c.Equivalent = "skipped"
		return
	}
	c.AfterCost = ac
	if c.BeforeCost != nil && ac != nil && *ac > 0 {
		ratio := *c.BeforeCost / *ac
		c.CostRatio = &ratio
	}

	if !doEquiv {
		c.Equivalent = "skipped"
		return
	}
	if !isReadOnlySQL(stripLeadingExplain(origSQL)) || !isReadOnlySQL(stripLeadingExplain(c.SQL)) {
		c.Equivalent = "skipped:非只读语句不做等价校验"
		return
	}
	ectx, cancel := context.WithTimeout(ctx, equivTimeout)
	defer cancel()
	oh, err1 := rowHash(ectx, conn, origSQL)
	ch, err2 := rowHash(ectx, conn, c.SQL)
	switch {
	case err1 != nil:
		c.Equivalent = "unverified:原查询采样失败(" + firstLine(err1.Error()) + ")"
	case err2 != nil:
		c.Equivalent = "unverified:候选采样失败(" + firstLine(err2.Error()) + ")"
	case oh == ch && oh != "":
		c.Equivalent = "yes"
	default:
		c.Equivalent = "no"
	}
}

// rowHash hashes a bounded, order-independent sample of a query's result, so two
// queries returning the same rows (any order) hash equal. Ported from opendb's
// runRowHash.
func rowHash(ctx context.Context, conn *db.Conn, query string) (string, error) {
	hashSQL := fmt.Sprintf(`SELECT md5(string_agg(row_text, '|' ORDER BY row_text))
FROM (SELECT (sub.*)::text AS row_text FROM (%s) AS sub LIMIT %d) t`,
		stripLeadingExplain(query), equivLimit)
	return conn.QueryScalar(ctx, hashSQL)
}
