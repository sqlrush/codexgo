# opendb → codexgo GaussDB `/` 命令迁移评估

> 下一版大功能的基线清单:把 opendb 上**值得迁移的全部 GaussDB `/` 命令**迁移到
> codexgo 的 gaussdb MCP 插件,且不止迁移 —— 同时做**功能与 UI 的全面优化**。
> 本版**只做 GaussDB**。

**数据来源**
- opendb 命令注册:`internal/gaussdb/register.go`(GaussDB 复用 openGauss skill 实现,
  系统视图相同,仅 wire 驱动因 SCRAM-SHA256(10) 不同而独立)。
- 命令实现/采样 SQL/UI:`internal/opengauss/skill/{monitor,query,admin,schema}/*.go`。
- slash 调度:`internal/ui/skill_runner.go`。
- codexgo 现状:`plugins/codexgo-db-gaussdb/internal/tools/*.go`(已注册 14 个工具)。

**范围约定**
- Oracle 专属命令(`/sga /pga /redo /fra /asm /latches /mutexes /awr /standby /resize
  /scheduler /undosess /sortusage` 等)GaussDB 不注册,**已排除**。
- AI 编排层(`/llm /diagnose /sentinel /rule`)不是数据命令,是模型编排,**另议**(见末尾)。

**codexgo 现状(已迁移 11 个数据命令 + connect/help)**
`health · slowsql · topsql · explain · ash · planhistory · sqltune(+verify) · sqlfetch ·
indexhealth · wdr · wdranalyze` + `connect · help`。其中 `sqltune`(两趟自校验)、`health`
(6 维单轮诊断)、`explain`(结构化计划树)在功能上**已优于 opendb**。

图例:✅ 已迁移 · 🟡 部分覆盖 · ❌ 未迁移。采样指标列格式 = `SQL条数 · 关键视图 · ~指标/字段数`。

---

## A. 会话 / 锁 / 阻塞(6)

| 命令 | 作用 | 采样指标 | opendb UI | codexgo |
|---|---|---|---|---|
| `/sessions` | 全部连接概览(状态/等待/当前SQL) | 1 SQL · pg_stat_activity · ~9 | lipgloss 表格 | ❌ |
| `/activesessions` | 仅活跃会话(state=active) | 1 SQL · pg_stat_activity · ~7 | Panel+定宽表(20行) | ❌ |
| `/locks` | 行/表/Advisory 锁等待 | 1 SQL · pg_locks+activity · ~8 | 表格(自连接) | ❌ |
| `/blocktree` | 锁阻塞链**树形**+环检测 | 1 SQL · pg_locks+activity · ~8 | 🔒 树形 box-drawing | ❌ |
| `/lwlocks` | LWLock 轻量锁争用 | 1 SQL · pg_thread_wait_status · ~5 | 表格(按 waiters 降序) | ❌ |
| `/longtx` | 长事务列表 | 1 SQL · pg_stat_activity · ~6 | 表格(`[limit]`) | ❌ |

## B. 事务 / MVCC / 膨胀 / 空间(9)

| 命令 | 作用 | 采样指标 | opendb UI | codexgo |
|---|---|---|---|---|
| `/xid` | 事务ID年龄/回卷风险 | 1 SQL · pg_database · ~3 | 表格 | 🟡 (health 含一项) |
| `/vacuum` | Vacuum 状态/死元组 | 1 SQL · pg_stat_user_tables · ~8 | 表格(LIMIT30) | 🟡 (诊断含死元组) |
| `/autovacuum` | autovacuum 进度/活动 | 2 SQL · activity+all_tables · ~8 | 表格(worker优先) | ❌ |
| `/bloat` | 表膨胀估算(死元组比) | 1 SQL · pg_stat_user_tables · ~8 | 表格(`[阈值][limit]`) | ❌ |
| `/segments` | 表+索引大小排行 | 1 SQL · user_tables+relation_size · ~5 | 表格(`[limit]`) | ❌ |
| `/tempusage` | 临时文件使用(库/会话) | 2 SQL · stat_database+activity · ~6 | 表格(实时spill优先) | ❌ |
| `/hotkey` | 热点表 Top20(读写评分) | 1 SQL · pg_stat_all_tables · ~9 | 表格+heavy标记 | ❌ |
| `/toasttable` | TOAST 大字段存储 Top20 | 1 SQL · pg_class+namespace · ~5 | 表格 | ❌ |
| `/space` | 库级空间使用 | 1 SQL · pg_database · 2 | Panel+柱状条 | ❌ |

## C. 内存(2)

| 命令 | 作用 | 采样指标 | opendb UI | codexgo |
|---|---|---|---|---|
| `/gsmem` | 引擎总量+共享缓冲+命中率 | 3 SQL · gs_total_memory_detail+stat_database+settings · ~15 | 多面板 key:value | ❌ |
| `/sessionmem` | 会话内存 Top20 | 1 SQL · gs_session_memory_detail · ~4 | 表格 | ❌ |

## D. WAL / 复制 / 高可用(9)

| 命令 | 作用 | 采样指标 | opendb UI | codexgo |
|---|---|---|---|---|
| `/wal` | WAL LSN+归档+参数 | 4 SQL · xlog+archiver+bgwriter+settings · ~20 | 多面板分段 | ❌ |
| `/walsummary` | WAL 归档细节/生成速率 | 2 SQL · settings+xlogfile_name · ~5 | 文本行 | ❌ |
| `/checkpoint` | Checkpoint 频率/写放大 | 2 SQL · bgwriter+settings · ~14 | 表格+诊断提示 | ❌ |
| `/bgworker` | 后台进程状态 | 2 SQL · thread_wait_status+archiver · ~10 | 表格+告警行 | ❌ |
| `/replication` | 流复制(主/备自适应) | 2 SQL · stat_replication+wal_receiver · ~15 | 多面板(每备库LSN) | ❌ |
| `/slots` | 复制槽状态 | 1 SQL · pg_replication_slots · ~4 | 表格 | ❌ |
| `/logicalslots` | 逻辑复制槽(retained WAL) | 1 SQL · pg_replication_slots · ~7 | 表格 | ❌ |
| `/pubsub` | 发布/订阅(逻辑复制) | 2 SQL · publication+subscription · ~8 | 表格 | ❌ |
| `/cmha` | CM 集群/双机热备 | 2 SQL · stream_replications+replication · ~12 | 表格+本地角色 | ❌ |

## E. SQL / 性能(13)

| 命令 | 作用 | 采样指标 | opendb UI | codexgo |
|---|---|---|---|---|
| `/topsql` | Top SQL(多排序) | 1 SQL · dbe_perf.statement · ~6 | 单行化表格 | ✅ |
| `/slowsql` | 慢SQL(按阈值) | 1 SQL · dbe_perf.statement · ~6 | 单行化表格 | ✅ |
| `/sql` | 执行任意 SQL | N/A(用户输入) | 表格/OK | ❌(可由通用执行替代) |
| `/explain` | 执行计划+风险检测 | 1 SQL(EXPLAIN) · ~3-5 warn | 计划树+黄标 | ✅(已增结构化计划树) |
| `/ash` | 活跃会话按等待分组 | 1 SQL · pg_stat_activity · ~4 | 表格+进度条 | ✅ |
| `/planhistory` | 计划演化/回退检测 | 1 SQL · statement_history · ~9 | 表格(含计划树) | ✅ |
| `/sqltune` | **LLM** 五维SQL优化 | 多 SQL(候选+验证) · ~20+ | markdown报告+候选树 | ✅(两趟+自校验) |
| `/sqlfetch` | 按SQL_ID取可EXPLAIN全文 | 2 SQL · statement_history/statement · ~5 | 面板(占位符回填) | ✅ |
| `/wdr` | WDR 快照列表/生成指引 | 1 SQL · snapshot.snapshot · ~4 | 表格+指引 | ✅ |
| `/wdranalyze` | **LLM** 7层WDR分析 | 多 SQL(全量WDR+TopSQL) · ~50+ | markdown 3层报告 | ✅ |
| `/perfsnap` `/psnap` | 性能快照捕获/对比 | 3 SQL · stat_database+bgwriter+statements · ~20+ | markdown+表格(snap/compare/list/baseline) | ❌ |
| `/dbtop` | 实时性能仪表盘(类top) | 多 SQL · ~20+ | **交互式TUI刷新** | ❌(MCP下需特殊设计) |
| `/sqlcount` | SQL类型统计(DML/DDL/DCL) | 1 SQL · gs_sql_count · ~12 | 表格 | ❌ |

## F. 体检 / 系统 / 资源 / 元数据(8)

| 命令 | 作用 | 采样指标 | opendb UI | codexgo |
|---|---|---|---|---|
| `/health` | 健康总览(多检查+评分) | 7 SQL · settings+activity+database+bgwriter+extension · ~17项 | Panel+状态图标+进度条 | ✅(6维单轮诊断) |
| `/indexhealth` | 索引健康(未用/无效/重复/膨胀) | 4 SQL · stat_user_indexes+pg_index · ~20/类 | 4-section 面板 | ✅ |
| `/os` | 宿主机 load/mem/cpu | 1-2 SQL · pv_os_run_info/proc · ~8 | Panel+进度条 | ❌ |
| `/resource` | 连接/WALsender/checkpoint/worker 限额 | 4 SQL · settings+activity+replication+bgwriter · ~15 | Panel+进度条(4组) | ❌ |
| `/respool` | 资源池(WLM) | 1 SQL · pg_resource_pool · ~7 | 表格 | ❌ |
| `/users` | 用户/角色(权限/过期) | 1 SQL · pg_authid · ~7 | 表格 | ❌ |
| `/mot` | MOT 内存表引擎 | 2 SQL · mot_mem_cfg+session_memory · ~10 | 表格 | ❌ |
| `/ogerr` | SQLSTATE 错误知识库 | N/A(本地KB30+) · ~4字段 | 面板(诊断/修复分段) | ❌ |

## G. 管理 / 变更 ⚠️写(7)

| 命令 | 作用 | 采样指标 | opendb UI | 读写 | codexgo |
|---|---|---|---|---|---|
| `/kill` | 终止会话/取消查询 | 1 SQL · pg_stat_activity · ~9 | 确认面板+结果 | **写** | ❌ |
| `/alter` | ALTER SYSTEM 改参数 | 1 SQL · pg_settings · ~5 | 确认+reload校验 | **写** | ❌ |
| `/params` | 搜索参数 | 1 SQL · pg_settings · ~50/750 | 表格 | 只读 | ❌ |
| `/alert` | 死锁/冲突/临时文件 | 1 SQL · pg_stat_database · ~8 | 面板+severity标 | 只读 | ❌ |
| `/backup` | WAL归档/备份状态 | 2 SQL · stat_archiver+setting · ~7 | 多section面板 | 只读 | ❌ |
| `/jobs` | 定时任务(pg_cron) | 1 SQL · cron.job · ~7 | 表格 | 只读 | ❌ |
| `/gather` | ANALYZE 收集统计 | 1-2 SQL · stat_user_tables · ~5 | check→run 两阶段 | **写** | ❌ |

## H. Schema(2)

| 命令 | 作用 | 采样指标 | opendb UI | codexgo |
|---|---|---|---|---|
| `/tableinfo` | 表结构(列/索引/约束/统计) | 4 SQL · attribute+index+constraint+stat · ~12 | 4-section 面板 | ❌ |
| `/indexadvise` | 索引推荐(EXPLAIN分析) | 2 SQL(EXPLAIN JSON) · ~5建议 | 多section面板 | ❌ |

---

## 汇总

- opendb GaussDB 数据/管理命令 **≈ 56 个**;codexgo **已迁移 11 个**(+ connect/help)→
  **待迁移 ≈ 43 个**。
- 已迁移的几个**功能上已优于 opendb**(sqltune 两趟自校验、health 6 维单轮、explain 结构化计划树)。

## 待拍板的范围问题(影响怎么迁、迁多少)

1. **只读边界**:codexgo 插件目前**全只读**。`/kill /alter /gather` 会改库状态。是否纳入?
   纳入需设计确认/审计/权限,且破"纯只读"定位 —— 建议本版**先不做写操作**,或单独分批。
2. **AI 编排层**:opendb 的 `/llm /diagnose /sentinel /rule` 是**模型编排**,不是数据命令。
   codexgo 的对应物是它自己的 model loop + 单轮诊断范式(见
   [OPENDB-DIAGNOSIS-REFERENCE.zh-CN.md](./OPENDB-DIAGNOSIS-REFERENCE.zh-CN.md))。**另开话题**,不计入命令迁移。
3. **`/dbtop` 交互式刷新**:类 top 持续刷新终端,在 MCP(一次请求一次响应)模型下不天然 ——
   要么做成"单次快照",要么放弃。

## UI 优化通用方向(迁移时统一执行)

opendb 大量用 lipgloss 服务端渲染,box-drawing 字符在 CJK locale(zh_CN)下是
East-Asian-Ambiguous 宽度 = 2 cell,会与内容错位(已踩过坑)。codexgo 侧统一走:
- **ASCII 对齐表格**(`+ - |` 定宽 1 cell)+ markdown 表格渲染(codexgo TUI 已支持);
- 树形(blocktree)用 **ASCII 树**,避免歧义宽度;
- 进度条/柱状用 `█░`(同一行内同宽,不与边框交错);
- 数字采集确定性(插件)/ 叙述分析交模型,沿用单轮诊断范式。
