# 49 — MCP Forward-Sync (0.137→0.147, Cluster A)

> **APPROVED** — maintainer approve 2026-08-11（含两决策点：0.147 基线引用扩展、DEVIATIONS forward-synced 状态）。 本 spec 是首个**定向前向同步** spec：
> 不做全量 rebase，只把上游 0.137→0.147 的 MCP 协议演进簇移植进主线（airush
> 抽核依赖，见 ~/airush/docs/codexgo-sync-assessment.md 簇 A / 策略 C）。
> 含两个 §8 级 maintainer 决策点，随本 spec approve 一并生效（见「决策点」节）。

| | |
|---|---|
| **Phase** | 6 — Extensibility（forward-sync 轨） |
| **Status** | Needs 1-5 implemented（2026-08-12；待 user 验证二进制后 push/tag v0.5.0） |
| **Depends on** | 21 (mcp-client), 26 (mcp wiring), 42 (app-server v2 faces) |
| **Size** | L |
| **Drop-in critical** | ★（wire protocol：版本协商 + schema 保形 + token 序列化互通） |

## 决策点（maintainer 已批 2026-08-11）

1. ✅ **基线引用扩展**：`reference-codex-0147`（rust-v0.147.0）worktree 已就位（不入库，
   .gitignore 已加）；0.136.0 主基线与 parity 二进制不动；
2. ✅ **DEVIATIONS 新状态 `forward-synced`**：定向同步领先 0.136 的行为登记为
   `forward-synced (spec 49, target 0.147)`，与上游未来版本对齐不算未解释偏差；
3. ✅ **需求 2 工具目录缓存 = 内存 LRU（方案 a）**：忠实照搬上游 0.147
   `McpToolCatalogCache`（`Arc<Mutex<LruCache>>`，容量 32/TTL 30min，进程内、无磁盘）。
   不做磁盘持久化（那是超上游的自创设计，已否）；
4. ✅ **需求 6 摘出簇 A**：工具暴露策略（eager vs always-defer-behind-tool-search）是路线
   抉择，与协议同步正交，且与本仓在建的 `@show`/relevance-gate 路线可能冲突——本 spec
   **范围收敛为需求 1-5**，需求 6 待「@show vs tool-search」方向定后单独立项。

## 目标 / Goal

把上游 0.137→0.147 的 MCP 协议演进定向移植进 codexgo 主线（client `internal/mcp` +
server 面 `internal/mcpserver`），使 MCP 面达到 0.147 等价：协议版本 2026-07-28、
启动非阻塞化 + 工具目录缓存、schema 保形、连接可靠性包、OAuth token 刷新。
既有能力（streamable HTTP、tools/list 分页、elicitation）**不重做**。

## 源参考 / Source reference

> **步骤1 已校正路径**（初稿的 `core/src/mcp/` 在两棵树均不存在——实际是
> `core/src/mcp.rs` 单文件 + 独立 `codex-mcp` crate）。完整映射见 `49-diff-inventory.md`。

- `../reference-codex-0147/codex-rs/rmcp-client/src/`（client 行为、协议模式、OAuth、重试）；
- `../reference-codex-0147/codex-rs/codex-mcp/src/`（连接管理重构：`connection_manager/{required,
  resources,startup,tool_catalog}.rs` + `tool_catalog_cache.rs` 内存 LRU）；
- `../reference-codex-0147/codex-rs/tools/src/`（`json_schema.rs` schema 保形、`tool_search.rs`）；
- `../reference-codex-0147/codex-rs/mcp-server/src/message_processor.rs`（server 面协议版本回显）；
- MCP 规范修订 2026-07-28（版本协商矩阵权威）；
- 步骤1 差异盘点全文：`docs/specs/49-diff-inventory.md`（含每需求行号级锚点与移植要点）；
- MCP 规范修订 2026-07-28（公开 spec，版本协商矩阵的权威来源）；
- 现状锚点：`internal/mcp/protocol_messages.go:12`（2025-06-18）、
  `internal/mcp/http_transport.go`（session 头/SSE 已有）、`internal/mcp/operations.go:53`
  （分页已有）、`internal/mcp/oauth_tokens.go`（token 层，无刷新流）、
  `internal/cli/mcp_wiring.go`（进程级一次性启动——本 spec 改造对象）。

## 功能需求 / Functional requirements

1. **协议版本协商**：client 请求 2026-07-28，按服务端能力降级（2026-07-28 →
   2025-06-18 → 2025-03-26 协商矩阵）；`mcpserver` 面 `defaultProtocolVersion`
   同步升级并接受旧版本 client；版本常量收敛到单一定义点；
2. **非阻塞启动 + 工具目录缓存**（0.147）：MCP server 启动移出装配期关键路径——
   引擎先用**缓存的工具目录**提供工具面，server 就绪后增量刷新；单 server 启动
   失败/超时不再只是"跳过"而是进入重试轨（需求 4）；缓存随 tools/list 成功刷新更新。
   **⚠ 步骤1 校正（与源码不符，含 maintainer 决策点）**：上游 0.147 的工具目录缓存
   是**进程内内存 LRU**（`codex-mcp/src/tool_catalog_cache.rs`，`Arc<Mutex<LruCache>>`，
   容量 32 / TTL 30min），**无磁盘文件、无 JSON 落盘**；本 spec 初稿写的"`~/.codexgo`
   磁盘缓存文件 + JSON 格式对齐上游"**没有可抄的上游依据**。CLI 每次是新进程，内存缓存
   对单次调用无收益——"启动移出关键路径"在内存缓存语义下**只对同进程内多轮 turn 生效**。
   决策：（a）忠实移植内存 LRU（等价上游，但对 CLI 单次调用几乎无感）；（b）改磁盘
   持久化（超出"对齐上游"，触规则5硬门槛，需 maintainer 批准）——**待 user 决策，未决前
   本需求按 (a) 收敛，"非阻塞启动"仅指 manager 生命周期不阻塞装配**；
3. **Schema 保形**（0.139）：工具 schema 透传保留 `oneOf`/`allOf`；超大 schema
   压缩路径保形（不丢关键字段）；golden 用例覆盖嵌套组合子；
4. **连接可靠性包**（0.140/0.145/0.146）：per-server 启动超时（可配置，默认对齐
   上游）；瞬时失败指数退避重试；**config 变更只重连受影响 server**，健康连接
   保持（对照 `internal/cli/mcp_wiring.go` 现状"一次性启动"改造为可增量 reconcile
   的 manager 生命周期）；工具目录跨重连安全复用（重连期间旧目录可用，就绪后原子替换）；
5. **OAuth token 刷新**（0.140/0.145）：`oauth_tokens.go` 增 refresh_token 流
   （过期前/401 后刷新、序列化写回 keyring/文件 blob 保持与上游字节级同形）；
   完整 authorization-code 授权流仍**不做**（见非目标）；
6. ~~**工具搜索默认启用**~~ **——已摘出本 spec（见决策点 4）**。步骤1 深挖发现这不是
   "一行翻转"：codexgo live 装配走 eager 暴露（`cli/assembly.go:272`/`appserver/mcp_tools.go:20`
   调 `ListAllToolInfos`），deferred 机制（`ListAllToolSpecs`/`McpToolToDeferredResponsesApiTool`）
   已移植但零 live 调用，flag 声明却无消费方。上游 0.147 走 always-defer-behind-tool-search，
   而本仓在建 `@show`/relevance-gate 路线（commit 28f45de/1de5f98）——两条路线抉择属 §8
   维护者决策，与协议同步正交，单独立项，不在本 spec；
7. **簿记**：每项与 0.136 基线的行为差登记 DEVIATIONS `forward-synced`；
   `docs/PARITY.md` 增 MCP 面基准说明；`docs/STATUS.md` 增 spec 49 行。

## 验收方案 / Acceptance criteria

- 版本协商矩阵单测（3×3：client 版本 × server 版本，含拒绝路径）；
- fake MCP server 集成测试：慢启动（缓存目录先行可用）、启动超时、瞬时失败重试、
  config 增/删/改 server 后的选择性重连（健康连接的 session id 不变即为证）、
  重连期间工具调用不中断；
- oneOf/allOf schema roundtrip golden（fake server 提供嵌套组合子 schema）；
- OAuth 刷新：过期 token 自动刷新 + 写回序列化与上游格式字节级对照（复用
  paritytest canonicalizer）；401 触发刷新后重试一次；
- 既有 parity 套件（`CODEX_PARITY_BIN` 0.136 二进制）保持绿——MCP forward-synced
  面若进差分视野则按 DEVIATIONS 登记加 mask，mask 必须逐条对应登记项；
- `gofmt` / `go vet` / `go test -race ./...` 全绿（CI 既有闸门）；
- VERSION minor bump（0.4.12 → 0.5.0），走 deploy-local.sh 流程：**USER 验证已部署
  二进制后才 push/tag**（既有 release 纪律，§8 触发⑤）。

## 风险与难点 / Risks

- **in-house client vs 上游 rmcp 3.0**：移植的是**语义**不是代码——上游 SDK 升级
  的 changelog 面宽，需先做 0.136→0.147 的 rmcp 行为 diff 清单再动手（步骤 1 产出物）；
- **与 feat/opendb-mvp 在途 MCP 渲染工作的合并点**：近期 commit 都在 MCP 工具渲染
  链路；本 spec 改的是 transport/manager 层，接缝在 `Manager.ListAllToolInfos` ——
  实施前 rebase 到分支头并保持渲染面 API 不动；
- **manager 生命周期改造是最大单点**（一次性启动 → 可 reconcile）：现有"孤岛接线"
  刚打通（MCP-WIRING），改坏了影响全部 MCP 功能；靠 fake-server 集成电池 + 渲染面
  回归用例兜底；
- **工具目录缓存引入新持久化文件**：格式必须对齐上游（drop-in ★），先从 0.147 源
  确认文件名/路径/格式再写；
- **需求 6 的边界不确定性**：工具搜索在上游的实现规模 fetch 前无法确认，已在需求内
  写明"发现超界即拆分回报"的熔断。

## 非目标 / Non-goals

- 全量 rebase 到 0.147（其余簇 B/C/D/E/F 各有后续 spec；TUI/桌面/账户面不同步）；
- OAuth authorization-code 完整授权流（浏览器回调/PKCE/动态注册）——上游 0.140 有，
  但簇 A 只取"凭据序列化刷新"；完整流列簇 F 或独立 spec；
- in-process transport、`roots/list`、`sampling/createMessage`（spec 21 遗留，非簇 A 范围）；
- airush 侧租户注入/服务化（airush spec-1.9 的事，本仓保持单机产品语义）；
- MCP 工具渲染/TUI 展示面变更（在途工作，互不侵入）。

## 实施 changelog（approve 后追加，不改上文）

**2026-08-12 — 需求 1-5 实施完成**（commits `3a2d6d7`…`ae617d2`，分支 `feat/opendb-mvp`）：

| 需求 | 落点 | 备注 |
|---|---|---|
| 1 协议协商 | `internal/mcp/protocol_mode.go` + client/server 两侧接线 | 版本常量单点；协商矩阵单测。**回归修复**：严格协商把仍停在首个稳定修订 2024-11-05 的服务端判为致命，打断了真实 gaussdb 插件（`TestBuildMcpManagerLaunchesGaussdbPlugin`）；现接受该修订（codexgo 仍只 offer 2025-06-18/2026-07-28），未知版本仍致命 |
| 2 工具目录缓存 | `internal/mcp/tool_catalog_cache.go` + `startServer` 接线 | 内存 LRU(32)/TTL 30min，身份=server 名+EnvironmentID+stdio 指纹；代际发布 `BeginFetch`/`PublishIfNewest`（决策点 3a：无磁盘） |
| 3 schema 保形 | `internal/tools` schema 透传 | oneOf/allOf 保留 + golden |
| 4 步骤 1-2 | 缓存接线 + per-server 启动重试 | 退避 250ms→2s 上限、3 次；失败不再"跳过即永久" |
| 4 步骤 3 非阻塞启动 | `Manager.startInBackground` / `resolvePending` / `clientByName` | 有新鲜缓存目录 → 立即以缓存工具挂占位 client（`StartupDeferred`）+ 后台建连；工具调用等 pending 落地；后台失败即摘除该 server；`Shutdown` 取消并等待在途启动（竞态中落地的连接被关闭）。占位重新套用当前工具过滤（缓存身份不含 enabled/disabled 列表） |
| 4 步骤 4 选择性 reconnect | `Manager.Reconcile` | 增→启、删→关、改→重连（新连接就绪后再换下旧连接，旧目录在窗口内仍可调用）、未变→连接与在途调用不动；被 Reconcile 取代的后台启动自带 cancel 并在 resolve 时判定"已非当前"而不安装 |
| 5 OAuth 刷新 | `internal/mcp/oauth_refresh.go` | 过期前/401 后刷新 + 序列化写回 |

**裁剪**（见 `49-need4-manager-lifecycle-design.md` §3，未移植）：上游 `McpStartupUpdateEvent`
进度事件流、ChatGPT auth provider 与 apps-server 专属缓存——codexgo 面向非 OpenAI 后端且
CLI 装配不消费该事件流。

**簿记**：DEVIATIONS 增 `49 MCP forward-sync (cluster A)` 行（状态 `forward-synced`，
并在表头说明该状态语义）；`docs/PARITY.md` 增「MCP surface baseline」节说明该面基线为
0.147、当前 parity 差分不涉（harness 不接 MCP server，故无需 mask）；`docs/STATUS.md` 增 49 行。

**验证**：`gofmt`/`go vet` 全绿；`go test -race ./internal/mcp/...` 全绿（新增
`manager_nonblocking_test.go` 6 例 + `manager_reconcile_test.go` 7 例）；`internal/cli`
的 gaussdb 插件端到端用例由红转绿。全量 `go test -race ./...` 余两处**环境性、与本 spec 无关**
的失败（已实证在 HEAD 同样失败）：`internal/cli` skills 用例读取宿主机真实
`~/.agents/skills`；`internal/uds` 在 macOS 沙箱 TMPDIR 下 socket 路径超 `sun_path` 上限。

**⚠ parity 闸门受阻（非本 spec 引起）**：验收要求"既有 parity 套件（0.136 二进制）保持绿"，
但宿主机 `codex` 已升到 **0.147.0**（standalone 缓存最老仅 0.144.1，无 0.136 可用）。对 0.147
跑差分套件报 13 例失败，全部是版本漂移（0.147 新增 `cache_write_input_tokens` 用量字段、
`tool_search_output.id` 等），**已实证与本 spec 无关**：同样 13 例在本簇工作落地前的
`deff240` 上逐例复现。恢复该闸门需装回 0.136 二进制并指向 `CODEX_PARITY_BIN`；详见
`docs/PARITY.md` 顶部「Baseline binary drift」节。

**部署后验证（2026-08-12，Mac 宿主机 `/opt/homebrew/bin/codexgo` v0.5.0 `e2b2ad6`）**：
以临时 `-c mcp_servers.gaussdb.command=…` 覆盖接入真实 gaussdb 插件跑一轮 exec，装配无 MCP
启动告警，模型完整列出该 server 的 38 个工具。**地面真值另取**（`scripts/mac-probe-mcp-stdio.py`
直接对二进制走 JSON-RPC initialize+tools/list，不经模型）：服务端自报
`protocolVersion=2024-11-05`、`codexgo-db-gaussdb v0.9.16`、38 个工具，与模型所述集合逐名一致。
该 2024-11-05 正是需求 1 严格协商此前判死的修订——本次修复为承重项，非锦上添花。

**待办**：VERSION 已 0.4.12→0.5.0；`deploy-local.sh` 已部署且验证通过 → 待 user 指示后 push/tag。
