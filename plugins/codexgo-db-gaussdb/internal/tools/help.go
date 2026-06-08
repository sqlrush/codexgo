package tools

import (
	"context"
	"encoding/json"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// helpText is the command catalog, mirroring opendb's /help. It is plain text
// (codexgo renders it as-is) and needs no database connection.
const helpText = `GaussDB 插件命令 (codexgo-db-gaussdb) —— 与 opendb 命令名一致

连接(默认读取 ~/.dbaa/config.yaml,健康检查等命令会自动连接默认库)
  /connect                      连接 ~/.dbaa 里的默认 GaussDB/openGauss 连接
  /connect {"name":"gauss_local"}   连接 ~/.dbaa 里指定名字的连接
  /connect {"host":"…","port":8000,"user":"…","password":"…","database":"postgres"}
                                显式连接(覆盖配置)

健康检查
  /health                       实例整体体检(加权健康分 0-100 + 分项评级)
  /slowsql [{"threshold_ms":1000,"limit":20}]   慢 SQL(按平均单次耗时)
  /topsql  [{"sort":"el|ae|ex|lr|rw","limit":20}]  Top SQL(按所选维度)
  /ash                          活动会话采样 + 等待分布
  /indexhealth [{"limit":20}]   索引健康(未使用/失效/重复/大索引)

SQL 调优
  /explain {"sql":"SELECT …","analyze":false}   执行计划 + 低效算子标注
  /sqlfetch {"sql_id":"…"}      按 SQL_ID 还原语句
  /sqltune {"sql_or_id":"…","analyze":false}    调优素材(计划+索引建议+五维清单)
  /planhistory {"sql_id":"…","limit":10}        执行计划历史(判定计划回退)

WDR(工作负载诊断报告)
  /wdr [{"limit":20}]           列出可用 WDR 快照
  /wdranalyze [{"begin":N,"end":M,"detail":"summary|all"}]  生成并分析 WDR 报告

其它
  /help                         显示本帮助

说明:无参命令直接回车;带参命令传 JSON 对象。所有命令只读,不会修改数据或配置。`

func registerHelp(s *mcp.Server) {
	tool := mcp.Tool{
		Name:        "help",
		Description: "List the available GaussDB plugin commands and their arguments. No database connection required. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(_ context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		return mcp.CallToolResult{Content: []mcp.ContentItem{mcp.TextContent(helpText)}}, nil
	})
}
