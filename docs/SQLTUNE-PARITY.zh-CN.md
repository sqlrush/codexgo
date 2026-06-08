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

> **状态更新(插件 v0.3.0,2026-06-08):A 区(确定性能力)已补齐**,并在真实
> openGauss 5.0.3 上端到端验证。`sqltune` 现在:回填绑定变量(#2)→ 采集 plan+cost
> (#4)、plan/SQL 反模式标注(#6)、schema(表/索引/统计/FK,#7)、运行时等待/锁(#8)、
> 视图定义(#3)、gs_index_advise → 生成机械改写候选并用 **plan cost 前后对比(#11)
> + 结果等价性抽样校验(#12)** 验证;`analyze=true` 出 EXPLAIN PERFORMANCE(#5)。
> 下表的"codexgo 插件"列为补齐前的原始差距分析,保留作为对照。补齐细节见
> `plugins/codexgo-db-gaussdb/internal/tools/{bindfill,plan,annotate,schemactx,runtimectx,viewexpand,candidates,verify,tune}.go`。
> 仍按设计保留在 codexgo 模型侧的是 B 区(LLM 分析/多轮编排)。

## A. 确定性能力(缺了就是真缺口)

| # | 能力 | opendb | codexgo 插件 | 性质 | 结论 |
|---|------|:---:|:---:|:---:|------|
| 1 | 按 SQL_ID 还原语句(statement_history→statement) | ✅ | ✅ | 确 | **等价** |
| 2 | 绑定变量回填(类型感知:查 information_schema 列类型 + 启发式默认值) | ✅ | ❌ 仅检测占位符并警告,不回填 | 确 | **缺口(关键)** |
| 3 | 视图展开(`pg_get_viewdef` 递归,让分析看到真实底表) | ✅ | ❌ | 确 | **缺口** |
| 4 | 执行计划采集(EXPLAIN[ANALYZE],按结果行数分级开 ANALYZE+超时,JSON→计划树) | ✅ 结构化树 | ⚠️ 单次 EXPLAIN TEXT,不分级、不解析树 | 确 | **偏弱** |
| 5 | `EXPLAIN PERFORMANCE`(openGauss 每算子 11 列性能) | ✅ | ❌ | 确 | **缺口** |
| 6 | 计划反模式标注 | ✅ 丰富 | ⚠️ 仅 3 类(seq scan / sort / nested loop) | 确 | **偏弱** |
| 7 | Schema 上下文(表/索引/统计/FK,4 路并行) | ✅ | ❌ | 确 | **缺口** |
| 8 | 运行时上下文(等待事件 + 锁快照) | ✅ | ❌ | 确 | **缺口** |
| 9 | 引擎索引顾问 `gs_index_advise` | (用自有候选) | ✅ 直接调 | 确 | **codexgo 反而有** |
| 10 | 确定性改写候选(非 LLM 规则改写) | ✅ | ❌ | 确 | **缺口** |
| 11 | 改写后执行计划校验(前后 cost 对比) | ✅ | ❌ | 确 | **缺口(关键)** |
| 12 | 等价性校验(hash 抽样行对比,改写前后结果一致) | ✅ | ❌ | 确 | **缺口(关键)** |

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
- **真正的缺口**(A 区 2/3/5/7/8/10/11/12 + B 区 16):这些**不需要 LLM**,本该留在插件里,
  当初被一并砍掉是过度解耦。
- **最伤的三个**:
  - **#2 绑定变量回填** —— 归一化 SQL 带 `?` 时插件直接 EXPLAIN 不了。
  - **#11 改写后 cost 校验 + #12 等价性校验** —— 没有它,模型给的改写/索引"建了到底有没有用"
    无人验证(实测中模型就给过对 `LIKE '%test%'` 无效的索引建议)。

## 若要补(优先级建议,非本次)

1. 绑定变量回填(#2)——解锁"归一化 SQL 也能调"。
2. 改写/索引候选的**前后 EXPLAIN cost 对比 + 等价性校验**(#11/#12)——杜绝"无效建议"。
3. Schema / 运行时上下文(#7/#8)+ 更全的计划标注(#4/#6)——喂给模型更准的素材。
4. 视图展开(#3)、EXPLAIN PERFORMANCE(#5)、确定性候选(#10)。
5. 记忆集成(#16)接 codexgo 自己的 memory。

> 架构原则保持不变:**确定性机器活回插件(返回"已验证"素材),自然语言综合判断留给 codexgo 模型。**
