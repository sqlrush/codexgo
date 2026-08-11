# Spec 49 需求 4 — Connection Manager 生命周期改造 · 接线设计

> 本文是 spec 49 需求 4（+需求 2 接线）的**实施前设计**：需求 4 是 spec 49 自标的
> "最大单点"，改动刚打通的 MCP 接线（MCP-WIRING）风险高，故先定接口与增量步骤，
> maintainer 认可后再动核心代码。上游对照：0.147 把单文件 `connection_manager.rs`(880 行)
> 拆为 `connection_manager.rs`(710) + `connection_manager/{required,resources,startup,
> tool_catalog}.rs`(共 +614 净增)。

## 1. 现状（codexgo，锚 0.136）

- `internal/mcp/manager.go`：`NewManager` **一次性同步**启动全部 server（`startServer`
  逐个 initialize+listTools），`Shutdown` 全关。**无**增量 reconcile、**无**非阻塞启动、
  **无**per-server 重试。
- `internal/cli/mcp_wiring.go`：`buildMcpManager` 装配期直接 `NewManager`——server 启动
  在关键路径上，慢 server 拖慢整个装配。
- 需求 1（协议协商）已接线；需求 2 缓存**数据结构已建但零 live 调用**（`ListAllToolSpecs`
  也是备而未用，live 走 `ListAllToolInfos` eager 路径）。

## 2. 目标能力（0.147 等价，需求 4 范围）

| 能力 | 0.147 位置 | codexgo 目标 |
|---|---|---|
| 非阻塞启动（缓存先行） | `startup.rs` + `tool_catalog.rs` | server 启动移出装配关键路径；引擎先用缓存工具目录（需求 2），就绪后刷新 |
| per-server 启动超时 + 瞬时重试 | `rmcp_client.rs` 退避 + `streamable_http_retry.rs` | 单 server 启动失败进重试轨（指数退避），不再"失败即跳过" |
| config 变更选择性重连 | `connection_manager.rs` reconcile | 只重连受影响 server，健康连接的 session 不动 |
| 工具目录跨重连安全复用 | `tool_catalog.rs` `publish_if_newest` | 重连期间旧目录可用，新目录就绪原子替换（需求 2 已实现的代际发布） |

## 3. codexgo 的裁剪（本仓非 ChatGPT 集成的 codex）

上游 `startup.rs` 大量逻辑与 codexgo 无关，**明确不移植**：
- `chatgpt_auth_provider_for_server`、`CODEX_APPS_MCP_SERVER_NAME` 专属缓存——codexgo
  面向非 OpenAI 后端，无 ChatGPT apps server（能力继承清单 §3.C）；
- `McpStartupUpdateEvent` 事件流——codexgo CLI 装配不消费该 TUI 进度事件流；**改为**
  最小 `ServerStartupResult` 状态（已有）+ 后台 goroutine 完成回填，不引入事件通道；
- `EffectiveMcpServer` 完整包装——codexgo 已有 `effectiveMcpServers` 解析，够用。

即：codexgo 需求 4 = **非阻塞启动 + per-server 重试 + 选择性 reconnect**，不含 ChatGPT/
事件流/apps-cache 那部分上游体量。

## 4. 缺失抽象与最小引入

- **McpRuntimeContext / Environment**：上游缓存身份依赖它们；codexgo 需求 2 已**规避**——
  缓存身份用 `EnvironmentID` 字符串 + stdio 指纹（tool_catalog_cache.go 已实现），
  **无需**引入 Environment 弱引用抽象。✅ 不再是阻塞项。
- **启动协调状态**：需求 2 的 `OptionalStartupDeadline`/`BeginFetch`/`PublishIfNewest`
  已就位，直接消费。

## 5. 接口变更（增量、不破坏现有消费方）

```go
// Manager 新增（不改现有方法签名）：
func (m *Manager) Reconcile(ctx, servers map[string]config.McpServerConfig) []ServerStartupResult
    // diff 当前 vs 新 config：新增→启动；移除→关闭；变更（指纹不同）→重连；未变→保留

// startServer 改造（内部）：
//   1. 有 cache.Context 且 CurrentTools() 非空 → 先用缓存工具构造 ManagedClient（非阻塞）
//   2. 后台 goroutine：initialize + listTools → PublishIfNewest → 原子替换 clients[name]
//   3. 失败 → 退避重试（上限），期间缓存目录仍可用

// buildMcpManager 保持返回 *Manager；内部启动改非阻塞，装配不再等慢 server
```

**关键不变量**：`ListAllToolInfos`/`ListAllToolSpecs`（@show/relevance-gate 与渲染在途工作
的消费面）签名与语义**不变**——它们读 `m.clients` 快照；非阻塞启动只是让 `clients` 从
"缓存工具"演进到"live 工具"，读方无感。这是与在途 MCP 渲染工作的安全接缝。

## 6. 增量步骤（每步独立可测可提交）

1. **缓存接线（需求 2 part 2）**：`startServer` 在 initialize 前查 `cache.Context`；
   listTools 成功后 `PublishIfNewest`。先不改同步语义——仅让缓存被填充与读取。
   测试：重启 manager，第二次启动命中缓存（listTools 不再打网络）。
2. **per-server 重试**：`startServer` 失败进退避重试轨（复用需求 1 的 backoff 思路）。
   测试：fake server 首次失败、二次成功 → 最终 ready。
3. **非阻塞启动**：有新鲜缓存时用缓存工具即时构造 client，后台刷新。
   测试：慢启动 server + 有缓存 → 装配立即返回、工具可用、后台就绪后刷新。
4. **选择性 reconnect**：`Reconcile` diff。测试：改一个 server config → 仅该 server 重连，
   其余 client 指针不变（session 不动）。

## 7. 风险

| 风险 | 缓解 |
|---|---|
| 改坏刚打通的 MCP 接线（MCP-WIRING） | 接缝限定在 `ListAllTool*` 之下；每步跑 `internal/cli`/`internal/core` 回归；渲染面 API 不动 |
| 非阻塞启动引入并发（缓存工具→live 工具切换）竞态 | 需求 2 的代际 `PublishIfNewest` + `clients` 加锁原子替换；`-race` 测试 |
| 与在途 `@show`/渲染 commit 合并冲突 | 实施前 rebase feat/opendb-mvp 头；改动集中在 manager.go/mcp_wiring.go，与 tui 渲染文件不重叠 |
| reconnect 期间工具调用落到旧连接 | 重连原子替换 + 调用按 name 解析当前 client；旧连接 graceful close |
| parity 回归（0.136 差分） | forward-synced 登记；startup 面不进 no-auth 差分视野（无 golden 依赖） |

## 8. 验收

- 四步各自单测/集成通过；`go test -race ./internal/mcp/... ./internal/cli/... ./internal/core/...` 绿；
- 既有 parity 套件（CODEX_PARITY_BIN 0.136）不回归；
- DEVIATIONS 登记 forward-synced 条目；VERSION bump 与需求 1/2/3/5 一并至 0.5.0，
  deploy-local 后 user 验证二进制再 push+tag。

## 9. 待 maintainer 确认点

1. 裁剪范围（§3）：不移植 ChatGPT auth / 事件流 / apps-cache——认可？
2. 增量顺序（§6）：先缓存接线→重试→非阻塞→reconnect——认可？
3. `Reconcile` 是否 Stage 1 就要（config 热变更场景），还是仅做非阻塞+重试、reconnect 延后？
   （codexgo 单机 CLI 的 config 热变更频率低，reconnect 可作为步骤 4 视时间取舍。）
