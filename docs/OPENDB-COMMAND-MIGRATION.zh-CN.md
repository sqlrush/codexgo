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

---

# 逐命令迁移评估(去向 / 采样优化 / UI 优化)

**评级**:🟢核心必迁 · 🔵推荐迁 · 🟡增强后迁 · 🔀合并入其他工具 · ⏭️后置(场景窄/依赖环境)· ⛔写操作(触只读边界,单独决策)· ✅已迁移。

**判据**:诊断价值 × 使用频度 × GaussDB 适用性 × MCP(一问一答)可行性 × 与现有工具重叠度。

## A. 会话 / 锁 / 阻塞 → 合并为 3 个工具

| 命令 | 去向 | 采样优化 | UI 优化 |
|---|---|---|---|
| `/sessions` | 🟢 独立(吸收 activesessions) | 补 idle 时长(now−state_change)、wait_event_type 分类、backend_type 过滤后台、client_addr、按 state 聚合计数;参数 `active`/`all` | ASCII 表 + 顶部 state 分布条(active/idle/idle-in-tx)+ 长 idle-in-tx ⚠️高亮 |
| `/activesessions` | 🔀 并入 `/sessions active` | 同上 | 同上 |
| `/locks` | 🟢 独立(吸收 blocktree) | granted/waiting 区分、锁模式、持有者↔等待者 pid+query、等待时长 | 平铺 ASCII 表 **+** 阻塞链 ASCII 树二合一 |
| `/blocktree` | 🔀 并入 `/locks` | 递归 CTE 多级链、环检测、根阻塞者标记、阻塞时长 | CJK-safe **ASCII 树**(非 box-drawing)+ 根因高亮 + "建议 kill 根 pid X" |
| `/lwlocks` | 🔵 独立 | 按 wait_status/event 聚合、Top 争用、持续时间;补 OG 特有 LWLock 名 | ASCII 表 + 等待分布条 |
| `/longtx` | 🟢 独立 | 补 backend_xmin(是否阻塞 vacuum)、是否持锁、时长分级阈值 | ASCII 表 + 时长分级(>5min⚠️ >30min🔴)+ "阻塞 vacuum"标记 |

## B. 事务 / MVCC / 膨胀 / 空间 → 合并为 5 个工具

| 命令 | 去向 | 采样优化 | UI 优化 |
|---|---|---|---|
| `/xid` | 🟢 独立(比 health 单项细) | per-db + Top 高龄表、距 autovacuum_freeze_max_age / wraparound 余量、需 freeze 表 | ASCII 表 + 回卷风险进度条(age/2³¹)+ 分级 |
| `/vacuum` | 🟢 独立(吸收 autovacuum) | 死元组比、last_(auto)vacuum、距触发阈值(threshold+scale×reltuples)、超阈未触发 | ASCII 表 + 死元组比条 + "已超阈未触发"🔴 |
| `/autovacuum` | 🔀 并入 `/vacuum` | 进行中 worker(pg_stat_progress_vacuum 若有)、被 longtx 阻塞源 | 进行中高亮 + 阻塞源关联 |
| `/bloat` | 🔵 独立(或并 vacuum) | 升级:经典膨胀估算 SQL(reltuples/relpages+列宽,免扫表);pgstattuple 可选精确 | ASCII 表 + 膨胀比条 + 回收建议(VACUUM FULL/重建) |
| `/space` | 🟢 独立(库级,吸收 segments/toast 钻取) | 库 size、表空间、磁盘剩余(配合 /os) | Panel + 大小条 |
| `/segments` | 🔀 并入 `/space`(表级钻取) | 表+索引+TOAST 分解、relkind | ASCII 表 + 表/索引/TOAST 占比条 |
| `/toasttable` | 🔀 并入 `/space`(大对象展开) | TOAST 占比、关联主表 | 仅大 TOAST 时展开列 |
| `/tempusage` | 🔵 独立 | pg_stat_database temp_files/bytes + 实时 spill 会话 + work_mem 设置 | ASCII 表 + spill 会话高亮 + work_mem 调整建议 |
| `/hotkey` | 🔵 独立 | seq/idx scan 比、HOT 比、ins/upd/del、关联表大小 | ASCII 表 + 活动评分条 + seq-heavy/update-heavy 标记 |

## C. 内存 → 合并为 1 个工具

| 命令 | 去向 | 采样优化 | UI 优化 |
|---|---|---|---|
| `/gsmem` | 🟢 独立(吸收 sessionmem) | 按 memorytype 分类、max_dynamic_memory vs used、命中率、各 context Top | Panel 分段(共享缓冲/动态内存/命中率)+ 使用率条 + 命中率分级 |
| `/sessionmem` | 🔀 并入 `/gsmem`(Top 会话) | Top 内存会话 + 关联 query/user | 总览下挂 Top 会话表 |

## D. WAL / 复制 / HA → 合并为 3 个工具

| 命令 | 去向 | 采样优化 | UI 优化 |
|---|---|---|---|
| `/wal` | 🔵 独立(吸收 walsummary/checkpoint/backup) | LSN、归档状态、生成速率、checkpoint timed/req 比、completion_target | Panel 分段 + 请求式 checkpoint 占比⚠️条 |
| `/walsummary` | 🔀 并入 `/wal` | OG5.0 无 archiver 的降级逻辑 | — |
| `/checkpoint` | 🔀 并入 `/wal` | 写放大、buffers_checkpoint 占比 | 请求式占比警告 |
| `/replication` | 🟢 独立(吸收 slots/logicalslots/pubsub) | 主端每备库 sent/write/flush/replay LSN + lag(字节/时间)、sync_state、断连告警 | Panel 每备库 + 延迟条 + 同步/异步分级 |
| `/slots` | 🔀 并入 `/replication` | active/inactive、restart_lsn、retained WAL | retained WAL 过大⚠️条 |
| `/logicalslots` | 🔀 并入 `/replication` | catalog_xmin 阻塞 vacuum、plugin、retained WAL | "阻塞 vacuum"标记 |
| `/pubsub` | 🟡 并入 `/replication`(可选段) | publication/subscription 计数 | 仅有逻辑复制时展开 |
| `/bgworker` | 🟡 独立或并 `/wal` | thread_wait_status 聚合、archiver 失败 | ASCII 表 + 失败告警行 |
| `/cmha` | ⏭️ 后置 | 依赖企业版 CM 视图,普通部署无 | — |

## E. SQL / 性能(多数已迁移)

| 命令 | 去向 | 采样优化 | UI 优化 |
|---|---|---|---|
| `/topsql` `/slowsql` `/explain` `/ash` `/planhistory` `/sqltune` `/sqlfetch` `/wdr` `/wdranalyze` | ✅ 已迁移 | 增强:topsql 补全 sort_key 维度;ash 补 wait_event 分类树 | 已用 codexgo 范式;ash 可加等待分布条 |
| `/perfsnap` `/psnap` | 🔵 独立(需持久化) | 多源 delta、ring buffer 持久化(snap/compare/list/baseline) | before/after/**delta 对比表** + 变化高亮(↑↓) |
| `/sqlcount` | 🟡 独立 | gs_sql_count 按 user/类型、avg/max 延迟 | ASCII 表 + DML/DDL/DCL 占比条 |
| `/sql` | ⛔/🟡 只读版 | 仅放行 SELECT/EXPLAIN(只读边界);DML 不做 | 表格;非只读语句拒绝 |
| `/dbtop` | ⏭️ 降级 | 改"**单次快照仪表盘**"(放弃持续刷新,MCP 不适交互刷新) | 单帧 Panel 多指标 |

## F. 体检 / 系统 / 资源 / 元数据

| 命令 | 去向 | 采样优化 | UI 优化 |
|---|---|---|---|
| `/health` `/indexhealth` | ✅ 已迁移 | — | — |
| `/resource` | 🔵 独立 | 各 max_*(连接/walsender/worker/prepared_xact)vs 当前使用率 | Panel + 4 组使用率条 |
| `/os` | 🔵 独立(依赖视图) | pv_os_run_info(远程 DB 主机的 load/mem/cpu);插件本机 /proc≠DB 主机,须走视图 | Panel + 进度条;视图不可用时友好降级 |
| `/users` | 🔵 独立 | pg_roles/pg_authid、rolvaliduntil、超权、登录权限、密码过期;权限不足 fallback pg_roles | ASCII 表 + 过期/超权🔴高亮 |
| `/alert` | 🔵 独立或并 health | deadlocks/conflicts/temp_files、近期增量 | ASCII 表 + severity 标 |
| `/respool` | ⏭️ 后置 | WLM 资源池,使用面窄 | — |
| `/mot` | ⏭️ 后置 | MOT 需编译开启,极少用 | — |
| `/ogerr` | 🟡 独立(低成本) | 纯本地 KB,无需 DB;模型本身懂错误码 → 做成模型可查参考即可 | 面板(成因/诊断命令/修复) |

## G. 管理 / 变更 ⛔写(单独决策)

| 命令 | 去向 | 采样优化 | UI 优化 |
|---|---|---|---|
| `/params` | 🔵 独立(只读) | pg_settings 全量 + 分类 + context(改后是否需重启) | ASCII 表 + 按类分组 + 可改性标记 |
| `/backup` | 🔀 并入 `/wal`(只读) | archiver + 配置 | Panel 段 |
| `/kill` | ⛔ 决策 | 只读边界:纳入需确认+审计+权限 | 确认面板 + 结果 |
| `/alter` | ⛔ 决策(高危) | 改参数,后置 | 确认 + reload 校验 |
| `/gather` | ⛔ 决策 | check(只读)可迁;run(写)受限 | check→run 两阶段 |
| `/jobs` | ⏭️ 后置 | 依赖 pg_cron 扩展 | ASCII 表 |

## H. Schema

| 命令 | 去向 | 采样优化 | UI 优化 |
|---|---|---|---|
| `/tableinfo` | 🟢 独立 | 列/索引/约束/统计/大小/分区/最近 analyze | 多 section ASCII 表 |
| `/indexadvise` | 🔵 独立(注意重叠) | gs_index_advise + EXPLAIN 分析;与 sqltune/indexhealth 重叠,定位"单 SQL 索引建议" | 建议列表 + 与现有索引去重提示 |

## 关键结论:56 命令 → 约 24 个聚合工具

合并是这次最大的"功能优化"—— opendb 命令细碎,codexgo 聚合成更少、更强的诊断工具:

- **A 会话锁**:6 → 3(`sessions`含active / `locks`含blocktree / `lwlocks` / `longtx`)
- **B 空间MVCC**:9 → 6(`xid` / `vacuum`含autovacuum / `bloat` / `space`含segments+toast / `tempusage` / `hotkey`)
- **C 内存**:2 → 1(`gsmem`含sessionmem)
- **D WAL复制**:9 → 3(`wal`含walsummary+checkpoint+backup / `replication`含slots+logicalslots+pubsub / `bgworker`)
- **E 性能**:已迁 9 + 新 `perfsnap` / `sqlcount` / `dbtop`(单帧)
- **F 系统**:`resource` / `os` / `users` / `alert`
- **G/H**:`params` / `tableinfo` / `indexadvise`;写操作单独决策

## 建议分批

- **本版(纯只读,不破边界)**:A+B+C+D+F + perfsnap/sqlcount + tableinfo/indexadvise/params ≈ **新增 ~20 个聚合工具**。
- **后置**:cmha / respool / mot / jobs / ogerr。
- **单独议**:kill / alter / gather(写操作)、dbtop(交互刷新)、sql(任意执行)。
