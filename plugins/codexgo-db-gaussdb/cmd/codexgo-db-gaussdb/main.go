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

const version = "0.9.8"

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
2. 开放式诊断("数据库有什么问题")一轮完成:调用 health,它确定性采集 6 维证据
   (体检 + 等待 + 慢SQL + 锁 + 死元组 + 索引,数字准确)并连同格式参考返回给你;
   你据此一轮写出一份完整诊断报告直接给用户 —— 保留准确数字/表格,补充带跨维
   因果链的根因分析与分优先级(P0/P1/P2)方案,不要再调用其它工具。
3. 单条 SQL 深度调优一轮完成:调用 sqltune,它确定性采集证据(执行计划 + [Pn] 代价
   热点 + 表/索引/统计 + 反模式 + 引擎索引建议 + 已校验机械改写候选)并连同格式参考返回;
   你据此一轮写出优化报告,每条改写/索引注明针对哪个 [Pn] 热点(已校验候选标【实测】,
   你新提的标【AI推断】)。
4. 专项监控工具(确定性渲染,直接展示给用户,你勿复述其表格,可一句话点评或串联下钻):
   · 会话/锁:sessions、locks(含阻塞树)、lwlocks、longtx
   · MVCC/空间:vacuum、xid、bloat、space、tempusage、hotkey
   · 内存/WAL/复制:gsmem、wal、replication、bgworker
   · 系统/元数据:resource、os、users、alert、params、tableinfo、indexadvise、sqlcount
   · 趋势:perfsnap(snap/list/compare/baseline,快照持久化)

GaussDB 使用专有驱动与 SCRAM-SHA256 认证,系统视图复用 openGauss(pg_stat_*、
dbe_perf.*)。当某项探针因权限或视图缺失而失败时,该项会标记为告警且不影响
其余检查 —— 请在结论中说明哪些项目未能采集。所有工具均为只读。`

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
