package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /perfsnap — point-in-time performance snapshots with delta comparison. A
// snapshot captures cumulative counters (from pg_stat_database + bgwriter) to a
// ring buffer persisted under ~/.codexgo/perfsnap/. `compare` diffs two
// snapshots into per-second rates — the lightweight, MCP-friendly stand-in for
// opendb's interactive dbtop.
//
// Actions: snap (default, capture) · list · compare [idA idB] (default: latest
// two) · baseline [id] (mark/unmark a baseline).

const perfSnapMax = 144

// perfMetric is one captured counter (ordered for stable rendering).
var perfMetrics = []struct{ Key, Label, SQL string }{
	{"xact_commit", "事务提交", "sum(xact_commit)"},
	{"xact_rollback", "事务回滚", "sum(xact_rollback)"},
	{"blks_hit", "缓存命中块", "sum(blks_hit)"},
	{"blks_read", "磁盘读块", "sum(blks_read)"},
	{"tup_inserted", "插入行", "sum(tup_inserted)"},
	{"tup_updated", "更新行", "sum(tup_updated)"},
	{"tup_deleted", "删除行", "sum(tup_deleted)"},
	{"temp_files", "临时文件", "sum(temp_files)"},
	{"temp_bytes", "临时字节", "sum(temp_bytes)"},
	{"deadlocks", "死锁", "sum(deadlocks)"},
	{"conflicts", "冲突", "sum(conflicts)"},
}

type perfSnapshot struct {
	ID       int                `json:"id"`
	Taken    string             `json:"taken"`
	Unix     int64              `json:"unix"`
	Baseline bool               `json:"baseline,omitempty"`
	Metrics  map[string]float64 `json:"metrics"`
}

func registerPerfSnap(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "perfsnap",
		Description: "Capture and compare point-in-time GaussDB/openGauss performance snapshots (cumulative counters from pg_stat_database + bgwriter), persisted under ~/.codexgo/perfsnap/. Actions: snap (default, capture a new snapshot) · list (show saved) · compare [idA idB] (per-second delta of two snapshots, default the latest two) · baseline [id]. Renders directly to the user. Read-only against the DB.",
		InputSchema: jsonObjSchema(map[string]any{
			"action": strProp("snap (default) | list | compare | baseline"),
			"ids":    strProp("for compare: 'idA idB'; for baseline: 'id' (default latest)"),
		}),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		var a struct {
			Action string `json:"action"`
			IDs    string `json:"ids"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		action := strings.ToLower(strings.TrimSpace(a.Action))
		if action == "" {
			action = "snap"
		}
		store, _ := loadPerfStore()

		var userText, digest string
		switch action {
		case "snap":
			if err := ensureConn(ctx, conn); err != nil {
				return mcp.CallToolResult{}, err
			}
			snap, err := capturePerfSnapshot(ctx, conn)
			if err != nil {
				return mcp.CallToolResult{}, err
			}
			snap.ID = nextPerfID(store)
			store = append(store, snap)
			if len(store) > perfSnapMax {
				store = store[len(store)-perfSnapMax:]
			}
			_ = savePerfStore(store)
			userText = renderPerfSnap(snap, len(store))
			digest = fmt.Sprintf("已捕获性能快照 #%d(共 %d 个);用 perfsnap compare 看两次间隔的每秒速率。", snap.ID, len(store))
		case "list":
			userText = renderPerfList(store)
			digest = monDigest("快照列表", len(store), "可对两个快照 compare 看变化")
		case "baseline":
			id := firstInt(a.IDs)
			userText = markBaseline(&store, id)
			_ = savePerfStore(store)
			digest = "已更新基线标记。"
		case "compare":
			userText = renderPerfCompare(store, a.IDs)
			digest = "性能对比(每秒速率)已渲染;可点评读写/命中/落盘变化。"
		default:
			userText = "未知 action;支持 snap | list | compare | baseline。"
			digest = userText
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(userText, "user"),
			mcp.TextContentFor(digest, "assistant"),
		}}, nil
	})
}

func capturePerfSnapshot(ctx context.Context, conn *db.Conn) (perfSnapshot, error) {
	var sel []string
	for _, m := range perfMetrics {
		sel = append(sel, "COALESCE("+m.SQL+",0)")
	}
	res, err := conn.Query(ctx, "SELECT "+strings.Join(sel, ", ")+" FROM pg_stat_database")
	if err != nil {
		return perfSnapshot{}, err
	}
	now := time.Now()
	snap := perfSnapshot{Taken: now.Format("2006-01-02 15:04:05"), Unix: now.Unix(), Metrics: map[string]float64{}}
	if len(res.Rows) > 0 {
		r := res.Rows[0]
		for i, m := range perfMetrics {
			if i < len(r) {
				snap.Metrics[m.Key] = atof(r[i])
			}
		}
	}
	if v, err := conn.QueryScalar(ctx, "SELECT checkpoints_timed + checkpoints_req FROM pg_stat_bgwriter"); err == nil {
		snap.Metrics["checkpoints"] = atof(v)
	}
	return snap, nil
}

func renderPerfSnap(snap perfSnapshot, total int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# 📸 性能快照 #%d · %s\n\n", snap.ID, snap.Taken))
	b.WriteString(fmt.Sprintf("> 已保存(共 %d 个,环形上限 %d)。累计计数器,用 compare 看两次间隔速率。\n\n```\n", total, perfSnapMax))
	cols := []tableColumn{{Header: "指标", Max: 16}, {Header: "累计值", Right: true}}
	var rows [][]string
	for _, m := range perfMetrics {
		rows = append(rows, []string{m.Label, fmt.Sprintf("%.0f", snap.Metrics[m.Key])})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n")
	return b.String()
}

func renderPerfList(store []perfSnapshot) string {
	var b strings.Builder
	b.WriteString("# 📸 性能快照列表\n\n")
	if len(store) == 0 {
		b.WriteString("暂无快照。用 perfsnap(action=snap)捕获第一个。\n")
		return b.String()
	}
	b.WriteString("```\n")
	cols := []tableColumn{{Header: "ID", Right: true}, {Header: "时间", Max: 20}, {Header: "基线"}}
	var rows [][]string
	for _, s := range store {
		base := "-"
		if s.Baseline {
			base = "★"
		}
		rows = append(rows, []string{strconv.Itoa(s.ID), s.Taken, base})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n> perfsnap compare 默认对比最近两个;或传 ids='idA idB'。\n")
	return b.String()
}

func renderPerfCompare(store []perfSnapshot, ids string) string {
	a, c, ok := pickCompare(store, ids)
	if !ok {
		return "# 📸 性能对比\n\n需要至少两个快照(或指定有效的 ids='idA idB')。先用 perfsnap 多捕获几个。\n"
	}
	secs := float64(c.Unix - a.Unix)
	if secs <= 0 {
		secs = 1
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# 📸 性能对比 · #%d → #%d\n\n", a.ID, c.ID))
	b.WriteString(fmt.Sprintf("> 间隔 %s(%.0fs);速率 = 增量 / 间隔。\n\n```\n", humanSecs(secs), secs))
	cols := []tableColumn{
		{Header: "指标", Max: 16},
		{Header: "增量", Right: true},
		{Header: "每秒", Right: true},
	}
	var rows [][]string
	for _, m := range perfMetrics {
		delta := c.Metrics[m.Key] - a.Metrics[m.Key]
		rows = append(rows, []string{m.Label, fmt.Sprintf("%.0f", delta), fmt.Sprintf("%.2f", delta/secs)})
	}
	// derived: cache hit ratio over the interval
	dh := c.Metrics["blks_hit"] - a.Metrics["blks_hit"]
	dr := c.Metrics["blks_read"] - a.Metrics["blks_read"]
	if dh+dr > 0 {
		rows = append(rows, []string{"区间命中率", fmt.Sprintf("%.1f%%", 100*dh/(dh+dr)), "-"})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n")
	return b.String()
}

// pickCompare resolves the two snapshots to compare (explicit ids, or the
// latest two), oldest first.
func pickCompare(store []perfSnapshot, ids string) (perfSnapshot, perfSnapshot, bool) {
	byID := map[int]perfSnapshot{}
	for _, s := range store {
		byID[s.ID] = s
	}
	fields := strings.Fields(ids)
	if len(fields) >= 2 {
		ia, _ := strconv.Atoi(fields[0])
		ib, _ := strconv.Atoi(fields[1])
		a, oka := byID[ia]
		c, okc := byID[ib]
		if oka && okc {
			if a.Unix > c.Unix {
				a, c = c, a
			}
			return a, c, true
		}
	}
	if len(store) >= 2 {
		sorted := append([]perfSnapshot(nil), store...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Unix < sorted[j].Unix })
		return sorted[len(sorted)-2], sorted[len(sorted)-1], true
	}
	return perfSnapshot{}, perfSnapshot{}, false
}

func markBaseline(store *[]perfSnapshot, id int) string {
	if len(*store) == 0 {
		return "暂无快照可标记。"
	}
	if id <= 0 {
		id = (*store)[len(*store)-1].ID
	}
	found := false
	for i := range *store {
		if (*store)[i].ID == id {
			(*store)[i].Baseline = true
			found = true
		}
	}
	if !found {
		return fmt.Sprintf("未找到快照 #%d。", id)
	}
	return fmt.Sprintf("✅ 已将快照 #%d 标记为基线。", id)
}

// --- persistence ------------------------------------------------------------

func perfStorePath() string {
	home := os.Getenv("CODEXGO_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".codexgo")
		}
	}
	return filepath.Join(home, "perfsnap", "snapshots.json")
}

func loadPerfStore() ([]perfSnapshot, error) {
	data, err := os.ReadFile(perfStorePath())
	if err != nil {
		return nil, err
	}
	var store []perfSnapshot
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return store, nil
}

func savePerfStore(store []perfSnapshot) error {
	path := perfStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func nextPerfID(store []perfSnapshot) int {
	max := 0
	for _, s := range store {
		if s.ID > max {
			max = s.ID
		}
	}
	return max + 1
}

func firstInt(s string) int {
	for _, f := range strings.Fields(s) {
		if n, err := strconv.Atoi(f); err == nil {
			return n
		}
	}
	return 0
}
