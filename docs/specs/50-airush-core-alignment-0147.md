# 50 — airush-core：抽核对齐 0.147（簇 D 接口 + core 五块 + 缝合）

> **APPROVED**（承接 airush spec-1.8，maintainer approve 2026-08-16：airush spec-1.8 §8 Q1-Q9 全★，
> 其 D0 即本 spec）。本 spec 是 airush 抽核分支 `airush-core` 的正式载体：**分支上实施，
> 是否合回主线另定**（airush spec-1.8 §1.2 #9）。范围与依据：`~/airush/specs/spec-1.8-agent-runtime.md` D0、
> `~/airush/docs/codexgo-diff-inventory-bcd.md`、`~/airush/docs/codexgo-diff-inventory-core.md`。

| | |
|---|---|
| **Phase** | 6 — Extensibility（forward-sync 轨，分支 `airush-core`） |
| **Status** | D0.1–D0.9 已落地（2026-08-16）；余项见 changelog"未做（登记）"列 |
| **Depends on** | 49（簇 A，已完成）；airush spec-1.8 |
| **Size** | L（~4.2k LOC 增 + 净删 goals/agent_jobs） |
| **Drop-in critical** | ☆（服务化消费者是 airush，不是 CLI 用户；wire 兼容以 protocol 新变体为主） |

## 决策点（随 airush spec-1.8 approve 生效）

1. ✅ 在 `airush-core` 分支实施，不动主线；DEVIATIONS 以 `forward-synced (spec 50, target 0.147)` 登记领先项，
   以 `accepted`/`review` 登记删除项（goals/agent_jobs）；
2. ✅ **缝合（seam）是本 spec 的一部分**：抽核目标包（core/protocol/api/client/mcp/multiagent/agentgraph/threadstore/
   rollout/state/config/modelproviderinfo/modelsmanager/otel/skills/tools/msghistory/hooks）当前**直接 import**
   了 airush 不要的包（sandbox/unifiedexec/pty/shellcmd/applypatch/execserver/codemode/gitutils/keyring/
   networkproxy/filesearch/appserverproto，共 24 处非测试文件），并因此拖进 go-git/goja/age/keyring/pty/
   modernc-sqlite/mvdan-sh 等第三方模块。缝合原则：**本地执行类工具处理器与 SQLite 实现移到子包**
   （`core/localtools`、`state/sqlite`、`agentgraph/sqlite`、`secrets/keyring`），CLI 侧接线不变、行为不变（parity 差分不动），
   airush 只 import 接口与 PG 实现；
3. ✅ ThreadStore 接口对齐 0.147 的 32 方法：**接口先行、实现分期**——`in_memory`/`local` 对未实现方法返回
   `ErrorKindUnsupported`（0.136 CLI 路径不受影响）。

## 目标 / Goal

把 codexgo 的 agent loop 抽到 0.147 该有的形态并可被 airush 以 `go.mod replace` 消费：
线程模型接口对齐、id v7、steer 准入、上下文窗口/预算/压缩、集中审批阶段、客户端健壮性、
协议新增、multiagent 失败上抛、删上游已放弃项、缝合掉不要的依赖。

## 源参考 / Source reference

上游 rust-v0.147.0（`reference-codex-0147/codex-rs`），逐项：

| 需求 | 上游文件 |
|---|---|
| D0.1 ThreadStore | `thread-store/src/store.rs`（32 方法）、`local/{delete_thread,paginated_fork,model_context,thread_history*}.rs`；`protocol/src/{thread_id,response_item_id}.rs`（v7） |
| D0.2 steer 准入 | `core/src/session/input_queue.rs`、`core/src/user_message_admission.rs`、`core/src/tools/handlers/multi_agents_v2/wait.rs`（`WaitOutcome::Steered`） |
| D0.3 上下文窗口/压缩 | `core/src/session/{context_window,token_budget}.rs`、`core/src/{compact_token_budget,compact_model_fallback,compact}.rs`、`protocol/src/compacted_item.rs`、`core/src/tools/handlers/{get_context_remaining,new_context_window}.rs` |
| D0.4 审批阶段 | `core/src/tools/approvals.rs`、`core/src/tools/executed_tool_calls.rs`（guardian 不做） |
| D0.5 客户端 | `core/src/client.rs`（`prepare_response_items_for_request`、`response_items_equal_ignoring_internal_metadata`、`build_responses_compatibility_headers`、`reasoning_effort_for_request`、`responses_retry_tests.rs`） |
| D0.6 协议 | `protocol/src/protocol.rs`（`EventMsg` +6）、`protocol/src/models.rs`（`ResponseItem` +2） |
| D0.7 multiagent | `core/src/tools/handlers/multi_agents/wait.rs`（`Errored|NotFound → Failed`）、`core/src/agent/control/execution.rs` |
| D0.8 删除 | 上游 0.147 无 `goals.rs`、`tools/handlers/agent_jobs*` |
| D0.9 缝合 | （无上游对应；本仓结构调整） |

## 功能需求 / Functional requirements

按 airush spec-1.8 §1.1 D0.1-D0.8 + 决策点 2 的 D0.9（缝合）。每项一个 commit 序列、各带用例；
`threadstore` 导出 `contracttest` 包供 airush pgstore 复用。

## 验收方案 / Acceptance criteria

airush spec-1.8 §4 C1-C9 + 缝合验收：`go list -deps` 抽核目标包不再引用 sandbox/unifiedexec/pty/shellcmd/
applypatch/execserver/codemode/gitutils/keyring/networkproxy/filesearch/appserverproto；第三方模块清单收敛到
airush 批准集；`go build ./...` + parity 差分不红（删除项已登记）。

## 风险与难点 / Risks

- 缝合触及 core 的工具注册（`tool_executors.go`/`managers.go`）：移动处理器时 CLI 侧行为必须逐位不变——parity 差分兜底；
- steer/上下文窗口两块改动 turn 主循环，回归面大——单测 + 现有 turn 集成用例；
- 分支与主线漂移——DEVIATIONS 逐项、周期 rebase。

## 非目标 / Non-goals

guardian 实现、agent_jobs/goals 重建、本地文件形态的 thread-store 新工程（写锁/迁移/压缩/反向扫描）、
远程压缩、TUI 任何变化、合回主线（另定）。

## 实施 changelog（approve 后追加，不改上文）

### 2026-08-16 D0.9 缝合落地（8 个 commit：8f0527a…bffb7cc）

实际子包名与决策点 2 的预估不同（按各包既有结构就近落位），逐项：

| 缝 | 动作 | 结果 |
|---|---|---|
| S3 | `GitSha`/`GitInfo` 唯一定义落 `protocol`（Rust 本就在 `codex_protocol::protocol`）；`gitutils`/`threadstore` 保留别名；`rollout` 不再 import `gitutils`——`CreateParams.GitInfo`（`GitInfoCollector`）由本地 thread store 注入，nil 不记 git | rollout 闭包去掉 go-git |
| S1 | `threadstore/local*.go`（文件 + SQLite state）→ `threadstore/local`；父包只留契约/类型/patch/错误/in-memory；导出 `NewInternalError`/`NewInvalidRequestError`/`NewThreadNotFoundError`/`OnRequestApproval`/`ReadOnlyPermissionProfile`/`GitInfoFromParts`/`Flatten` | threadstore 闭包去掉 modernc-sqlite/go-git |
| S2 | `agentgraph/local.go`（SQLite）→ `agentgraph/local`；行为用例抽成 `agentgraph/agentgraphtest.RunSuite`（in-memory / SQLite / 外部 PG 实现共用） | agentgraph 闭包去掉 sqlite |
| S6 | `codemode` goja 引擎 → `codemode/engine`（定义/描述/TS schema 留 `codemode`；`PublicToolName`/`WaitToolName` 移到 codemode）；新叶子包 `ptycap`（`ConPTYSupported` build-tag）供 `tools/tool_config` | tools 闭包去掉 goja/creack-pty |
| S7 | 本地命令执行 → `core/localexec`：exec_command/write_stdin、shell_command、apply_patch、`ExecService` 契约、turn 沙箱策略解析、沙箱拒绝升级、unified-exec 后台 watcher；core 导出执行器契约（`ToolExecutor`/`ToolHandlerContext`/`NewTextToolOutput`/`TelemetryPreview`/`PayloadArguments`/`EmitTurnItem*`/`NowUnixMillis`/`TurnShellToolType`/`TurnTruncationPolicy`/`GranularAllowsSandboxApproval`/`UnsandboxedExecutionAllowed`）+ `SessionArmer`（`Spawn` 对每个注册执行器回调 `ArmSession`）；`BuiltinToolDeps` 改为 `ShellTools []ToolExecutor` + `ApplyPatch ToolExecutor`（`localexec.ShellExecutors`/`NewApplyPatchExecutor` 装配，CLI 接线已改）；删除从未被读取的 `SessionServices.ExecService`（appserver/mcpserver 配置同删）；新增 `core/coretest`（经 `core.Spawn` 造会话的测试夹具，`Session.InstallActiveTurnForTesting` 支撑审批用例） | core 闭包去掉 creack-pty/sandbox/unifiedexec/execserver/applypatch |
| S5 | `shell` 新叶子包（`ShellType`/`DetectShellType`/`UserShell`/`DefaultUserShell`，对应 Rust codex-shell）；`shellcmd` 保留别名与 bash/heredoc/URL/升级检测（mvdan） | core 闭包去掉 mvdan-sh |
| S8 | Responses websocket 传输 → `api/responsesws`（仓内无外部使用者）；导出 `api.ResponsesPath` | api/core 闭包去掉 coder-websocket |
| S4 | 系统 keyring → `keyring/system`（`keyring` 留 Store/StoreError/MemoryStore + 新 `Unavailable`）；`mcp.NewOAuthStore(store, codexHome)` + `ManagerOptions.Keyring` 注入（nil=无系统 keyring：Auto 落文件、Keyring 模式报不可用；CLI 传系统 store）；`secrets`/`login` 直接构造系统 store；仓库根发现（纯文件系统）→ `gitutils/gitroot`，`config` trust 与本地 thread store 改用之 | mcp/config/hooks 闭包去掉 go-keyring(dbus)/go-git |

**验收**：`go build ./...` 全绿；Mac 上 `dev-check.sh ./...` 除 7 个基线即失败的环境用例（`cli` skills 读宿主 `~/.agents/skills`、`uds` 临时路径超 unix socket 长度；在 e20c9d6 同样失败）外全过；airush `deploy/scripts/mac-codexgo-deps.sh` 对抽核目标包（core/coretest/protocol/api/client/mcp/multiagent/agentgraph(+agentgraphtest)/threadstore/rollout/config/modelproviderinfo/modelsmanager/skills/tools/msghistory/hooks/features）`go list -deps` 第三方模块 = `google/uuid`（airush 已有）+ `pelletier/go-toml/v2`、`klauspost/compress`、`rivo/uniseg`（2026-08-16 user 批准），**未审项为空**；目标包不再引用 sandbox/unifiedexec/pty/execserver/applypatch/state/secrets。

**parity**：CLI 侧行为不变（接线等价：shell 家族注册顺序、apply_patch 位置、watcher 装配时机、OAuth 存储模式语义均逐位保留）；parity 差分不在本机跑（基线 codex 0.147 漂移），以单测 + Mac dev-check 兜底。

### 2026-08-16 D0.1–D0.8 落地（commit a45229c → a1e9eed）

| 项 | 落地 | 与上游的差异 / 未做（登记） |
|---|---|---|
| D0.1 ThreadStore | 接口 31 方法（Rust 32 含 as_any，Go 用类型断言）；`UnimplementedStore` 可嵌入默认 + `ArchiveThreadsSequentially`/`DeleteThreadsSequentially`；0.147 类型全套（model context / fork / turns / items / occurrences / sections / bulk）；`CreateThreadParams`/`StoredThread`/`ThreadMetadataPatch` 字段对齐（`ReasoningEffort` clearable、`AdvanceRecencyAt`、`ParentThreadID`、`RecencyAt`、`HistoryMode`、`Section*`）；`threadstore/contracttest` 存储中立契约套件（in-memory / local 通过）；appserver 默认 ThreadIDFactory=UUIDv7 | local：`LoadLatestModelContext`=全量、`DeleteThread`=Unsupported、sections/occurrences/paginated=Unsupported、不持久化 `ParentThreadID`/recency（契约套件用能力开关表达）；`EventPersistenceMode` 保留在 create/resume params（0.147 已移出）；契约套件揪出并修复 local 首次 `UpdateThreadMetadata` 无 state 行时应建行的偏差 |
| D0.6 协议 | `EventMsg` +turn_moderation_metadata/safety_buffering/raw_response_completed/sub_agent_activity；`ResponseItem` +agent_message/additional_tools；`protocol.NewThreadIDV7`/`NewSessionIDV7`/`NewResponseItemID`/`NewUUIDV7`；`ThreadHistoryMode`/`HistoryPosition` | 0.147 对既有 `ResponseItem` 变体改为 id 有则序列化（skip_serializing_if）——涉及全部 rollout/wire 输出与 parity 差分，未做；`internal_chat_message_metadata_passthrough` 未做；0.147 event_mapping 兜底 item id 仍是 v4，无需改 |
| D0.5 客户端 | 采样重试循环（run_sampling_request + responses_retry：可重试性分类、200ms×2^n±10% 退避、stream_error "Reconnecting... n/max"、`TurnContext.StreamMaxRetries` 默认 5 上限 100）；`ErrorAwareModelClient`（中途流错误不再被吞）；`ReasoningEffort` +max/ultra 与 `ForRequest()` ultra→max；`ModelClientConfig.Transport` 已是外部 `*http.Client` 注入点 | Responses 兼容头 / memgen 头 / `prepare_response_items_for_request`（去非前缀 id）/ 忽略内部元数据的项去重：均依赖 0.147 的 id 序列化与 OpenAI 专属头，未做；websocket 回退传输不存在于 codexgo |
| D0.7 multiagent | `TurnItem` +CollabAgentToolCall（status in_progress/completed/failed）；v1 wait_agent 发生命周期项，任一目标 Errored/NotFound → failed（子 agent 失败上抛）；`multiagent.ExecutionLimiter` + `CountingExecutionLimiter`（按执行 turn 的 agent 数计），`Control.sendOp` 起 turn 时取槽、离开 Running 释放，满则 `ErrAgentLimitReached` | 上游仅限 v2 子 agent，codexgo 只有 v1 → 对所有子 agent 生效（nil 不限）；spawn/send_input/resume/close 四个工具的 CollabAgentToolCall 项未发（只做 wait，spec 范围）；agent_jobs 从未移植 |
| D0.8 删除 | `internal/ext/goal` 整包、core goal 执行器 + `BuiltinToolDeps.GoalTools`、cli 装配/事件 sink、`state/goals.go` CRUD | `state` 仍打开/迁移 goals_1.sqlite（0.147 state crate 同样保留）；协议 `ThreadGoalUpdated` 与 TUI `/goal` 项按 0.147 保留；`auto_compact_window` 旧判定在 D0.3 由 context_window 状态取代 |
| D0.2 steer 准入 | `core/input_queue.go`（steer/mailbox 活动、mailbox 排空 + 父 turn id 唯一归约、pending 检测、推迟到下一 turn）；`Session.SteerInput` + `SteerInputError`；`handleUserInput` → 准入语义（有 regular turn 则 Steered，否则 Started；review/compact 不可 steer → BadRequest 事件，不再静默替换）；`Codex.SubmitUserMessage` 返回 `UserMessageAdmission`；runTurn 每次采样前排空 pending、模型说完但有 pending 继续；`OpInterAgentCommunication` 接入 mailbox（trigger_turn 且空闲起 turn，邮件记为 agent_message 项）；v1 wait 被 steer 中断 | 上游 Submission 的 `parent_turn_id` 未贯穿（mailbox 记 nil）；additional_context / realtime 镜像 / thread-settings-applied 回显未做 |
| D0.3 上下文窗口 | `auto_compact_window` 0.147 形态（window_number、UUIDv7 窗口链、new_context 请求、每窗口一次的提醒/回退）；`context_window.go`（`ContextWindowTokenStatus`：Total/body_after_prefix、回退缓冲、硬上限、剩余基线；活跃上下文改按上游 `get_total_token_usage`——原实现误用累计总量）；`TokenBudgetConfig` + 提醒/回退提示；`Session.StartNewContextWindow`（换窗、compacted 行带窗口链元数据、window id 推给 `WindowIDSetter`）；runTurn 采样后按 needs_follow_up && (new_context 请求 \|\| 达阈值) 中途 rollover；预采样判定统一走状态；工具 get_context_remaining / new_context（TokenBudget 开启时广告）；`ResponsesModelClient.SetWindowID` | 远程压缩、compact_model_fallback（模型切换压缩回退，遥测语义）未做；`TokenBudget` 与 `AutoCompactTokenLimitScope` 由 `SessionConfiguration` 注入（无 config.toml 键，CLI 默认关闭） |
| D0.4 审批阶段 | `core/approvals_stage.go`：hooks → 自动评审（`ReviewerApprover`，`SessionServices.Approver`）→ 用户三路 + approved-for-session 缓存 + `ToolDecisionRecorder`；`ReviewDecision.Rejection`（0.147 Denied{rejection}，双形式 wire）；`TurnContext.ApprovalsReviewer`；localexec 沙箱升级改经 `RequestApproval` | guardian 本体不做（接口留）；`executed_tool_calls`（用户 shell 命令回灌）不做——codexgo 无 run_user_shell_command；shell/exec/apply_patch 首次审批仍在各执行器内（沙箱升级重试已统一，首次提示 STUB 未接入阶段） |

**验收**：`go build ./...`、`go vet ./...` 全绿；Mac `dev-check.sh ./internal/core/... ./internal/threadstore/... ./internal/multiagent/... ./internal/protocol/... ./internal/appserver/...` 全过（cli 5 个 skills 环境用例基线即失败）；airush `mac-codexgo-deps.sh` 未审项仍为空。
