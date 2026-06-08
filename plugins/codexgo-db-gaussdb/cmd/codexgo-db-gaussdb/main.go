// Command codexgo-db-gaussdb is a standalone MCP server (stdio) exposing GaussDB
// health-check and SQL-tuning tools to codexgo. It is an INDEPENDENT module:
// codexgo never imports it — it is launched as an external MCP plugin process and
// spoken to over JSON-RPC. Domain knowledge ships as the MCP `instructions`
// string (not a SKILL.md), per docs/PLUGIN-DB-DESIGN.zh-CN.md.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/tools"
)

const version = "0.4.1"

// instructions is the server-level domain knowledge codexgo surfaces to the LLM
// at initialize. It tells the model what this server is for and how to drive the
// tools — the MCP-native replacement for opendb's per-DBType SKILL files.
const instructions = `GaussDB 数据库专家插件 (codexgo-db-gaussdb)。

提供 GaussDB / openGauss 实例的健康检查与 SQL 调优能力,所有工具均为只读
(不会修改数据或配置)。每个工具返回结构化 JSON(含具体数值、阈值、OK/WARN/FAIL
评级与处置建议),由 codexgo 负责渲染表格/面板 —— 不要把返回的 JSON 原样贴给用户,
请基于其中的 score/grade/items 用自然语言给出诊断结论与优先级建议。

典型流程:
1. 先调用 connect 建立连接(host/port/user 必填;password 可由 codexgo 通过
   交互式输入安全收集;database 默认 postgres;sslmode 默认 disable)。
2. 再调用 health 做整体体检,得到加权健康分(0-100)与分项评级。
3. 针对体检暴露的问题,使用 SQL 调优类工具(slowsql / topsql / explain
   等)逐项下钻定位。

GaussDB 使用专有驱动与 SCRAM-SHA256 认证,系统视图复用 openGauss(pg_stat_*、
dbe_perf.*)。当某项探针因权限或视图缺失而失败时,该项会标记为 UNKNOWN 且不影响
其余检查 —— 请在结论中说明哪些项目未能采集。`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "codexgo-db-gaussdb:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conn := db.New()
	defer conn.Close()

	srv := mcp.NewServer("codexgo-db-gaussdb", version, instructions)
	tools.Register(srv, conn)

	return srv.Serve(ctx, os.Stdin, os.Stdout)
}
