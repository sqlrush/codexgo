package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /params — search GaussDB/openGauss configuration parameters (pg_settings).
// Read-only. Surfaces the change scope (restart / reload / dynamic) so the user
// knows how a parameter takes effect.

const paramsCurated = `'shared_buffers','work_mem','maintenance_work_mem','effective_cache_size',
'max_connections','max_process_memory','max_dynamic_memory',
'wal_level','max_wal_size','min_wal_size','checkpoint_timeout','checkpoint_completion_target',
'autovacuum','autovacuum_max_workers','autovacuum_vacuum_scale_factor',
'enable_seqscan','enable_nestloop','random_page_cost','synchronous_commit','fsync'`

type paramRow struct{ Name, Value, Category, Mutability, Desc string }

type paramsData struct {
	Target  string
	Pattern string
	Rows    []paramRow
}

func registerParams(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "params",
		Description: "Search GaussDB/openGauss configuration parameters (pg_settings) by name/category substring; with no pattern, shows a curated set of important parameters. Each row shows value, category, change scope (restart/reload/dynamic) and description. Renders a table directly to the user. Read-only. Optional arg pattern.",
		InputSchema: jsonObjSchema(map[string]any{"pattern": strProp("substring to match parameter name or category")}),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			Pattern string `json:"pattern"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		d, err := collectParams(ctx, conn, strings.TrimSpace(a.Pattern))
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderParams(d), "user"),
			mcp.TextContentFor(monDigest("参数", len(d.Rows), "勿复述;可解释关键参数含义与是否需重启生效"), "assistant"),
		}}, nil
	})
}

func collectParams(ctx context.Context, conn *db.Conn, pattern string) (*paramsData, error) {
	d := &paramsData{Target: conn.Label(), Pattern: pattern}
	var sqlStr string
	if pattern == "" {
		sqlStr = `SELECT name, setting, COALESCE(unit,''), category, context, COALESCE(short_desc,'')
FROM pg_settings WHERE name IN (` + paramsCurated + `) ORDER BY category, name`
	} else {
		p := strings.ReplaceAll(pattern, "'", "''")
		sqlStr = fmt.Sprintf(`SELECT name, setting, COALESCE(unit,''), category, context, COALESCE(short_desc,'')
FROM pg_settings WHERE name ILIKE '%%%s%%' OR category ILIKE '%%%s%%' ORDER BY category, name LIMIT 100`, p, p)
	}
	res, err := conn.Query(ctx, sqlStr)
	if err != nil {
		return nil, err
	}
	for _, r := range res.Rows {
		if len(r) < 6 {
			continue
		}
		val := strings.TrimSpace(r[1] + " " + r[2])
		d.Rows = append(d.Rows, paramRow{Name: r[0], Value: val, Category: r[3], Mutability: mutability(r[4]), Desc: r[5]})
	}
	return d, nil
}

// mutability maps a pg_settings context to how a change takes effect.
func mutability(context string) string {
	switch context {
	case "postmaster":
		return "需重启"
	case "sighup":
		return "reload"
	case "backend", "superuser-backend":
		return "新连接"
	case "user", "superuser":
		return "动态"
	default:
		return context
	}
}

func renderParams(d *paramsData) string {
	var b strings.Builder
	b.WriteString("# ⚙️ 参数 · " + d.Target + "\n\n")
	if d.Pattern == "" {
		b.WriteString("> 常用参数精选(传 pattern 可搜索全部)。\n\n")
	} else {
		b.WriteString("> 匹配 \"" + d.Pattern + "\" 的参数(上限 100)。\n\n")
	}
	if len(d.Rows) == 0 {
		b.WriteString("无匹配参数。\n")
		return b.String()
	}
	b.WriteString("```\n")
	cols := []tableColumn{
		{Header: "参数", Max: 32},
		{Header: "值", Max: 22},
		{Header: "生效"},
		{Header: "说明", Max: 40},
	}
	var rows [][]string
	for _, r := range d.Rows {
		rows = append(rows, []string{r.Name, r.Value, r.Mutability, r.Desc})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n> 生效:需重启=postmaster 级;reload=sighup(pg_reload_conf);动态=会话级。\n")
	return b.String()
}
