package tools

import (
	"context"
	"encoding/json"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// helpText is the full command catalog, rendered as markdown so codexgo styles
// the headings/lists. Grouped by domain; kept in sync with the registered tools.
const helpText = "# 🧰 GaussDB 插件命令 · codexgo-db-gaussdb\n\n" +
	"> 命令名与 opendb 一致。无参命令直接回车;带参命令传 JSON 对象(如 `/slowsql {\"limit\":10}`)。**全部只读**,不改数据或配置。\n\n" +
	"## 连接\n" +
	"默认读取 `~/.dbaa/config.yaml`,大多数命令会自动连接默认库。\n\n" +
	"- `/connect` — 连接默认连接;`/connect {\"name\":\"gauss_local\"}` 指定名字;`/connect {\"host\":\"…\",\"port\":8000,\"user\":\"…\",\"password\":\"…\"}` 显式连接\n\n" +
	"## 诊断 / 体检\n" +
	"- `/health` — 开放式诊断:6 维证据 + 模型综合根因与分级方案\n" +
	"- `/ash` — 活跃会话采样:等待分布 + 活动会话\n" +
	"- `/alert` — 各库异常计数(死锁 / 冲突 / 临时文件)\n\n" +
	"## 会话 / 锁\n" +
	"- `/sessions [{\"scope\":\"active\"}]` — 全部连接 + 状态分布\n" +
	"- `/locks` — 锁等待 + 阻塞链(ASCII 树)\n" +
	"- `/lwlocks [{\"limit\":20}]` — 轻量锁 / 等待状态争用\n" +
	"- `/longtx [{\"limit\":20}]` — 长事务(按时长分级)\n\n" +
	"## 事务 / 空间 / 膨胀\n" +
	"- `/vacuum [{\"limit\":30}]` — 死元组 + autovacuum 状态(含触发阈值)\n" +
	"- `/xid [{\"limit\":10}]` — 事务ID年龄 / 回卷风险\n" +
	"- `/bloat [{\"threshold_pct\":5,\"limit\":30}]` — 表膨胀估算\n" +
	"- `/space [{\"limit\":20}]` — 空间使用(库 → 表 → TOAST)\n" +
	"- `/tempusage` — 临时文件 / 落盘 + work_mem\n" +
	"- `/hotkey [{\"limit\":20}]` — 热点表(读写活动)\n\n" +
	"## 内存 / WAL / 复制\n" +
	"- `/gsmem` — 引擎内存 + 命中率 + Top 会话\n" +
	"- `/wal` — WAL / 检查点 / 归档\n" +
	"- `/replication` — 流复制(主备)+ 复制槽\n" +
	"- `/bgworker` — 后台进程状态\n\n" +
	"## 系统 / 元数据\n" +
	"- `/resource` — 连接 / WAL发送器 / 工作进程限额使用率\n" +
	"- `/os` — 主机 load / CPU / 内存(pv_os_run_info)\n" +
	"- `/users [{\"pattern\":\"…\"}]` — 角色 / 权限 / 密码过期\n" +
	"- `/params [{\"pattern\":\"work_mem\"}]` — 参数搜索(含生效方式)\n" +
	"- `/sqlcount [{\"limit\":20}]` — SQL 类型计数(gs_sql_count)\n" +
	"- `/tableinfo {\"table\":\"schema.table\"}` — 表结构(列 / 索引 / 约束 / 统计)\n\n" +
	"## SQL 性能 / 调优\n" +
	"- `/slowsql [{\"threshold_ms\":1000,\"limit\":20}]` — 慢 SQL(按平均单次耗时)\n" +
	"- `/topsql [{\"sort\":\"el|ae|ex|lr|rw\",\"limit\":20}]` — Top SQL\n" +
	"- `/explain {\"sql\":\"SELECT …\",\"analyze\":false}` — 执行计划 + 风险标注\n" +
	"- `/sqlfetch {\"sql_id\":\"…\"}` — 按 SQL_ID 还原语句\n" +
	"- `/sqltune {\"sql_or_id\":\"…\",\"analyze\":false}` — 一轮深度调优(证据 + 模型报告,关联 [Pn] 热点)\n" +
	"- `/indexadvise {\"sql\":\"SELECT …\"}` — 单条 SQL 索引建议(gs_index_advise)\n" +
	"- `/planhistory {\"sql_id\":\"…\",\"limit\":10}` — 执行计划历史(判定计划回退)\n\n" +
	"## WDR / 趋势\n" +
	"- `/wdr [{\"limit\":20}]` — 列出 WDR 快照\n" +
	"- `/wdranalyze [{\"begin\":N,\"end\":M,\"detail\":\"summary|all\"}]` — 生成并分析 WDR 报告\n" +
	"- `/perfsnap [{\"action\":\"snap|list|compare|baseline\"}]` — 性能快照与每秒速率对比\n\n" +
	"## 其它\n" +
	"- `/help` — 显示本帮助\n"

func registerHelp(s *mcp.Server) {
	tool := mcp.Tool{
		Name:        "help",
		Description: "List the available GaussDB plugin commands and their arguments, grouped by domain. No database connection required. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{}),
	}
	s.Register(tool, func(_ context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(helpText, "user"),
			mcp.TextContentFor("命令目录已展示给用户。可据其建议下一步用哪个命令。", "assistant"),
		}}, nil
	})
}
