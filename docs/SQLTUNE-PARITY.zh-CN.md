# sqltune 能力对比:opendb vs codexgo 插件

> 目的:盘点 opendb `/sqltune` 的全部能力,对照 codexgo gaussdb 插件当前实现,
> 标清**哪些是故意解耦(LLM 编排移到 codexgo 模型侧)**、**哪些是确定性能力的真实缺口**。
> 仅作规划参考,不代表立即改。
>
> 来源:opendb `internal/opengauss/sqltuner/*`、`internal/sqltune/*`、
> `internal/opengauss/skill/query/sqltune_skill.go`;codexgo
> `plugins/codexgo-db-gaussdb/internal/tools/tune.go`(+ `query.go` 的 sqlfetch)。

图例:✅ 有 · ⚠️ 部分/偏弱 · ❌ 无 ·
性质:**确**=确定性(本该在插件,不需 LLM) · **编**=LLM/编排(设计上移到 codexgo 模型侧)

> **状态更新(插件 v0.4.0,2026-06-08):A 区(确定性能力)已 100% 补齐到 1:1**,
> 并在真实 openGauss 5.0.3 上端到端验证。`sqltune` 现在:回填绑定变量(#2)→
> **结构化 JSON 计划树 + 按 SQL 行数分级 EXPLAIN ANALYZE(带超时与回退)(#4)**、
> plan/SQL 反模式标注(文本 + 树形 spill/hash/行偏差,#6)、schema(表/索引/统计/FK,#7)、
> 运行时等待/锁(#8)、视图定义(#3)、gs_index_advise → 生成机械改写候选并用
> **plan cost 前后对比(#11)+ 结果等价性抽样校验(#12)** 验证;`analyze=true` 时真跑查询出
> 真实 actual 行/时间(分级 ANALYZE)+ EXPLAIN PERFORMANCE(#5),默认只取估算计划(不执行)。
> 补齐细节见 `plugins/codexgo-db-gaussdb/internal/tools/{bindfill,plan,plantree,annotate,schemactx,runtimectx,viewexpand,candidates,verify,tune}.go`。
> 下表已更新为**补齐后的现状**。仍按设计保留在 codexgo 模型侧的是 B 区(LLM 分析/多轮编排)。

## A. 确定性能力(缺了就是真缺口)

| # | 能力 | opendb | codexgo 插件(v0.4.0) | 性质 | 结论 |
|---|------|:---:|:---:|:---:|------|
| 1 | 按 SQL_ID 还原语句(statement_history→statement) | ✅ | ✅ | 确 | **等价** |
| 2 | 绑定变量回填(类型感知:列类型启发式 + 默认值) | ✅ | ✅ `bindfill.go` 类型感知回填,归一化 SQL 可 EXPLAIN | 确 | **等价** |
| 3 | 视图展开(让分析看到真实底表) | ✅ `pg_get_viewdef` 递归 | ✅ `viewexpand.go` 返回视图定义;EXPLAIN 时 planner 自动展开底表 | 确 | **等价** |
| 4 | 执行计划采集(EXPLAIN[ANALYZE],按 SQL 行数分级开 ANALYZE+超时,JSON→计划树) | ✅ 结构化树 | ✅ `plantree.go` 结构化 JSON 计划树 + 分级 ANALYZE(<100→30s、<500→60s、≥500→plain;超时回退);`analyze=true` 触发执行 | 确 | **等价** |
| 5 | `EXPLAIN PERFORMANCE`(openGauss 每算子性能) | ✅ | ✅ `analyze=true` 时输出 | 确 | **等价** |
| 6 | 计划反模式标注 | ✅ 丰富 | ✅ 文本(seq scan/sort/nested loop)+ 树形(sort 下盘 / 高成本 hash / 估算-实际行偏差) | 确 | **等价** |
| 7 | Schema 上下文(表/索引/统计/FK) | ✅ | ✅ `schemactx.go` | 确 | **等价** |
| 8 | 运行时上下文(等待事件 + 锁快照) | ✅ | ✅ `runtimectx.go` | 确 | **等价** |
| 9 | 引擎索引顾问 `gs_index_advise` | (用自有候选) | ✅ 直接调 | 确 | **codexgo 反而有** |
| 10 | 确定性改写候选(非 LLM 规则改写) | ✅ | ✅ `candidates.go`(冗余 DISTINCT 移除;可经 `candidate` 入参验证模型改写) | 确 | **等价** |
| 11 | 改写后执行计划校验(前后 cost 对比) | ✅ | ✅ `verify.go` plan cost 前后对比 | 确 | **等价** |
| 12 | 等价性校验(hash 抽样行对比,改写前后结果一致) | ✅ | ✅ `verify.go` 1000 行抽样 hash(同 opendb 口径) | 确 | **等价** |

## B. LLM / 编排能力(设计上移到 codexgo 模型侧)

| # | 能力 | opendb | codexgo 插件 | 性质 | 结论 |
|---|------|:---:|:---:|:---:|------|
| 13 | 五维方案(改写/索引/hint/schema/stats) | ✅ 工具内 LLM 产出 + 逐项验证 | ⚠️ 插件给"五维清单",由 codexgo 模型产出(但**无验证**) | 编 | 故意解耦,**但丢了"逐项验证"** |
| 14 | quick / deep 模式 | ✅ | ❌ | 编 | 故意(交给模型自行决定深浅) |
| 15 | 自动升级(4 信号:LLM 低置信 / 改善<2× / 探索维度<3 / …→深度 3-15 轮迭代) | ✅ | ❌ | 编 | 故意/未做 |
| 16 | 记忆集成(存/取历史调优结论) | ✅ | ❌ | 混合 | 缺口(codexgo 有自己的 memory,未接) |
| 17 | 最终报告渲染(markdown,含前后 cost、已验证候选) | ✅ 工具内渲染 | ⚠️ 插件返回结构化素材,codexgo 渲染 + 模型综合 | 编 | 故意解耦 |
| 18 | 方言上下文(多库一套引擎) | ✅ | — 单库插件,隐含 gaussdb | — | 不适用(一库一 MCP) |

## 小结

- **设计上故意移走的**(B 区 13/14/15/17):LLM 分析与多轮编排 → 交给 codexgo 的模型。
  理由是模型无关(GLM/DeepSeek/任意后端)+ 插件轻量。这个方向是成立的。
- **A 区(确定性能力)已 100% 补齐**(#2/#3/#4/#5/#6/#7/#8/#10/#11/#12,至 v0.4.0),
  全部在真实 openGauss 5.0.3 上端到端验证。这些**不需要 LLM**,本该留在插件里,补齐后归位。
- **当初最伤的三个,现已全部堵上**:
  - **#2 绑定变量回填** —— 归一化 SQL 带 `?` 现在回填后即可 EXPLAIN。
  - **#11 改写后 cost 校验 + #12 等价性校验** —— 模型给的改写/索引现在可经 `candidate` 入参
    送回 sqltune,用 `cost_ratio`(前后 plan cost)+ `equivalent`(结果抽样一致)验证后再采纳。
- **唯一仍未接**:B 区 #16 记忆集成(codexgo 有自己的 memory,尚未对接调优历史)——按需再做。

## 完成情况

A 区 12 项已全部完成(#2–#12 于 v0.3.0 起补齐,#4 于 v0.4.0 升级到结构化树 + 分级 ANALYZE):

1. ✅ 绑定变量回填(#2)——归一化 SQL 也能调。
2. ✅ 改写/索引候选的前后 EXPLAIN cost 对比 + 等价性校验(#11/#12)——杜绝"无效建议"。
3. ✅ Schema / 运行时上下文(#7/#8)+ 更全的计划标注(#4 结构化树 + #6 文本/树形)。
4. ✅ 视图定义(#3)、EXPLAIN PERFORMANCE(#5)、确定性候选(#10)。
5. ⬜ 记忆集成(#16)接 codexgo 自己的 memory —— 仍待做(B 区,按需)。

> 架构原则保持不变:**确定性机器活回插件(返回"已验证"素材),自然语言综合判断留给 codexgo 模型。**
