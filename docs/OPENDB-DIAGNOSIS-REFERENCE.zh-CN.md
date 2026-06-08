# opendb 诊断行为参考(供 codexgo DB 插件对照)

> 本文记录从 opendb 源码(`/Users/sqlrush/opendb`)观察到的诊断/调优/体检相关行为,
> 作为 codexgo `codexgo-db-gaussdb` 插件设计与"做得比 opendb 更好"的对照基线。
> 来源:对 opendb 的三轮源码深扫(sqltune 渲染器、LLM 健康诊断、各诊断命令)。
>
> 关键结论先行:**opendb 让 GLM 多维诊断,靠的不是模型,而是"编排"——
> 强制多维工具调用 + 诊断系统提示词。** codexgo 用同样的 GLM 但缺这套编排,
> 所以模型问"有什么问题"只会查单维。我们的对策是**插件侧确定性多维采集**
> (不赌模型自觉),详见 §8。

---

## 0. 总体架构:三条 LLM 诊断路径 + 确定性体检

| 路径 | 入口 | 是否走 LLM | 输出 |
|---|---|---|---|
| `/health` 体检 | `skill/monitor/health.go` | ❌ 纯确定性,阈值判定 OK/WARN | box panel,"X OK, Y WARN",无综合评分 |
| **A. on-demand `/llm current`**(当前数据库有什么问题) | `skill/ai/diag_skill.go` → 引擎 agentic loop | ✅ 强制多维证据 + LLM 综合 | 根因分析 / 紧急措施 / 根因修复 |
| **B. Sentinel 突发异常** `/llm N` | sentinel + ruleengine | ✅ 规则引擎 + LLM | 结构化诊断(severity/confidence/evidence/actions) |
| **C. WDR 分析** `/wdranalyze` | `skill/query/wdranalyze_skill.go` + `wdranalyze/synthesizer.go` | ✅(单次 synthesis)| 3 层契约 markdown |

**最重要的设计取向**:per-skill 的 SQL 视图多为**定位/快照视图,内置判断很薄**;真正的
数值判断集中在三处:`healthcheck/healthcheck.go`(静态阈值)、`sentinel/classify*.go`
(24 条根因规则)、`monitor/dbtop/health.go`(实时 TUI 两级阈值)。而 LLM 诊断路径**本身
不套用这些数值规则**,靠的是**系统提示词里的判断启发式 + 等待事件知识表**。

---

## 1. SQL 调优报告(sqltune)

### 1.1 渲染入口
`internal/opengauss/sqltuner/tuner.go` `renderFallbackReport()` —— 确定性模板渲染。
两种触发:**QuickMode**(跳过 Round-2 LLM 润色)或 **Round-2 LLM 失败**。截图里
"Quick mode 已跳过 Round 2 LLM 润色,本报告由确定性模板渲染" 即此路径。

### 1.2 报告结构(7 段)
```
# SQL Tuning Report
> {reason}
## 1. 输入 SQL                       ```sql ... ```
## 2. 执行计划                       Total cost + 计划树([P1..Pn] 标注)+ 标注说明(对应方案 N)
## 3. 关键证据  ### 代价热点(markdown 表,cap 12)  ### 表/索引/统计信息
## 4. CBO 分析                       round1.CBOAnalysis(LLM 产出,非模板)
## 5. 优化方案                       已采纳候选(操作/依据/SQL/验证/风险/前置/回滚)
## 6. 待验证策略 / ## 7. 模型候选被拒绝(调试)
## 不确定的点                        round1.UncertaintyNotes(LLM 产出)
```

### 1.3 [Pn] 编号(关键)
两遍:① 先给 hotspots(按评分序)分配 [P1..];② 再给 candidate 关联的关系节点
(DFS 树序)分配后续编号。**不是纯按 cost、也不是纯树序。**

热点评分(`buildPlanHotspots`,top-N=**12**):base = `TotalCost`,乘数累计:
- seq scan 且 `PlanRows>10000` → ×1.3,reason 改"大行数顺序扫描"
- 排序落盘(`SortSpaceType != Memory`)→ ×1.2
- 估算/实际行偏差 `>10×` 或 `<0.1×` → ×1.5(最后命中的 reason 覆盖前者)

> ⚠️ codexgo 改进点:opendb 按**累积 TotalCost** 排序,会把树顶算子(Sort/Join,
> 自身成本极小)排在真瓶颈(叶子 Seq Scan)前面。codexgo 改用 **self-cost
> = TotalCost − Σ子节点**,真瓶颈排第一(见 plugin render report)。

### 1.4 算子→诊断说明映射(`planMarkerReason`)
```
seq scan    → 高成本顺序扫描,优先检查过滤列/连接列索引与统计信息
bitmap heap → Bitmap Heap 访问成本高,检查索引覆盖度与过滤选择性
sort        → 排序节点成本高,检查排序键索引、LIMIT 下推或 work_mem
hash join   → Hash Join 成本高,检查构建端行数、连接列索引和统计信息
nested loop → Nested Loop 成本高,检查内层是否可用连接列索引
```
注解门槛 `isAccessPathWorthTuning`:seq scan `cost≥100 || rows≥1000`;bitmap heap `cost≥1000`。

### 1.5 候选验证 + 拒绝阈值
- cost 前后对比 + 等价性(行 hash 抽样,LIMIT 1000)。
- **拒绝阈值:`Speedup < 1.3`(cost 改善不足 30%)**;等价不通过;含省略号/模板占位符/不完整片段。
- 每个修复三件套:**操作 / ⚠️风险评估 / 📋前置检查 / 🔄回滚方案**。
- 采纳排序:可验证优先 → Speedup 降序 → 类型(index10/stats20/rewrite30/param40/hint50/schema60)→ 风险(low1/med2/high3)→ ID。

### 1.6 LLM 部分(非模板)
- `Round1Output` JSON:`confidence / cbo_analysis / candidates[] / explored_dimensions / uncertainty_notes`。
- Round1 prompt 做 5 维分析(改写/索引/hint/schema/stats),要求真实 EXPLAIN 验证 + 等价检查。
- "sargable"等措辞**只在 LLM prompt 里**,确定性 fallback 只有 terse 摘要。

---

## 2. 开放式健康诊断 `/llm current`(当前数据库有什么问题)

### 2.1 强制证据工具集(核心)
`agent/diagnose.go` `currentDBEvidenceToolCalls()`:
```go
[]string{"health", "activesessions", "waits", "topsql", "slowsql", "blocktree"}
// topsql → {"args":"el"}; slowsql → {"args":"1000"}; 其余 {}
```
经 `RequireToolEvidence` 强制:模型若想零工具直接回答会被拒,注入 system-reminder
*"当前问题必须先调用只读数据库工具采集证据…请立即调用 health、activesessions、
waits、topsql、slowsql 或 blocktree"*;最终失败:*"⚠️ 未完成数据库采集,不能给出诊断结论"*。

> 注:当前 build `DeterministicRoutingEnabled=false`,强制路径"意图"存在但默认关,
> 主要靠 `RequireToolEvidence` 兜底逼工具。触发关键词(`IsFullDiagnosisIntent`):
> 存在什么问题/有哪些问题/有没有问题/问题/异常/慢/诊断/排查/性能/故障/风险/瓶颈/
> 卡/响应慢/阻塞/等待/死锁/健康/cpu高/内存高/io高。

### 2.2 诊断系统提示词(判断引导,judgment in prompt)
`engine/context/builder.go`(strict/templated 两版)+ `profile/opengauss.go` 知识块。关键句:
- **多维**:"先观察后判断 —— 至少 2-3 个维度的数据再给结论,避免单一指标误判"
- **充分性**:"调查充分才收敛 —— 根因 + 完整证据链 + 可直接执行的修复 SQL 都到位…
  只识别出'可能根因'或'建议进一步调查'都不算充分"
- **完成标准 6 项自检**:提 sql_id→须取 SQL 文本≥200 字符;提对象→须 `sql` 验存在;
  给修复 SQL→须可执行非骨架;提 plan_hash→须 `explain`;提锁→须 `blocktree` 全链;
  提等待占比→须具体百分比非"高/低"。"任一缺失却写进结论 → 停下先调工具"
- **6 步推理**:观察现象 → 建假设(≤2-3) → 收证据(每轮1-2工具) → 排除/确认 → 深入验证
  (waits→SQL→执行计划) → 给结论
- **工具路由**:单 SQL→`sqltune`;集群级→`health→alert→activesessions→waits→blocktree`;WDR→`wdranalyze`
- **反 deferral(禁用措辞)**:"本次诊断仅基于 X"/"如需更精准"/"建议补充调用 X"/"需要补充 SQL 文本"
- **背景噪音归因**:"持续写入负载的 WAL 压力和 IO 等待是背景噪音,不作首要根因";"因果链不超过 3 层"

### 2.3 输出结构
```
## 根因分析   证据表(≤4列:指标/数据/来源工具)+ 因果链 + SQL 文本前200字符
## 紧急措施   可立即执行止血 SQL
## 根因修复   每方案:操作 / ⚠️风险评估 / 📋前置检查 / 🔄回滚方案
```
(sqltune/wdranalyze 的 markdown 可原样透传)

### 2.4 迭代 / 收敛
- `DefaultMaxRounds=20`(auto/assist 模式);playbook=1 轮(无工具)。
- 收敛信号:**某轮无 tool_calls = 最终答案**。
- 大/中模型 `AutonomousStrategy`(auto),耗尽自动 fallback 到 `GuidedStrategy`(assist)。
- 末 2 轮注入"请本轮直接给最终诊断,不要再调工具";每轮注入已调工具列表(反幻觉)。

### 2.5 证据压缩(喂给 LLM)
- 动态预算 = 剩余上下文 **30%**,per-tool = 总额/N,clamp `[500, 4000]` token。
- `SmartTruncate` 保留头 70% + 尾 20%。
- 受控证据路径(小模型)`DBAA_EVIDENCE` 块:`HARD_FACTS`(确定性标记)/`CANDIDATE_FINDINGS`/
  `TOOL_STATUS`/`TOOL_EVIDENCE`(per-tool 限额:health 1600字/30行;waits/blocktree 900/16;
  topsql/slowsql 1800/18;总 cap 12000 字)。
- 输出后**校验器**:LLM 报告与证据冲突("当前无阻塞链"却说"存在阻塞")→ 拒绝,
  回退确定性 `renderCurrentDBExpertReport`。当前快照无故障时**门控危险 SQL**
  (reset_unique_sql/pg_stat_reset/TRUNCATE/kill/ALTER SYSTEM)。

---

## 3. WDR 分析(3 层契约,最严谨)

`wdranalyze/synthesizer.go` `buildWDRSystemPrompt`:
- **Layer 1 总览**:确定性 scorecard(5 模块:Database Stat → Load Profile → Instance
  Efficiency → IO Profile → TopSQL),评级 🔴/🟡/✅ **由规则引擎给定,LLM 不得改评级**。
- **Layer 2 风险详解**:仅对 🔴/🟡 模块,每个 `### R<N>: 标题 — 模块 icon` +
  关键指标表(指标/实测值/阈值-基线/**偏离倍数**,≥2行)+ 根因 + 业务影响 + **↔R# 交叉引用**。
- **Layer 3 优化方案**:`P0→P1→P2` 表(优先级/优化项/**关联R#**/操作/预期效果)。
- **后置校验器** `ValidateAndPatch`:LLM 漏报必填的 Layer 或 fallback finding → 代码**补回**
  "⚠️ 补充兜底警告(LLM 漏报)";LLM 失败 → 纯规则 findings 兜底。LLM 严格**增量于**确定性安全网。
- 5 条 `RunFallbackRules`(必出,无视 LLM):autovacuum=off / deadlock>0 / replication_lag>60s /
  buffer_hit<80% / single_sql>50% DB Time。

---

## 4. 各诊断命令清单(查询 / 阈值 / 提示词)

> skill 描述是给 LLM 的"这个工具干什么"提示。多数命令本身**无数值判断**(判断在 §5)。

| 命令 | 查什么(SQL/维度) | 内置判断 | skill 描述 |
|---|---|---|---|
| health | 实例(version/uptime/cache hit/TPS)、内存、会话、维护(dead_tup/XID)、复制、扩展 | §5 阈值 | health overview: connections/cache/dead tuples/replication/XID |
| waits | `pg_stat_activity` 按 wait_type/event 分组,LIMIT 15;业务 vs 后台线程拆分 | 主导等待 >50% 给提示;biz=0&bg>0→"仅后台空闲" | wait event distribution snapshot |
| activesessions | `pg_stat_activity WHERE state='active'`:pid/user/wait/elapsed/query | 无(列表) | list active sessions with wait events |
| ash | 同 waits + 占比 `pct = count/sum() over()` | 无(% 条) | active session history approximation |
| slowsql | `dbe_perf.statement WHERE avg_ms > 阈值`,ORDER BY avg desc LIMIT 20 | **阈值 1000ms** | slow SQL exceeding threshold |
| topsql | `dbe_perf.statement` ORDER BY el/ae/ex/lr/rw LIMIT 20 | 无(排名) | top SQL by elapsed/avg/exec/reads/rows |
| **blocktree** | `pg_locks` 自连接(**非** pg_blocking_pids)。**必须 `kl.granted`** 防 N×(N-1) 爆炸;blocker→blocked 树,环检测 | 无数值;链数+victim 数 | blocking chain tree via pg_locks self-join |
| locks | 同上但无 granted 要求,扁平列表 | 无 | lock waits blocker/blocked |
| vacuum | `pg_stat_user_tables WHERE n_dead_tup>0`:dead_pct/last_vacuum… LIMIT 30 | 无(filter) | vacuum status, dead tuples, last vacuum times |
| autovacuum | 在跑 worker(`pg_stat_activity`)+ `pg_stat_all_tables WHERE n_dead_tup>100` | filter>100 | in-progress autovacuum workers + top dead-tuple tables |
| bloat | `pg_stat_user_tables WHERE dead_pct > 阈值` + total size | **阈值 5%** | table/index bloat by dead tuple ratio |
| xid | `pg_database` datfrozenxid + `xid_age=txid_current()-datfrozenxid` | 无(prompt: >15/18/20亿) | XID age & wraparound risk per database |
| indexhealth | 4 维:未使用(idx_scan=0)/无效(NOT indisvalid)/重复(GROUP BY indkey)/大(>10MB) | 按段排序,无数值 | unused/invalid/duplicate/large index |
| replication | `pg_is_in_recovery` 分支:`pg_stat_replication` / `pg_stat_wal_receiver` | 0 副本告警 | streaming replication status |
| slots / logicalslots | `pg_replication_slots`(逻辑槽含 confirmed_flush/catalog_xmin/retained_wal) | 无(逻辑槽可阻塞 VACUUM) | replication slots / logical slots |
| checkpoint | `pg_stat_bgwriter` + 7 个 checkpoint 设置 | **WARN:req > timed → 增大 max_wal_size** | checkpoint counters/timing/settings |
| wal | `pg_current_xlog_location` + `pg_stat_archiver` + bgwriter | failed_archive≠0 才详列 | WAL status |
| tempusage | `pg_stat_database` temp_files/bytes + 在 spill 的会话 | 无 | temp file usage + live spilling sessions |
| gsmem | shared_buffers + cache hit + `gs_total_memory_detail`(OG) | 无 | OG memory breakdown |
| params | `pg_settings` LIKE 或 ~50 参数白名单 | 无 | search parameters |
| **alert** | `pg_stat_database WHERE conflicts>0 OR deadlocks>0 OR temp_files>0` | **deadlocks>0→ERROR;conflicts>0→WARN** | conflicts/deadlocks/temp files |
| 其它 OG | respool / lwlocks / bgworker / hotkey / mot / cmha / perfsnap / segments / planhistory | — | — |

---

## 5. 阈值汇总(codexgo 应对照)

**`healthcheck/healthcheck.go`(/health + /dbtop 共享静态策略):**
```
Uptime WARN <3600s   CacheHit WARN <99.0%   Conn WARN ≥80%
Active WARN ≥50      IdleInTx WARN ≥10      DeadTuples WARN ≥100000
XIDAge WARN ≥50% of 2147483647
```

**`monitor/dbtop/health.go`(实时 TUI,两级 warn/crit):**
```
ActiveSessions warn>20 crit>50    CacheHit warn<95%
DBTime% warn>5 crit>10            WaitRatio% warn>30 crit>60
TPS drop% warn>50 crit>80         Event PCT% warn>30 crit>50
Session elapsed warn>300s crit>600s
```
> 注意:CacheHit 在 /health 是 **99%**、在 /dbtop 是 **95%** —— 两套策略。

**`sentinel/classify*.go`(24 条根因规则,confidence 分层,gated >0.3):**
```
锁等待: lockWaits≥3 或 blockers≥1 (0.6; ≥5/≥2→0.8)
IO瓶颈: tempRate≥50MB/s 或 ioWait≥30% 或 (cacheHit<70%&active≥10);升级 200MB/s/50%/70%
慢查询: longQueries≥2 (>30s 算 long);≥3&占比>0.5→0.7
Vacuum滞后: deadRatio≥20% (≥50%→0.85);deadCritical≥40% (>60%&xidAge>30%→0.9)
XID回卷: xidAge≥50% (≥80%→0.95)
WAL: walRate≥100MB/s (≥500→0.8)
连接风暴: connPct≥80% (≥95%→0.9)
复制延迟: ≥10s (≥60s→0.8)
Checkpoint风暴: req≥10 (≥50→0.8)
IdleInTx: ≥5(10/20分层)  Deadlock: >0(>1→0.85)  Temp spill ≥50MB/s
活跃突增 ≥20(50/100)  阻塞链 victims ≥5(>20→0.9)  多条慢SQL ≥3 个 >1s
```
**Sentinel 突发检测**:baseline 窗口 60,**sigma 阈值 3.0**,持续 3 次。15 个指标
(active_sessions/idle_in_transaction/lock_waits/long_queries/xact_commit_rate/cache_hit_pct/
dead_tuple_ratio/xid_age_pct/replication_lag_sec/temp_bytes_rate/checkpoints_req/wal_bytes_rate/
connections_pct/blocker_count/deadlocks)—— **维度覆盖清单**。

**prompt 知识(skill 里没有、只在 OG 知识块):** bloat >20%/>50%;XID >15亿/18亿/20亿;
autovacuum_freeze_max_age 默认 2亿;等待事件解释表;VACUUM 被阻 3 因(长事务/复制槽/prepared xact);
SQLSTATE 速查(53200 OOM / 53300 too many connections)。

---

## 6. 渲染机制

- `internal/format/panel.go`:box-drawing 面板(┌─┤└),**runewidth 处理 CJK 宽度**,ANSI 颜色。
- 状态:`✓`(green)/`⚠`(yellow)/`✗`(bold red);严重度 🔴🟡✅。
- 连接使用率等用进度条 `████░░░░`。
- `/health` 无综合评分,只 "X OK, Y WARN" 计数。

> ⚠️ codexgo 教训:opendb 自有 TUI + runewidth box 渲染。codexgo 走 goldmark,
> ① 原本无表格扩展(模型 markdown 表格 collapse,已修)② box-drawing 是
> East-Asian-Ambiguous 宽度,CJK locale 下算 2 → 框线错位,故 codexgo 表格用
> **纯 ASCII 框线 `+ - |`**(确定 1 宽)。

---

## 7. codexgo 现有工具 vs opendb(差距)

codexgo 插件现有:`connect / health / slowsql / topsql / sqlfetch / planhistory /
explain / ash / indexhealth / sqltune / sqltune_verify / wdr / wdranalyze / help`。

**已对齐或更优**:health(有 0-100 加权评分,opendb 无)、sqltune(self-cost 排序 +
改写 cost+等价校验 + 两趟结构化 + 插件渲染)、ash(= activesessions + waits)、
indexhealth(4 维)。

**缺失维度**(opendb 有、codexgo 无独立工具):
- **锁阻塞链 blocktree**(pg_locks 自连接 + granted 防爆炸 + 环检测)
- **长事务 longtx**、**临时文件 tempusage**(在 spill 的会话)
- **checkpoint**(req vs timed)、**alert**(deadlock/conflict 严重度)
- **死元组 per-table 分布**(health 只有总数)
- **逻辑复制槽 catalog_xmin**(阻塞 VACUUM)

**编排差距(最关键)**:opendb 用强制工具集 + 诊断系统提示词逼 GLM 多维;codexgo 无,
模型问"有什么问题"只查单维。

---

## 8. codexgo 的对策(已定方向)

**不靠"逼模型"(prompt 对 GLM 不可控),而是插件侧确定性多维采集:**
让 `health`(诊断入口)**自己一次查全多维**(health + 等待 + 慢SQL + 锁阻塞链 +
死元组分布 + 索引),确定性检测各维问题,渲染综合报告。多维由确定性保证全覆盖,
GLM 偷懒也漏不了维度。LLM 仅负责在多维证据上做根因/方案综合(可选两趟)。

报告渲染沿用已落地的:ASCII 框线表格(避 CJK 错位)、self-cost 代价热点、
对比条、树形因果链、【实测】vs【AI推断】标注、危险 SQL 门控。
