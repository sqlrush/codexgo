package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// helpGroups is the command catalog grouped by domain. Rendered as aligned ASCII
// tables (command + purpose). Command cells use a short <required>/[optional]
// notation; full JSON args are noted in the purpose. Kept in sync with the
// registered tools.
var helpGroups = []struct {
	Title string
	Rows  [][]string // {command, purpose}
}{
	{"连接(默认自动连 ~/.dbaa)", [][]string{
		{"/connect", "连接默认 GaussDB/openGauss 连接"},
		{`/connect {"name":..}`, "连接 ~/.dbaa 里指定名字的连接"},
		{`/connect {"host":..}`, "显式 host/port/user/password 连接"},
	}},
	{"诊断 / 体检(AI)", [][]string{
		{"/health", "开放式诊断:6 维证据 + AI 根因/方案"},
		{"/ash", "活跃会话采样 + 等待分布"},
		{"/alert", "各库死锁 / 冲突 / 临时文件告警"},
	}},
	{"会话 / 锁", [][]string{
		{"/sessions [active]", "全部连接 + 状态分布(active=仅活跃)"},
		{"/locks", "锁等待 + 阻塞链(ASCII 树)"},
		{"/lwlocks [limit]", "轻量锁 / 等待状态争用"},
		{"/longtx [limit]", "长事务(按时长分级)"},
	}},
	{"事务 / 空间 / 膨胀", [][]string{
		{"/vacuum [limit]", "死元组 + autovacuum 状态(含触发阈值)"},
		{"/xid [limit]", "事务ID年龄 / 回卷风险"},
		{"/bloat [pct,limit]", "表膨胀估算(死元组比)"},
		{"/space [limit]", "空间使用(库→表→TOAST)"},
		{"/tempusage", "临时文件 / 落盘 + work_mem"},
		{"/hotkey [limit]", "热点表(读写活动排名)"},
	}},
	{"内存 / WAL / 复制", [][]string{
		{"/gsmem", "引擎内存 + 命中率 + Top 会话"},
		{"/wal", "WAL / 检查点 / 归档"},
		{"/replication", "流复制(主备)+ 复制槽"},
		{"/bgworker", "后台进程状态"},
	}},
	{"系统 / 元数据", [][]string{
		{"/resource", "连接/WALsender/worker 限额使用率"},
		{"/os", "主机 load / CPU / 内存"},
		{"/users [pattern]", "角色 / 权限 / 密码过期"},
		{"/params [pattern]", "参数搜索(含生效方式)"},
		{"/sqlcount [limit]", "SQL 类型计数(gs_sql_count)"},
		{"/tableinfo <表>", "表结构(列/索引/约束/统计)"},
	}},
	{"SQL 性能 / 调优", [][]string{
		{"/slowsql [ms,limit]", "慢 SQL(按平均单次耗时)"},
		{"/topsql [sort,limit]", "Top SQL(sort: el/ae/ex/lr/rw)"},
		{"/explain <sql>", "执行计划 + 风险标注"},
		{"/sqlfetch <sql_id>", "按 SQL_ID 还原语句"},
		{"/sqltune <sql|id>", "一轮深度调优(计划+[Pn]+AI方案)"},
		{"/indexadvise <sql>", "单条 SQL 索引建议"},
		{"/planhistory <sql_id>", "执行计划历史(判定计划回退)"},
	}},
	{"WDR / 趋势", [][]string{
		{"/wdr [limit]", "列出 WDR 快照"},
		{"/wdranalyze [begin,end]", "生成并分析 WDR 报告(AI)"},
		{"/perfsnap [action]", "性能快照 snap/list/compare/baseline"},
	}},
	{"其它", [][]string{
		{"/help", "显示本帮助"},
	}},
}

func buildHelp() string {
	var b strings.Builder
	b.WriteString("# 🧰 GaussDB 插件命令 · codexgo-db-gaussdb\n\n")
	b.WriteString("> 无参命令直接回车;带参命令传 JSON 对象(如 `/slowsql {\"limit\":10}`、`/sqltune {\"sql_or_id\":\"123\"}`)。所有命令**只读**。AI 命令(health/sqltune/wdranalyze)用自然语言提问可获得 AI 分析。\n\n")
	cols := []tableColumn{
		{Header: "命令", Max: 24},
		{Header: "作用", Max: 44},
	}
	for _, g := range helpGroups {
		b.WriteString("## " + g.Title + "\n\n```\n")
		b.WriteString(asciiTable(cols, g.Rows))
		b.WriteString("```\n\n")
	}
	return b.String()
}

func registerHelp(s *mcp.Server) {
	tool := mcp.Tool{
		Name:        "help",
		Description: "List the available GaussDB plugin commands and their arguments, grouped by domain as tables. No database connection required. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(_ context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(buildHelp(), "user"),
			mcp.TextContentFor("命令目录(表格)已展示给用户。可据其建议下一步用哪个命令。", "assistant"),
		}}, nil
	})
}
