# 50 — airush-core：抽核对齐 0.147（簇 D 接口 + core 五块 + 缝合）

> **APPROVED**（承接 airush spec-1.8，maintainer approve 2026-08-16：airush spec-1.8 §8 Q1-Q9 全★，
> 其 D0 即本 spec）。本 spec 是 airush 抽核分支 `airush-core` 的正式载体：**分支上实施，
> 是否合回主线另定**（airush spec-1.8 §1.2 #9）。范围与依据：`~/airush/specs/spec-1.8-agent-runtime.md` D0、
> `~/airush/docs/codexgo-diff-inventory-bcd.md`、`~/airush/docs/codexgo-diff-inventory-core.md`。

| | |
|---|---|
| **Phase** | 6 — Extensibility（forward-sync 轨，分支 `airush-core`） |
| **Status** | 实施中（2026-08-16 起） |
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

（待追加）
