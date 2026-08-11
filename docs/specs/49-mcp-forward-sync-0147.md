# 49 — MCP Forward-Sync (0.137→0.147, Cluster A)

> **DRAFT — pending maintainer approval.** 本 spec 是首个**定向前向同步** spec：
> 不做全量 rebase，只把上游 0.137→0.147 的 MCP 协议演进簇移植进主线（airush
> 抽核依赖，见 ~/airush/docs/codexgo-sync-assessment.md 簇 A / 策略 C）。
> 含两个 §8 级 maintainer 决策点，随本 spec approve 一并生效（见「决策点」节）。

| | |
|---|---|
| **Phase** | 6 — Extensibility（forward-sync 轨） |
| **Status** | Draft |
| **Depends on** | 21 (mcp-client), 26 (mcp wiring), 42 (app-server v2 faces) |
| **Size** | L |
| **Drop-in critical** | ★（wire protocol：版本协商 + schema 保形 + token 序列化互通） |

## 决策点（maintainer 一次性批准，approve 本 spec 即视为批准）

1. **基线引用扩展（ROADMAP §8 触发⑤：外网操作）**：`git -C reference-codex fetch
   --tags && git worktree add ../reference-codex-0147 rust-v0.147.0`——0.136.0 主基线
   与 parity 二进制**不动**，0.147 仅作为移植对照的第二参考（同样不入库，.gitignore 增行）；
2. **DEVIATIONS 新状态 `forward-synced`（§8 触发②的制度化）**：凡因定向同步而领先
   0.136 基线的行为，登记为 `forward-synced (spec 49, target 0.147)` 而非 `review`——
   与上游**未来版本**对齐不算未解释偏差；DEVIATIONS.md 标题补注说明。

## 目标 / Goal

把上游 0.137→0.147 的 MCP 协议演进定向移植进 codexgo 主线（client `internal/mcp` +
server 面 `internal/mcpserver`），使 MCP 面达到 0.147 等价：协议版本 2026-07-28、
启动非阻塞化 + 工具目录缓存、schema 保形、连接可靠性包、OAuth token 刷新。
既有能力（streamable HTTP、tools/list 分页、elicitation）**不重做**。

## 源参考 / Source reference

- `../reference-codex-0147/codex-rs/rmcp-client/src/`（client 行为与可靠性语义的主对照）；
- `../reference-codex-0147/codex-rs/core/src/mcp/`（tool 目录缓存、config 变更重连、工具搜索开关）；
- `../reference-codex-0147/codex-rs/app-server*/`（server 面协议版本与非阻塞启动）；
- 精确文件映射在步骤 1（fetch 后）落到本节的实施附注——上游 crate 在 0.136→0.147
  间有目录重组，fetch 前写死路径必错；
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
   引擎先用**磁盘缓存的工具目录**（`~/.codexgo` 下新缓存文件，格式对齐上游）提供
   工具面，server 就绪后增量刷新；单 server 启动失败/超时不再只是"跳过"而是进入
   重试轨（需求 4）；缓存目录随 tools/list 成功刷新写回；
3. **Schema 保形**（0.139）：工具 schema 透传保留 `oneOf`/`allOf`；超大 schema
   压缩路径保形（不丢关键字段）；golden 用例覆盖嵌套组合子；
4. **连接可靠性包**（0.140/0.145/0.146）：per-server 启动超时（可配置，默认对齐
   上游）；瞬时失败指数退避重试；**config 变更只重连受影响 server**，健康连接
   保持（对照 `internal/cli/mcp_wiring.go` 现状"一次性启动"改造为可增量 reconcile
   的 manager 生命周期）；工具目录跨重连安全复用（重连期间旧目录可用，就绪后原子替换）；
5. **OAuth token 刷新**（0.140/0.145）：`oauth_tokens.go` 增 refresh_token 流
   （过期前/401 后刷新、序列化写回 keyring/文件 blob 保持与上游字节级同形）；
   完整 authorization-code 授权流仍**不做**（见非目标）；
6. **工具搜索默认启用**（0.143）：对照上游确认 codexgo 是否已有对应开关面；有则
   翻默认值 + config 覆盖，无则按上游语义补最小实现（实施时按 0.147 源确认边界，
   若发现是大型独立功能则拆分出去并回报——不静默扩 scope）；
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
