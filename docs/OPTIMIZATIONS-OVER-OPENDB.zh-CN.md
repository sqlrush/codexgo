# codexgo-db-gaussdb 相对 opendb 的优化点

> 本文记录 GaussDB 数据库插件(`plugins/codexgo-db-gaussdb`)在实现首批 + 第二批
> SQL 调优/健康检查命令时,**在 opendb 源码基础上做的优化**。
>
> 原则:不照抄 opendb,而是借助 codexgo 框架能力做"更好的交互、更美观的 UI、
> 更完善的数据、更精确的评估"。SQL 语句保持忠实于 opendb 的 openGauss 实现
> (GaussDB 复用 openGauss 视图),优化集中在**数据结构、评估模型、健壮性、
> 推理解耦与安全**五个层面。

参考设计文档:[PLUGIN-DB-DESIGN.zh-CN.md](./PLUGIN-DB-DESIGN.zh-CN.md)

---

## 覆盖的命令(批次1 + 批次2,共 11 个 + 连接)

| 工具 | 对应 /命令 | 批次 | 数据来源 |
|------|-----------|------|----------|
| `connect` | （连接) | — | gaussdb-go 驱动 |
| `health` | /health | 1 | pg_stat_* / pg_settings |
| `slowsql` | /slowsql | 1 | dbe_perf.statement |
| `topsql` | /topsql | 1 | dbe_perf.statement |
| `explain` | /explain | 1 | EXPLAIN |
| `ash` | /ash | 1 | pg_stat_activity |
| `indexhealth` | /indexhealth | 1 | pg_stat_user_indexes / pg_index |
| `sqlfetch` | /sqlfetch | 1 | dbe_perf.statement_history / statement |
| `sqltune` | /sqltune | 2 | EXPLAIN + gs_index_advise |
| `planhistory` | /planhistory | 2 | dbe_perf.statement_history |
| `wdr` | /wdr | 2 | snapshot.snapshot |
| `wdranalyze` | /wdranalyze | 2 | generate_wdr_report |
| `help` | /help | — | 静态命令目录(与 opendb 一致,无需连接) |

> 命令名与 opendb 保持一致(`/health` 而非 `/db_health`)。

---

## 优化点一:结构化输出 vs 服务端预渲染文本

**opendb 做法**:技能在服务端把结果渲染成固定的文本表格/字符串返回。UI 样式被
锁死在数据层,LLM 拿到的是已排版的文本,难以二次推理。

**codexgo 做法**:每个工具返回**结构化 JSON**(`columns/rows` 或 `score/items`),
由 codexgo 前端负责渲染表格/面板。带来三个收益:

1. **更美观的 UI**:渲染权交回 codexgo,可用统一的主题、表格组件、健康分仪表。
2. **更强的可推理性**:LLM 拿到的是真实数值(avg_ms=320.5、cache_hit=98.7%),
   而非排版后的字符串,诊断更精确。
3. **自带解释上下文**:每个结果封装为 `TableReport`,除 `columns/rows` 外还携带:
   - `title`:面板标题
   - `note`:**该指标如何解读**(例如"On CPU 占比高=算力瓶颈")
   - `params`:**本次实际生效的阈值/排序/限制**(透明可复现)

> 代码:`internal/tools/result.go` 的 `TableReport`,`server.go` 的 `instructions`
> 明确要求模型"不要把 JSON 原样贴给用户,要基于 score/items 给自然语言结论"。

---

## 优化点二:三级评估 + 加权健康分(更精确的评估)

**opendb 做法**:健康项多为 OK/WARN 两态,无统一量化总分。

**codexgo 做法**(`internal/tools/health.go`):

- **四级状态**:`OK / WARN / FAIL / UNKNOWN`,比 opendb 多出 FAIL(严重)与
  UNKNOWN(采集失败但不影响其余项)。
- **加权健康分(0–100)**:`score = 100 - WARN×6 - FAIL×18`(下限 0),把分散的
  检查项压缩成一个可对比、可排序、可在多库巡检中聚合的总分。
- **分级**:优(≥95)/ 良(80–94)/ 警告(60–79)/ 危险(<60 或存在 FAIL)。
- **每项携带阈值与建议**:`{value, threshold, status, suggestion}`,例如缓存命中
  率 `WARN<99% / FAIL<95%`,直接告诉用户"差多少、怎么办"。

健康检查项也比 opendb 更完整:实例运行时长、连接使用率、活动会话、事务中空闲
(idle-in-tx)、缓存命中率、死元组、**事务ID回卷风险(xid wraparound)**、备库
连接数——其中回卷风险与 idle-in-tx 是高危但常被忽略的项。

---

## 优化点三:逐项容错(更健壮)

**opendb 做法**:体检/索引审计中,单条探针 SQL 出错(权限不足、视图缺失)易导致
整份报告失败或缺章。

**codexgo 做法**:把每个探针/分区做成**独立、互不影响**的单元:

- `health`:任一探针失败仅记为该项 `UNKNOWN`,其余照常产出,并在结论里说明
  哪些项未采集。
- `indexhealth`:未使用 / 失效 / 重复 / 大索引四个分区各自独立查询,某分区失败
  只在 `notes` 里记一笔,不影响其它三个分区。
- `ash`:核心"等待分布"必出;"活动会话明细"作为增强项,失败时降级(可能因
  老版本缺 `wait_status` 列),不让整次调用失败。

---

## 优化点四:数据增强(更完善的数据)

在忠实 opendb SQL 的基础上,补充了对诊断有直接价值的列/维度:

| 工具 | opendb | codexgo 增强 |
|------|--------|--------------|
| `slowsql` | avg/total/calls/rows | **+ max_ms(抖动)+ 每条 SQL 的 cache_hit_pct(是否走磁盘)** |
| `ash` | 仅等待分布 | **+ 活动会话明细(pid/user/run_sec/SQL头),可直接点名问题会话** |
| `health` | 基础项 | **+ xid 回卷风险 + idle-in-tx + 备库连接** |
| `planhistory` | 计划+耗时 | 同时给 db_ms/exec_ms/cpu_ms/hard_parse,**便于判定是否计划回退** |

---

## 优化点五:数据采集与 LLM 推理解耦(模型无关,核心架构优化)

**opendb 做法**:`/sqltune`、`/wdranalyze` 把"采集 → LLM 分析 → 校验"整条流水线
**内嵌在技能里**,绑定单一 LLM 与固定提示词(quick/deep 模式),换模型成本高。

**codexgo 做法**:把一项能力拆成**两个入口**:

- **数据层(本插件,确定性)**:`sqltune` 只做确定性采集——解析 SQL、跑 EXPLAIN、
  标注计划问题(`plan_issues`)、调用引擎自带的 `gs_index_advise`,并附上一份
  **五维调优清单**(改写/索引/hint/表结构与统计/计划稳定性)。它**不调用任何 LLM**。
- **推理层(codexgo 主体,模型无关)**:由 codexgo 当前所用模型(GLM / DeepSeek /
  任意后端)基于这些素材产出最终优化建议。

`wdranalyze` 同理:插件确定性地生成 WDR 报告原文并结构化返回,codexgo 的模型
负责"工作负载画像 + 风险分级 + Top SQL",并可链式对 Top SQL 调 `sqltune` 下钻。

收益:**任意后端可用**(契合 codexgo 多模型路由)、提示词与编排可在 codexgo 侧统一
演进、单 DB 插件保持轻量纯净。

---

## 优化点六:领域知识走 MCP 协议原生通道

**opendb 做法**:按 DBType 维护 SKILL 文件承载领域知识。

**codexgo 做法**:领域知识作为 MCP `initialize.instructions` 与每个工具的
`description` 下发(见 `cmd/codexgo-db-gaussdb/main.go` 的 `instructions`)。这是
**MCP 协议原生**做法,任何 MCP 宿主都能消费,不依赖 codexgo 私有的 SKILL 机制,
可移植性更好。

---

## 优化点七:安全与只读保证

- **只读强约束**:`explain` / `sqltune` 通过 `isReadOnlySQL` 拒绝
  INSERT/UPDATE/DELETE/DDL/CALL/VACUUM 等(`EXPLAIN ANALYZE` 会真正执行语句,
  写语句会改数据,故必须拦)。
- **防注入**:`topsql` 的排序维度走**白名单**映射到 ORDER BY(ORDER BY 无法参数化);
  `gs_index_advise` 的 SQL 字面量做了单引号转义;数值型参数(threshold/limit)强类型化。
- **EXPLAIN 就绪检测**:`countPlaceholders` 检测归一化 SQL 中的 `?`/`$N`/`:N` 占位符
  (忽略字符串字面量内的),避免对带占位符的语句盲目 EXPLAIN。
- **连接校验**:`connect` 校验 host/port(1–65535)/user;DSN 使用 keyword/value
  形式(密码含 `@`/`/` 也安全),`default_query_exec_mode=simple_protocol` 规避
  GaussDB 在系统视图 xid 类型上的 codec 不匹配。

---

## 优化点八:人 / LLM 双入口

同一份能力,两种触发方式(见设计文档):

- **人类用 /命令**:`/slowsql 500` → 经 slash → `tools/call` 确定性直达,参数稳定。
- **LLM 用工具描述**:模型读 `description` 自主编排(先 `health`,再对暴露的热点
  `slowsql` → `sqltune`)。

---

## 优化点九:独立模块 + 单 DB 单 MCP(工程与商业化)

- 插件是**独立 Go module**(`github.com/sqlrush/codexgo-db-gaussdb`),codexgo 核心
  **不 import** 它,二者零运行时耦合;通过 stdio JSON-RPC 通信。
- **一库一 MCP**:适配"不同库迭代节奏差异大",也便于未来**只向客户售卖某一种
  数据库的 MCP**。GaussDB 用其**专有驱动** `HuaweiCloudDeveloper/gaussdb-go`
  (SCRAM-SHA256,与 pgx 不兼容),而非 pg 驱动。

---

## 质量保障

- `go build ./...` / `go vet ./...` 通过。
- 纯逻辑(评分、占位符识别、只读判定、计划问题识别、DSN 构建)有**表驱动单测**,
  `go test -race` 通过。
- 实现期间单测已捕获一个真实缺陷:EXPLAIN TEXT 子节点带 `->` 缩进前缀,最初的
  `HasPrefix("seq scan on")` 漏判全表扫描——已修正为先归一化节点标签再匹配。

> **待真机验证**:沙箱无真实 GaussDB,以上 SQL 需在用户环境端到端验证;部分增强列
> (如 `wait_status`、`max_elapse_time`)在个别版本可能不存在,届时按报错精修。
