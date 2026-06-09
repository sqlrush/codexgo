package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// Health item status levels (three-level, vs opendb's OK/WARN only).
const (
	statusOK      = "OK"
	statusWarn    = "WARN"
	statusFail    = "FAIL"
	statusUnknown = "UNKNOWN" // per-item probe failed; report didn't abort
)

// HealthItem is one structured check result. Carrying value+threshold+suggestion
// (not just a status string) is an optimization over opendb — codexgo renders a
// rich, explainable panel and the LLM can reason over precise numbers.
type HealthItem struct {
	Category   string `json:"category"`
	Name       string `json:"name"`
	Value      string `json:"value"`
	Threshold  string `json:"threshold,omitempty"`
	Status     string `json:"status"`
	Suggestion string `json:"suggestion,omitempty"`
}

// HealthReport is the structured health result codexgo renders.
type HealthReport struct {
	Target  string       `json:"target"`
	Score   int          `json:"score"` // 0-100 weighted health score
	Grade   string       `json:"grade"` // 优 / 良 / 警告 / 危险
	Counts  healthCounts `json:"counts"`
	Items   []HealthItem `json:"items"`
	Version string       `json:"version,omitempty"`
}

type healthCounts struct {
	OK   int `json:"ok"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

func registerHealth(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "health",
		Description: "Open-ended database DIAGNOSIS for the connected GaussDB/openGauss instance — use this for ANY \"当前数据库有什么问题 / what's wrong / health / 体检 / 性能 / 慢 / 锁 / diagnose\" question. The plugin deterministically collects 6 dimensions of EVIDENCE (health metrics + wait events + top slow SQL + lock-blocking chains + dead-tuple distribution + index summary) with accurate numbers and returns them together with a format reference. Based on this evidence, write ONE complete diagnosis report directly to the user in a single reply: keep the accurate numbers/tables, then add root-cause analysis (cross-dimension causal chains) and prioritized actions (P0/P1/P2 with executable SQL + risk). Do NOT call other tools for this. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(ctx context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		// Single-pass: multi-dimension collection (health + waits + slow SQL +
		// locks + dead tuples + indexes) — coverage is deterministic, not
		// model-chosen. The number-accurate evidence + a soft format reference go
		// to the model (assistant audience), which produces ONE final report from
		// it in its own reply. No rigid schema, no second tool call.
		d := collectDiagnosis(ctx, conn)
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(diagEvidenceWithTemplate(d), "assistant"),
		}}, nil
	})
}

// runHealth executes all checks. Each probe is independently fault-tolerant: a
// failing probe yields an UNKNOWN item rather than aborting the whole report
// (robustness optimization over opendb).
func runHealth(ctx context.Context, conn *db.Conn) HealthReport {
	r := HealthReport{Target: conn.Label()}

	if v, err := conn.QueryScalar(ctx, "SELECT version()"); err == nil {
		r.Version = firstLine(v)
	}

	r.Items = append(r.Items, checkUptime(ctx, conn))
	r.Items = append(r.Items, checkConnections(ctx, conn)...)
	r.Items = append(r.Items, checkCacheHit(ctx, conn))
	r.Items = append(r.Items, checkDeadTuples(ctx, conn))
	r.Items = append(r.Items, checkXIDAge(ctx, conn))
	r.Items = append(r.Items, checkReplication(ctx, conn))

	for _, it := range r.Items {
		switch it.Status {
		case statusOK:
			r.Counts.OK++
		case statusWarn:
			r.Counts.Warn++
		case statusFail:
			r.Counts.Fail++
		}
	}
	r.Score, r.Grade = scoreAndGrade(r.Counts)
	return r
}

// scoreAndGrade computes a weighted health score and a grade. WARN costs 6,
// FAIL costs 18 (clamped at 0). This single headline number is an addition
// over opendb's flat item list.
func scoreAndGrade(c healthCounts) (int, string) {
	score := 100 - c.Warn*6 - c.Fail*18
	if score < 0 {
		score = 0
	}
	switch {
	case c.Fail > 0 || score < 60:
		return score, "危险"
	case score < 80:
		return score, "警告"
	case score < 95:
		return score, "良"
	default:
		return score, "优"
	}
}

// --- individual checks -----------------------------------------------------

func checkUptime(ctx context.Context, conn *db.Conn) HealthItem {
	it := HealthItem{Category: "实例", Name: "运行时长", Threshold: "≥ 1h"}
	sec, err := conn.QueryScalar(ctx,
		"SELECT EXTRACT(EPOCH FROM clock_timestamp() - pg_postmaster_start_time())::bigint")
	if err != nil {
		return unknown(it, err)
	}
	s, _ := strconv.ParseFloat(strings.TrimSpace(sec), 64)
	it.Value = humanDuration(int64(s))
	if s < 3600 {
		it.Status, it.Suggestion = statusWarn, "实例近期重启,确认是否计划内"
	} else {
		it.Status = statusOK
	}
	return it
}

func checkConnections(ctx context.Context, conn *db.Conn) []HealthItem {
	res, err := conn.Query(ctx, `SELECT
  (SELECT setting::int FROM pg_settings WHERE name='max_connections') AS max_conn,
  (SELECT count(*) FROM pg_stat_activity) AS total,
  (SELECT count(*) FROM pg_stat_activity WHERE state='active') AS active,
  (SELECT count(*) FROM pg_stat_activity WHERE state='idle in transaction') AS idle_tx`)
	base := HealthItem{Category: "连接"}
	if err != nil || len(res.Rows) == 0 {
		return []HealthItem{unknown(HealthItem{Category: "连接", Name: "连接使用率"}, err)}
	}
	row := res.Rows[0]
	maxConn := atof(row[0])
	total := atof(row[1])
	active := atof(row[2])
	idleTx := atof(row[3])

	var items []HealthItem

	usage := HealthItem{Category: "连接", Name: "连接使用率", Threshold: "WARN≥80% FAIL≥95%"}
	pct := 0.0
	if maxConn > 0 {
		pct = total / maxConn * 100
	}
	usage.Value = fmt.Sprintf("%.0f/%.0f (%.1f%%)", total, maxConn, pct)
	usage.Status = level(pct, 80, 95)
	if usage.Status != statusOK {
		usage.Suggestion = "排查连接泄漏或上调 max_connections / 引入连接池"
	}
	items = append(items, usage)

	act := HealthItem{Category: "连接", Name: "活动会话", Threshold: "WARN≥50 FAIL≥200"}
	act.Value = fmt.Sprintf("%.0f", active)
	act.Status = level(active, 50, 200)
	if act.Status != statusOK {
		act.Suggestion = "活动会话偏高,结合 slowsql / ash 定位热点"
	}
	items = append(items, act)

	idle := HealthItem{Category: "连接", Name: "事务中空闲(idle in tx)", Threshold: "WARN≥10"}
	idle.Value = fmt.Sprintf("%.0f", idleTx)
	idle.Status = base.threshold1(idleTx, 10)
	if idle.Status != statusOK {
		idle.Suggestion = "存在长事务/未提交,可能阻塞 vacuum 与持锁,及时提交或回滚"
	}
	items = append(items, idle)
	return items
}

func checkCacheHit(ctx context.Context, conn *db.Conn) HealthItem {
	it := HealthItem{Category: "内存", Name: "缓存命中率", Threshold: "WARN<99% FAIL<95%"}
	res, err := conn.Query(ctx,
		"SELECT COALESCE(sum(blks_hit),0), COALESCE(sum(blks_read),0) FROM pg_stat_database")
	if err != nil || len(res.Rows) == 0 {
		return unknown(it, err)
	}
	hit := atof(res.Rows[0][0])
	read := atof(res.Rows[0][1])
	total := hit + read
	pct := 100.0
	if total > 0 {
		pct = hit / total * 100
	}
	it.Value = fmt.Sprintf("%.2f%%", pct)
	switch {
	case pct < 95:
		it.Status, it.Suggestion = statusFail, "命中率过低,考虑增大 shared_buffers 或排查全表扫描"
	case pct < 99:
		it.Status, it.Suggestion = statusWarn, "命中率偏低,关注热点表索引与内存配置"
	default:
		it.Status = statusOK
	}
	return it
}

func checkDeadTuples(ctx context.Context, conn *db.Conn) HealthItem {
	it := HealthItem{Category: "维护", Name: "死元组总数", Threshold: "WARN>100k"}
	v, err := conn.QueryScalar(ctx, "SELECT COALESCE(sum(n_dead_tup),0) FROM pg_stat_user_tables")
	if err != nil {
		return unknown(it, err)
	}
	n := atof(v)
	it.Value = fmt.Sprintf("%.0f", n)
	it.Status = it.threshold1(n, 100000)
	if it.Status != statusOK {
		it.Suggestion = "死元组堆积,检查 autovacuum 是否生效,必要时手动 VACUUM"
	}
	return it
}

func checkXIDAge(ctx context.Context, conn *db.Conn) HealthItem {
	it := HealthItem{Category: "维护", Name: "事务ID回卷风险", Threshold: "WARN≥50% FAIL≥80%"}
	v, err := conn.QueryScalar(ctx, `SELECT txid_current() - max(CAST(datfrozenxid::text AS numeric))
FROM pg_database WHERE datname=current_database()`)
	if err != nil {
		return unknown(it, err)
	}
	age := atof(v)
	const wrap = 2147483647.0
	pct := age / wrap * 100
	it.Value = fmt.Sprintf("%.1f%% (age=%.0f)", pct, age)
	it.Status = level(pct, 50, 80)
	if it.Status != statusOK {
		it.Suggestion = "回卷风险升高,尽快对高龄表执行 VACUUM (FREEZE)"
	}
	return it
}

func checkReplication(ctx context.Context, conn *db.Conn) HealthItem {
	it := HealthItem{Category: "高可用", Name: "备库连接数", Threshold: "信息项"}
	v, err := conn.QueryScalar(ctx, "SELECT count(*) FROM pg_stat_replication")
	if err != nil {
		return unknown(it, err)
	}
	it.Value = v
	it.Status = statusOK
	if atof(v) == 0 {
		it.Suggestion = "无备库连接,确认是否单机部署或备库是否掉线"
	}
	return it
}

// --- helpers ---------------------------------------------------------------

// threshold1 is a single-threshold (WARN-only) classifier on a HealthItem.
func (HealthItem) threshold1(v, warn float64) string {
	if v >= warn {
		return statusWarn
	}
	return statusOK
}

// level classifies v against warn/fail thresholds (FAIL ≥ fail > WARN ≥ warn).
func level(v, warn, fail float64) string {
	switch {
	case v >= fail:
		return statusFail
	case v >= warn:
		return statusWarn
	default:
		return statusOK
	}
}

func unknown(it HealthItem, err error) HealthItem {
	it.Status = statusUnknown
	if err != nil {
		it.Value = "采集失败: " + firstLine(err.Error())
	}
	return it
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func humanDuration(sec int64) string {
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}
