# codex 斜杠命令 × codexgo 可用性 × opendb 产品价值

> 评估日期:2026-06-07 · 对应 codexgo v0.3.0
> 用途:为 opendb(数据库专用 CLI Agent + L4 全自动运维平台)集成 codex
> 交互能力时的命令优先级排序提供依据。

## 评估口径

**codexgo 列**(接线状态,详见 `docs/UNWIRED.zh-CN.md`):

- ✅ 已接线可用(选中能执行)
- ◻ 底层引擎已移植,差 TUI 接线
- ▢ 整包孤岛(子系统实现完整但未接主循环)
- ⛔ 补全弹窗能力开关写死为空,命令被门控隐藏(`composer.slashPopup` 用
  `BuiltinCommandFlags{}`;数据在会话里,只差解析接线)

**opendb 价值列**:从「数据库运维 Agent」视角(只读安全、报告驱动、7×24
长时自治、多国产模型、Oracle/MySQL)判断借鉴/复用价值。

---

## A. 上下文 / 会话管理

| 命令 | 作用 | codexgo | opendb 价值 |
|------|------|---------|------------|
| `/compact` | 摘要压缩防爆上下文 | ✅ | 高 — 7×24 长会话运维必需 |
| `/new` `/clear` | 新会话 / 清屏 | ✅ | 高 — 切库/切故障场景重置 |
| `/status` | 模型+token+会话配置 | ◻ | 高 — 看用量/当前连库 |
| `/resume` | 恢复历史会话 | ◻ | 高 — 故障排查续接 |
| `/fork` | 分叉当前会话 | ◻ | 中 — 多方案并行推演 |
| `/archive` | 归档退出 | ◻ | 中 — 运维工单归档 |
| `/rename` `/title` | 重命名 / 标题项 | ✅ / ◻ | 中 — 按库/工单命名 |

## B. 模型 / 能力

| 命令 | 作用 | codexgo | opendb 价值 |
|------|------|---------|------------|
| `/model` | 选模型+推理力度 | ✅ | 高 — 多国产模型(GLM/DeepSeek)切换刚需 |
| `/personality` | 沟通风格 | ⛔ | 低 — 运维报告要严谨 |

## C. 安全 / 权限 / 沙箱(opendb 最相关)

| 命令 | 作用 | codexgo | opendb 价值 |
|------|------|---------|------------|
| 审批弹窗(exec/patch) | 危险操作确认 | ✅ (v0.3.0) | 极高 — opendb「只读+方案报告+人工执行」即审批门 |
| `/permissions` | 允许 Agent 做什么 | ◻ | 极高 — 精确控制 Agent 对 DB 的能力边界 |
| `/approve` | 批准一次自动审查重试 | ◻ | 高 — Autopilot 自治时的人工放行 |
| `/setup-default-sandbox` `/sandbox-add-read-dir` | 沙箱设置/加只读目录 | ⛔ / ◻ | 高 — 限制 Agent 文件/网络访问面 |
| `/experimental` | 开关实验特性 | ◻ | 低 |

## D. 任务编排(对 Autopilot 最相关)

| 命令 | 作用 | codexgo | opendb 价值 |
|------|------|---------|------------|
| `/plan` | 进入 Plan 模式 | ⛔(collab 引擎在) | 高 — 出「修复方案报告」天然契合 Plan |
| `/goal` | 设/看长任务目标 | ⛔(goal store 在) | 高 — 长时自治运维目标管理 |
| `/agent` `/subagents` | 切换活动 agent 线程 | ◻(collab 在) | 高 — 三层 Agent 集群多 agent 协作 |
| `/side` `/btw` | 临时分叉旁路对话 | ◻ | 中 — 临时查询不污染主线 |

## E. 审查类(代码场景为主,DB 场景需改造)

| 命令 | 作用 | codexgo | opendb 价值 |
|------|------|---------|------------|
| `/review` | 审查当前改动 | ✅ | 中 — 可改造为「审查 SQL 变更/DDL」 |
| `/diff` | git diff | ✅ | 中 — 可改造为「schema/配置 diff」 |
| `/init` | 生成 AGENTS.md | ◻ | 中 — 生成库级运维约定文件 |

## F. 扩展生态

| 命令 | 作用 | codexgo | opendb 价值 |
|------|------|---------|------------|
| `/mcp` | 列 MCP 工具 | ◻(server 在,client 孤岛) | 高 — 接外部 DB 工具/监控系统 |
| `/skills` | 技能改进特定任务 | ◻ | 高 — 沉淀「巡检/慢查询分析」技能 |
| `/memories` | 记忆使用与生成 | ▢(孤岛) | 高 — 沉淀历史故障知识库 |
| `/hooks` | 生命周期钩子 | ◻ | 中 — 巡检前后挂自动化 |
| `/plugins` `/apps` | 插件/应用 | ⛔ / ▢(connectors 孤岛) | 中 — 生态化扩展 |

## G. 上下文注入

| 命令 | 作用 | codexgo | opendb 价值 |
|------|------|---------|------------|
| `/mention`(@文件) | 引用文件 | ◻ | 高 — 可改造为 `@表/@schema/@AWR报告` |
| `/ide` | 引入 IDE 选区/打开文件 | ◻ | 低 — 运维少用 IDE |

## H. 终端 UI 偏好(产品无关)

| 命令 | codexgo | opendb 价值 |
|------|---------|------------|
| `/theme` | ◻ | 低 |
| `/vim` `/keymap` | ◻ | 低 |
| `/pets` | ◻ | 低(趣味) |
| `/statusline` | ◻ | 中 — 状态行显示「当前库/延迟/告警数」 |
| `/raw` `/copy` | ◻ | 中 — 拷贝报告/SQL |

## I / J. 实时语音 & 杂项

| 命令 | codexgo | opendb 价值 |
|------|---------|------------|
| `/realtime` `/settings` | ▢(孤岛+需音频UI) | 低 — 运维场景边缘 |
| `/ps` `/stop` | ◻ | 中 — 管理后台终端 |
| `/logout` `/feedback` `/rollout` | ◻ | 中 |
| `/debug-config` `/debug-m-*` `/test-approval` | ◻ | 低(调试) |

---

## opendb 视角结论

**最该借鉴到 opendb 的(核心价值)**:

1. **安全/权限/审批三件套**(C 组)— opendb「只读+方案报告+人工执行」的安全
   模型,正好对应 codex 的审批弹窗 + `/permissions` + 沙箱。opendb 最该吸收。
2. **Plan/Goal/Agent 编排**(D 组)— 对 Autopilot 三层 Agent 集群高度契合
   (修复方案=Plan,长任务目标=Goal,多 agent=Agent)。
3. **上下文管理**(A 组 `/compact` `/status` `/resume`)— 7×24 长会话基础设施。
4. **可改造类**:`/mention`→`@表/@AWR`、`/review`→审查 DDL/SQL、`/diff`→schema
   diff、`/memories`→故障知识库、`/skills`→巡检技能。

**价值低、可不接**:`/personality` `/theme` `/vim` `/pets` `/ide` `/realtime`。

**接线即得(不需重新开发)**:opendb 想要的高价值命令里,审批(✅)、`/model`
(✅)、`/compact`(✅)已可用;`/permissions` `/status` `/plan` `/goal`
`/agent` `/mcp` `/skills` `/memories` 都是「底层引擎在、差接线」。

## 待补:codex × opendb 现有斜杠命令差集

opendb 交互模式已有自己的斜杠命令体系(README:斜杠命令+SQL+自然语言)。
待用户提供 opendb 现有命令清单后,在此补「codex 有、opendb 缺、值得补」的
差集,使本表直接成为 opendb 的命令补全清单。
