# Spec 49 步骤1 — 行为差异盘点（0.136 → 0.147 MCP 演进簇）

> 本文件是 spec 49（`docs/specs/49-mcp-forward-sync-0147.md`）步骤1的产出物：
> 对上游 rust-v0.136.0（基线）→ rust-v0.147.0（目标）之间 MCP 协议演进簇做
> **只读行为差异盘点**，不改任何 `.go` 代码。逐条盘点 6 条功能需求 + 需求6 规模裁定。
>
> 参考树：
> - 基线 0.136：`/Users/sqlrush/codexgo/reference-codex/codex-rs`
> - 目标 0.147：`/Users/sqlrush/codexgo/reference-codex-0147/codex-rs`
> - codexgo 现状：`/Users/sqlrush/codexgo/internal/{mcp,mcpserver,cli}`
>
> 术语：所有引用行号以盘点时快照为准；移植实施前须以分支头复核。

## 上游文件 → codexgo 目标文件 映射表

> **重大结构发现**：0.136→0.147 之间 MCP 连接管理与工具目录从单文件
> `codex-mcp/src/connection_manager.rs` 重构为 `codex-mcp/src/connection_manager/{required,resources,startup,tool_catalog}.rs`
> 并新增 `catalog.rs`、`tool_catalog_cache.rs`、`pagination.rs`、`binding.rs`、
> `runtime.rs`、`client_capabilities.rs`。spec 源参考节写的 `core/src/mcp/` 路径在两棵树
> 均**不存在**（实际在 `core/src/mcp.rs` 单文件 + `codex-mcp` crate）；写死路径必错，已实地校正。

| 需求 | 0.147 上游位置 | 0.136 基线对应物 | codexgo 目标文件 |
|---|---|---|---|
| 1 协议版本协商 | `rmcp-client/src/protocol_mode.rs`（新增 `McpProtocolMode`）；`mcp-server/src/message_processor.rs:278`（回显） | 无 protocol_mode.rs；`rmcp_client.rs` 硬编码 2025-06-18 | `internal/mcp/protocol_messages.go:12`；`internal/mcp/managed_client.go:112`；`internal/mcpserver/processor.go:166-178,315` |
| 2 非阻塞启动 + 工具目录缓存 | `codex-mcp/src/tool_catalog_cache.rs`（进程内 LRU，非磁盘）；`codex-mcp/src/connection_manager/{startup,tool_catalog}.rs` | 均不存在（单文件 `connection_manager.rs`，无缓存） | `internal/cli/mcp_wiring.go:43`；`internal/mcp/manager.go`（无缓存概念） |
| 3 Schema 保形 | `tools/src/json_schema.rs`（`SCHEMA_CHILD_KEYS` 4键 + `COMPOSITION_SCHEMA_KEYS`） | 同文件，`SCHEMA_CHILD_KEYS` 仅 `["items","anyOf"]` | `internal/tools/json_schema_sanitize.go:16,21-22`；`json_schema_lower.go`；`json_schema.go:143` |
| 4 连接可靠性包 | `codex-mcp/src/connection_manager.rs:166`（`new(previous,...)` 复用）；`rmcp_client.rs:94,264`（退避）；`rmcp-client/src/streamable_http_retry.rs`（新增） | `new` 无 `previous`；无 reconnect/backoff/retry 文件 | `internal/cli/mcp_wiring.go:43`；`internal/mcp/manager.go`；`internal/mcp/managed_client.go:15-16` |
| 5 OAuth token 刷新 | `rmcp-client/src/oauth.rs:81,781`；`rmcp-client/src/oauth/{refresh_transaction,refresh_lock,store_lock}.rs`（0.145 新增事务锁） | 同 `StoredOAuthTokens` 格式；`refresh_if_needed` 内联 oauth.rs，无锁 | `internal/mcp/oauth_tokens.go:126`；`internal/mcp/oauth_store.go:34,206`（缺网络刷新+401触发） |
| 6 工具搜索默认启用 | `tools/src/tool_search.rs`+handlers（~931行）；`features/src/lib.rs:1155,1159`（both Removed，always-defer=true） | `tools/src/tool_discovery.rs`+handlers（~537行）；always-defer=UnderDev/false | `internal/tools/{tool_search_spec,mcp_search,bm25}.go`（已移植）；`internal/features/feature.go:352-353`（仅353待翻） |

---

## 需求1 — 协议版本协商

### 0.147 实现位置

- 客户端策略：`rmcp-client/src/protocol_mode.rs` —— 新增 `enum McpProtocolMode { Legacy, V20260728 }`。
  - `preferred_protocol_version()`：`Legacy → ProtocolVersion::V_2025_06_18`；`V20260728 → V_2026_07_28`。
  - `client_lifecycle()`：`Legacy → ClientLifecycleMode::Initialize`；`V20260728 → ClientLifecycleMode::Auto { preferred_versions: [V_2026_07_28], legacy_version: Some(V_2025_06_18) }`。**协商矩阵就是这里**：优先 2026-07-28，服务端不支持则降级 2025-06-18。
  - `stdio_mode(requested_version)`：读环境变量 `CODEX_MCP_PROTOCOL_VERSION`，只接受 `"2026-07-28"`，未知标记返回 `InvalidInput` 错误。
- 客户端接线：`rmcp-client/src/rmcp_client.rs:353` 字段 `protocol_mode: McpProtocolMode`；`:408 new_stdio_client_with_protocol_mode`、`:487 new_streamable_http_client_with_protocol_mode` 两条构造路径把 mode 注入 SDK lifecycle；旧构造函数默认 `McpProtocolMode::Legacy`（`:379`/`:401`/`:480`）。
- 服务端（codex 自身作为 MCP server）：`mcp-server/src/message_processor.rs:278` `.with_protocol_version(params.protocol_version)` —— **回显客户端请求的版本**，不做独立协商，与 codexgo 现状同构。
- 降级顺序（权威）：`2026-07-28 → 2025-06-18`（codex 代码只落两级）。规范 `2025-03-26` 是 MCP 协议地板，由 rmcp SDK 的 `ProtocolVersion` 兜底，**codex 层不出现**（`grep 2025-03-26 rmcp-client/src mcp-types/src` 无命中）。spec 49 写的三级矩阵中第三级需理解为「SDK 内建下限」，本仓移植时按需实现到 2025-06-18 即达 0.147 等价。

### 0.136 基线对应物

- `rmcp-client/src/protocol_mode.rs` **在 0.136 不存在**（已 `ls` 确认 NOT FOUND）——即**新增**。
- 0.136 客户端硬编码单版本 2025-06-18（`rmcp_client.rs:375` 注释引 `2025-06-18/lifecycle`），无 `McpProtocolMode`、无 `ClientLifecycleMode::Auto` 降级、无 stdio 版本环境开关。
- 行为差：0.136 只会宣告 2025-06-18；0.147 可宣告 2026-07-28 并在服务端老旧时自动回退。

### codexgo 现状锚点

- 客户端常量：`internal/mcp/protocol_messages.go:12` `const MCPProtocolVersion = "2025-06-18"`（单一定义点，已收敛）。
- 客户端发送：`internal/mcp/managed_client.go:112` `ProtocolVersion: MCPProtocolVersion`（initialize 里写死）。
- 缺省填充：`internal/mcp/operations.go:28-29`（空则填 `MCPProtocolVersion`）。
- 服务端常量：`internal/mcpserver/processor.go:315` `const defaultProtocolVersion = "2025-06-18"`。
- 服务端回显：`internal/mcpserver/processor.go:166-178`（`protocolVersion := req.ProtocolVersion`，空则 `defaultProtocolVersion`，原样回显）——与 0.147 `mcp-server` 回显语义一致，**无需引入协商逻辑，只需升级常量并接受旧版 client**。
- 测试基线遍布 `2025-06-18` 字面量（`client_test.go`、`fake_transport_test.go`、`serve_test.go`、`processor_test.go:19-20` 等）。

### 移植要点

1. 客户端引入等价 `McpProtocolMode`（Go 侧可为 `type ProtocolMode` + 首选版本函数），`MCPProtocolVersion` 升级为「首选 2026-07-28」并保留 2025-06-18 作 legacy 常量；协商降级顺序 `2026-07-28 → 2025-06-18`。
2. codexgo 无 rmcp SDK，无 `ClientLifecycleMode::Auto` 现成件——须在 initialize 结果解析处**手工实现降级**：请求 2026-07-28，若服务端回显更旧版本则接受并按其能力工作（不得因版本不等而报错）。
3. 服务端 `defaultProtocolVersion` 同步升级为 2026-07-28，但**继续回显 client 请求的版本**（保持接受旧 client），不主动拒绝 2025-06-18/2025-03-26。
4. 版本常量收敛到单一定义点（现已在 `protocol_messages.go:12` 与 `processor.go:315` 两处——移植时应合并到一处，消除双定义）。
5. stdio 版本环境开关 `CODEX_MCP_PROTOCOL_VERSION` 为可选增强，非硬需求；如实现须对齐上游只接受 `"2026-07-28"` 且未知值 fail-closed。

---

## 需求2 — 非阻塞启动 + 工具目录缓存

### 0.147 实现位置

- **缓存本体**：`codex-mcp/src/tool_catalog_cache.rs`（全文已读）——`pub struct McpToolCatalogCache`。
  - 结构：`Arc<Mutex<LruCache<ToolCatalogIdentity, Arc<ToolCatalogCacheEntry>>>>`。
  - 常量：`TOOL_CATALOG_CACHE_CAPACITY = 32`（LRU 容量）；`TOOL_CATALOG_CACHE_TTL = Duration::from_secs(30 * 60)`（30 分钟）。
  - 快照：`ToolCatalogSnapshot { tools: Vec<ToolInfo>, published_at: Instant }`；`current_tools()` 只在 `published_at.elapsed() <= TTL` 时返回。
  - 发布逻辑 `publish_if_newest(ticket, tools)`：用 generation 单调序号防旧结果覆盖新结果；发布前**清洗** `tool.namespace_description = None`、`tool.tool.annotations = None`（这两项属单条活连接，不得跨 session 复用）；`disabled_by_server` 时不缓存。
  - 身份键 `ToolCatalogIdentity`：`server_name` + transport 指纹（**仅 Stdio** 走缓存；HTTP catalog 因需已解析 auth 身份而 `return None` 不缓存）+ `Weak<Environment>` 指针 + stdio fallback cwd。Stdio 指纹用 **SHA1** 摘要 `(command, args, env, env_vars, cwd, environment_id, elicitation_capability, mcp_extensions)` 的 `serde_json` 序列化字节，再叠加各 env_var 名与其值哈希。
- **非阻塞启动接线**：`codex-mcp/src/connection_manager/startup.rs`（118 行）、`connection_manager/tool_catalog.rs`（432 行）。
  - `optional_startup_deadline(default_deadline)`：若已有未过期快照或被 server 禁用，直接用默认 deadline；否则记住首个 deadline —— 让引擎**先用缓存工具面**，不阻塞在慢启动 server 上。
  - `capture_binding_with_metadata`（tool_catalog.rs:171）：`has_cached_tools` 为真且非「必须等启动」时，用缓存目录立即成面；否则等 startup deadline。
- **客户端侧缓存挂载**：`codex-mcp/src/rmcp_client.rs:280` 字段 `tool_catalog_cache_context: Option<McpToolCatalogCacheContext>`；`:319 begin_fetch` 取 ticket，tools/list 成功后 `publish_if_newest` 写回；`has_cached_tools()` 供启动判定。
- **管理器持有**：`core/src/mcp.rs:64` `McpManager.tool_catalog_cache: McpToolCatalogCache`，`:94 tool_catalog_cache()` 暴露，进程级共享。

### 0.136 基线对应物

- `tool_catalog_cache.rs`、`connection_manager/{startup,tool_catalog}.rs` 在 0.136 **均不存在**（0.136 `codex-mcp/src` 只有单文件 `connection_manager.rs`，无 `catalog.rs`/`tool_catalog_cache.rs`/`pagination.rs`）——即**全新增**。
- 0.136 行为：MCP server 在装配期**同步**启动，`connection_manager.rs` 一次性 init，无工具目录缓存、无「先缓存后刷新」的非阻塞路径；单 server 慢/失败即在装配期表现为跳过。

### codexgo 现状锚点

- 启动接线：`internal/cli/mcp_wiring.go:43 buildMcpManager`（全文已读）——**进程级一次性启动**：`mcp.NewManager` 同步拉起全部 server，`ready==0` 返回 nil。注释明写「Servers run for the process lifetime」「no explicit shutdown」——这正是 spec 49 要改造的对象。
- 管理器：`internal/mcp/manager.go:75 NewManager`、`:132 startServer`、`:215 ListAllToolInfos`（渲染面接缝，须保持 API 不动）。
- **codexgo 当前无任何工具目录缓存**（`grep cache internal/mcp/manager.go managed_client.go` 无命中）；无 `has_cached_tools` 概念；tools/list 结果不缓存。

### 移植要点（含缓存事实核正）

1. **★关键事实核正（最大意外发现）**：0.147 的工具目录缓存是**进程内内存 LRU**（`Arc<Mutex<LruCache>>` + `tokio Instant` TTL），**不是磁盘文件**。全树 `grep` 未发现任何将 tool catalog 写入 `~/.codex`/`codex_home`/`.json` 的持久化路径。**spec 49 需求2 所述「`~/.codexgo` 下新缓存文件 / 磁盘缓存 / JSON 格式对齐上游」与 0.147 实际实现不符**——不存在可抄的磁盘路径与 JSON 样例。
2. 因此本条无法给出「确切磁盘路径 + JSON 格式样例」（源码中无此物）。**建议回 maintainer 决策**：(a) 按 0.147 真实语义实现**进程内缓存**（等价、drop-in 风险最低）；或 (b) 若 codexgo 确需跨进程持久化（因 CLI 每次调用是新进程，内存缓存对单次 CLI 无收益），则属**超出「对齐上游」范围的自主设计**，需 spec 修订与 §8 决策，不能静默按上游实现。此为架构/scope 判断，触碰规则5硬门槛①。
3. 若走进程内方案，抄以下常量：LRU 容量 `32`、TTL `30 分钟`；发布用单调 generation 防乱序；发布前清洗 `namespace_description` 与 `annotations` 两字段（避免跨 session 泄漏活连接私有元数据）。
4. 缓存身份键：仅 Stdio transport 入缓存，HTTP 不缓存；指纹覆盖 command/args/env/env_vars/cwd/environment_id/client capabilities，任一变更即失效——移植时 Go 侧可用同集合的稳定序列化 + 哈希实现。
5. 非阻塞化：`buildMcpManager` 从「装配期同步全量启动」改为「先用缓存目录成面 + 后台就绪刷新」；单 server 超时进重试轨（需求4），不再只是跳过。

---

## 需求3 — Schema 保形（oneOf / allOf）

### 0.147 实现位置

- 唯一权威文件：`tools/src/json_schema.rs`。关键常量/函数（行号为 0.147 快照）：
  - `SCHEMA_CHILD_KEYS: [&str; 4] = ["items", "anyOf", "oneOf", "allOf"]`（line 9）。
  - `COMPOSITION_SCHEMA_KEYS: [&str; 3] = ["anyOf", "oneOf", "allOf"]`（line 10，**新增**）。
  - `struct JsonSchema` 新增 `oneOf`（line 66）、`allOf`（line 68）字段（与既有 `anyOf` 并列）。
  - 压缩预算：`MAX_COMPACT_TOOL_SCHEMA_BYTES = 5_000`（line 222）、`MAX_COMPACT_TOOL_SCHEMA_DEPTH = 3`（line 223）。
  - 压缩流水线 `compact_large_tool_schema`（line 229）依次 `drop_schema_definitions` → `collapse_deep_schema_objects_from_root`，遍历 `SCHEMA_CHILD_KEYS`（含 oneOf/allOf）。
  - `sanitize_json_schema` 文档串（line 460）："Preserves explicit `anyOf`, `oneOf`, and `allOf`."

### 0.136 基线对应物

- 同文件 `tools/src/json_schema.rs`（0.136），差异逐项：
  - `SCHEMA_CHILD_KEYS: [&str; 2] = ["items", "anyOf"]`（line 9）——**无 oneOf/allOf**。
  - **无** `COMPOSITION_SCHEMA_KEYS`。
  - `struct JsonSchema` 只有 `anyOf`（line 59），无 oneOf/allOf 字段。
  - `MAX_COMPACT_TOOL_SCHEMA_BYTES = 4_000`（line 192）、`MAX_COMPACT_TOOL_SCHEMA_DEPTH = 2`（line 193）。
  - 文档串（line 402）："Preserves explicit `anyOf`."
- 即：0.139 的改动就是把 anyOf 的保形逻辑扩展到 oneOf/allOf，并把压缩预算从 4000B/深度2 放宽到 5000B/深度3。

### codexgo 现状锚点

- codexgo 已移植了 0.136 版此文件，散在三处，且**逐字对应 0.136**：
  - `internal/tools/json_schema_sanitize.go:16` `var schemaChildKeys = [2]string{"items", "anyOf"}`——缺 oneOf/allOf。
  - `internal/tools/json_schema_sanitize.go:21-22` `maxCompactToolSchemaBytes = 4_000`、`maxCompactToolSchemaDepth = 2`——旧值。
  - **无** compositionSchemaKeys 等价物（`grep composition` 无命中）。
  - `internal/tools/json_schema.go:143` `struct JsonSchema` 只有 `AnyOf []JsonSchema`——缺 `OneOf`/`AllOf` 字段。
  - `internal/tools/json_schema_lower.go:6,46-63` sanitize 只识别 `anyOf`/$ref（line 6 注释 "preserves anyOf / $ref / reachable definitions"）。
- 底层透传无损：`protocol.Tool.InputSchema` 是 `json.RawMessage`，wire 层不丢 oneOf/allOf；丢失点仅在 sanitize/compact 遍历不覆盖这两个键时（未被遍历的子树可能在 compact 时被折叠或在类型推断时被漏判）。

### 移植要点

1. `schemaChildKeys` 由 `["items","anyOf"]` 改为 `["items","anyOf","oneOf","allOf"]`（对齐 0.147 `SCHEMA_CHILD_KEYS`）。
2. 新增 `compositionSchemaKeys = ["anyOf","oneOf","allOf"]`（对齐 `COMPOSITION_SCHEMA_KEYS`），并在 `json_schema_lower.go` 的类型推断分支（现只判 `hasAnyOf`）扩展为 oneOf/allOf 同等处理（`len(schemaTypes)==0 && (hasRef || 任一 composition)` 时不误清空）。
3. `JsonSchema` 结构体加 `OneOf []JsonSchema` / `AllOf []JsonSchema` 字段及其 marshal（对齐 json_schema.go:177 的 `AnyOf` 处理）。
4. 压缩预算数字抄 0.147：`maxCompactToolSchemaBytes = 5_000`、`maxCompactToolSchemaDepth = 3`。
5. golden 用例：fake server 提供含嵌套 oneOf/allOf 的 inputSchema，断言 roundtrip 后组合子键与其子 schema 均保留、且超预算时 compact 不吞掉 composition 关键字。此条边界清晰、纯移植，风险低。

---

## 需求4 — 连接可靠性包（超时 / 退避重试 / 选择性重连）

### 0.147 实现位置

- **per-server 启动超时**：`codex-mcp/src/rmcp_client.rs:91` `DEFAULT_STARTUP_TIMEOUT = Duration::from_secs(30)`；`:92` `DEFAULT_TOOL_TIMEOUT = Duration::from_secs(300)`。per-server 覆盖：config `startup_timeout_sec`（`connection_manager/startup.rs:108-110` 缺省回落 `DEFAULT_STARTUP_TIMEOUT`；超时错误信息提示改 config.toml `[mcp_servers.NAME].startup_timeout_sec`）。
- **指数退避重连**：`codex-mcp/src/rmcp_client.rs`
  - 常量 `CODEX_APPS_RECONNECT_INITIAL_BACKOFF = 1s`（:94）、`CODEX_APPS_RECONNECT_MAX_BACKOFF = 30s`（:95）。
  - `codex_apps_reconnect_backoff(consecutive_failures)`（:264）：`exponent = min(failures-1, 5)`；`1s * (1<<exponent)`，上限 30s（即 1→2→4→8→16→30…s）。
  - `reconnect_in_background`（:199）：`retry_not_before` 时间闸门 + `consecutive_failures` 计数，失败后设下次可重试时刻。
  - `reconnect_failed_startup`（:513）：仅当 startup 已完成且 `client()` 为 `Failed` 时触发后台重连。
- **瞬时失败重试（streamable HTTP）**：`rmcp-client/src/streamable_http_retry.rs`（**新增文件**）——`STREAMABLE_HTTP_RETRY_DELAYS_MS = [250, 1_000]`（:23，2 次重试，250ms/1000ms）；`sleep_with_retry_deadline` 受 deadline 约束。
- **★config 变更选择性重连（保健康连接）**：`codex-mcp/src/connection_manager.rs:166` `McpConnectionSet::new(previous: Option<&Self>, ...)`。
  - 逐 server 判定（:338-363）：若 `previous.servers.get(name)` 存在，且 `connection.reusable_client(&connection_identity)` 满足「相同身份 + `catalog_item_limit` 相同 + `protocol_mode` 相同」，则 `Arc::clone(previous_view.connection)` **原地复用健康连接**（`reused_ready.push`，session/连接不变），`continue`；否则起新连接。
  - 复用前置门槛（:210）：`reusable_previous` 要求 previous 非空且 elicitation router 可复用。
- **工具目录跨重连安全复用**：由需求2 的 `McpToolCatalogCache`（TTL 30min）承担——重连期间旧快照仍在 TTL 内可用，新 tools/list 就绪后 `publish_if_newest` 原子替换（generation 单调防旧覆盖新）。

### 0.136 基线对应物

- `McpConnectionSet::new`（0.136 `connection_manager.rs:213`）签名**无 `previous` 参数**（已读，首参为 `mcp_servers: &HashMap<...>`）——每次全新构建，无健康连接复用、无选择性重连。
- **无** `reconnect_in_background` / `reconnect_failed_startup` / `CODEX_APPS_RECONNECT_INITIAL_BACKOFF`（`grep` 全无命中）——无退避重连轨。
- **无** `streamable_http_retry.rs`（0.136 `rmcp-client/src` 不含此文件）——无瞬时失败重试。
- 仅 `DEFAULT_STARTUP_TIMEOUT = 30s`（`rmcp_client.rs:76`）与 `startup_timeout_sec` config 已存在——超时值本身 0.136→0.147 **未变**。

### codexgo 现状锚点

- 一次性启动：`internal/cli/mcp_wiring.go:43 buildMcpManager`（同步全量拉起，失败即跳过，无重试、无 reconcile）。
- 管理器：`internal/mcp/manager.go:75 NewManager`（一次构建）、`:304 Shutdown`、`:132 startServer`——**无 `previous`/reconcile/reconnect 概念**（`grep reconnect|reconcile internal/mcp internal/cli` 全无命中）。
- 超时缺省：`internal/mcp/managed_client.go:15` `DefaultStartupTimeout = 30 * time.Second`（**与上游一致**）；`:16` `DefaultToolTimeout = 120 * time.Second`（**与上游 300s 不一致**）。per-server 覆盖 `startupTimeoutFor(cfg)`/`toolTimeoutFor(cfg)` 已存在（`manager.go:147`）。
- 无任何退避/重试常量；无 streamable HTTP 重试。

### 移植要点

1. **manager 生命周期改造（最大单点，spec 风险已列）**：给 `Manager` 增「可 reconcile」入口——接收新旧 server 配置，逐 server diff：身份不变的复用现有 `ManagedClient`（保持其连接/session），新增的起、删除的关；仅受影响 server 重连。对齐上游 `McpConnectionSet::new(previous, ...)` 的复用判定（身份 + tool 相关参数 + protocol_mode 相等才复用）。渲染面接缝 `ListAllToolInfos`（manager.go:215）API 保持不动。
2. 退避重连：抄常量 initial `1s`、max `30s`，指数式 `1s * (1<<min(failures-1,5))` 封顶 30s；后台重连带 `retry_not_before` 闸门与连续失败计数。
3. 瞬时失败重试：streamable HTTP 抄 `[250, 1000]ms` 两次重试，受整体 deadline 约束。
4. 超时缺省：startup 维持 30s（已对齐）；**建议同步把 `DefaultToolTimeout` 由 120s 升至 300s** 以对齐上游（低风险实现细节，进度报告说明即可；若视为行为契约变更再升级为决策）。
5. 工具目录跨重连复用依赖需求2 的缓存 TTL + 原子替换语义，两需求须一并落地方能满足验收「重连期间工具调用不中断」。

---

## 需求5 — OAuth token 刷新

### 0.147 实现位置

- 序列化格式（★drop-in 字节级对照对象）：`rmcp-client/src/oauth.rs:81` `struct StoredOAuthTokens { server_name, url, client_id, token_response: WrappedOAuthTokenResponse, expires_at: Option<u64> }`（5 字段，`expires_at` 为 `#[serde(default)]`，epoch 毫秒）。
- 刷新判定：`oauth.rs:78` `REFRESH_SKEW_MILLIS: u64 = 30_000`；`:781 token_needs_refresh(expires_at)`（`now + 30_000ms >= expires_at` 即需刷新）；`refresh_expires_in_from_timestamp`（:173）从持久化 `expires_at` 反推 `expires_in`。
- **刷新执行（0.145 重构，新增子系统）**：`rmcp-client/src/oauth/` 子目录（**0.136 无此目录**）——
  - `oauth/refresh_transaction.rs:33 refresh_if_needed()` → `:39 refresh_if_needed_in(store, REFRESH_REQUEST_TIMEOUT)`；常量 `REFRESH_REQUEST_TIMEOUT = Duration::from_secs(45)`（:30）。
  - `oauth/refresh_lock.rs`（`RefreshCredentialLock`）、`oauth/store_lock.rs`、`oauth/resolved_store.rs`：把刷新做成**带 keyring 事务锁**的临界区——预锁快照只作 hint，锁内重读为权威，避免并发重复刷新与写回竞争。
- 触发点：`oauth.rs:164 oauth_tokens_are_usable` 在 `token_needs_refresh` 为真时要求存在有效 refresh_token；连接建立/调用前经 `refresh_if_needed` 拉新 access_token 并 `save`。

### 0.136 基线对应物

- `StoredOAuthTokens` 结构**与 0.147 完全相同**（`oauth.rs:57` 同 5 字段）——序列化格式 0.136→0.147 未变，字节级兼容。
- `REFRESH_SKEW_MILLIS = 30_000`（`oauth.rs:54`）、`token_needs_refresh`（:504）、`refresh_expires_in_from_timestamp`（:105）均已存在。
- `refresh_if_needed`（`oauth.rs:346`）**内联在 oauth.rs**，无 `oauth/` 子目录、无事务锁——即刷新已存在但**非并发安全**版本；0.145 的改动是把它抽成锁定事务。

### codexgo 现状锚点

- **格式已忠实移植**：`internal/mcp/oauth_tokens.go:126 StoredOAuthTokens`（同 5 字段 + `expires_at,omitempty`）、`:23 OAuthTokenResponse`（含 `refresh_token`、`expires_in`，自定义 Marshal/Unmarshal 保序）、`:143 ComputeExpiresAtMillis`。
- **刷新判定已移植**：`internal/mcp/oauth_store.go:34 refreshSkewMillis = 30_000`、`:206 TokenNeedsRefresh`、`:181 refreshExpiresInFromTimestamp`（load 时反推 expires_in）。
- **缺口（spec 说的"无刷新流"精确定位）**：
  - `RefreshToken` 字段仅被**存/读/序列化**（oauth_store.go:248/371/416、oauth_tokens.go:27），**从不用于换取新 access_token**——无向 token endpoint POST `grant_type=refresh_token` 的网络调用。
  - HTTP transport **无 401 触发刷新**（`grep 401|Unauthorized internal/mcp/*.go` 无命中）。
  - 无 `refresh_if_needed` 等价入口，无事务锁。

### 移植要点

1. 补 refresh 网络调用：向 token endpoint POST `grant_type=refresh_token` + `refresh_token` + `client_id`，解析 `OAuthTokenResponse`，`ComputeExpiresAtMillis(now)` 重算 `expires_at`，`OAuthStore.Save` 写回——**写回序列化必须与既有 `StoredOAuthTokens` marshal 字节同形**（复用 paritytest canonicalizer 对照，格式 0.136=0.147 已知不变）。
2. 触发：连接/调用前 `TokenNeedsRefresh`（skew 30s，已有）为真则先刷新；401 后刷新并**重试一次**（验收明确要求）。
3. 并发安全：对齐 0.145 事务语义——刷新加进程内互斥（等价 `refresh_lock`），锁内重读 store 为权威快照，避免并发重复刷新。刷新请求超时抄 `45s`（`REFRESH_REQUEST_TIMEOUT`）。
4. scope 边界（守住非目标）：只做「凭据序列化刷新」——完整 authorization-code 流（浏览器回调/PKCE/动态注册）**不做**，属簇 F。若刷新缺 token endpoint 元数据须 fail-closed 报错（错误码），禁静默假成功。
5. 保留新 refresh_token：部分 IdP 在刷新响应里轮换 refresh_token，须回写覆盖，不能丢（对齐 oauth.rs:709 的 `token_response.refresh_token()` 回填）。

---

## 需求6 — 工具搜索默认启用（含规模裁定 / 熔断判定）

### 0.147 实现位置与规模（wc -l 证据）

- 工具搜索子系统（0.147）：
  - `tools/src/tool_search.rs`（160 行）+ `tools/src/tool_search_tests.rs`（179 行）；
  - `core/src/tools/handlers/tool_search.rs`（371 行）+ `core/src/tools/handlers/tool_search_spec.rs`（221 行）；
  - **合计约 931 行**（未计其依赖的 deferred-tool 装载、world state、BM25 检索等外围）。
- 默认开关（features 注册表）：
  - `features/src/lib.rs:1155` `FeatureSpec { id: ToolSearch, key: "tool_search", stage: Removed, default_enabled: false }`——注释（:175）"Removed compatibility flag retained as a no-op **now that tool_search is always enabled**"：即 tool_search **已恒定启用**，flag 只是被移除的兼容 no-op。
  - `features/src/lib.rs:1159` `FeatureSpec { id: ToolSearchAlwaysDeferMcpTools, key: "tool_search_always_defer_mcp_tools", stage: Removed, default_enabled: true }`——MCP 工具**恒定 defer 到 tool_search 背后**。

### 0.136 基线对应物

- tool_search 逻辑在 0.136 位于 `tools/src/tool_discovery.rs`（149 行，**无独立 `tools/src/tool_search.rs`**）+ `core/src/tools/handlers/tool_search.rs`（275 行）+ `tool_search_spec.rs`（113 行），合计约 537 行。0.147 把它重构抽出独立文件并扩容（537 → 931 行）。
- **关键**：`tool_search` 在 0.136 features 注册表（`features/src/lib.rs:966`）**已经**是 `Stage::Removed`，注释同为"always enabled"——即**「工具搜索默认启用」这一翻转发生在 0.136 之前，不是 0.143→本簇的增量**。
- 真正的 0.136→0.147 增量只有一条：`tool_search_always_defer_mcp_tools` 从 `Stage::UnderDevelopment, default_enabled: false`（0.136 `lib.rs:970`）翻为 `Stage::Removed, default_enabled: true`（0.147）——即「MCP 工具恒定 defer 到 tool_search 背后」成为默认。

### codexgo 现状锚点（★子系统已存在）

- codexgo **已移植 tool search 子系统**：`internal/tools/tool_search_spec.go`、`tool_search_entry.go`、`mcp_search.go`、`bm25.go`（合计约 **589 行**）；deferred 装载 `tool_definition.go:28 IntoDeferred`、`responses_api.go:206 McpToolToDeferredResponsesApiTool`、`LoadableToolSpec` 均在。
- codexgo features 注册表精确对照：
  - `internal/features/feature.go:352` `{ID: FeatureToolSearch, Key: "tool_search", Stage: removed(), DefaultEnabled: false}`——**已与 0.147 一致**（removed no-op，行为恒开）。
  - `internal/features/feature.go:353` `{ID: FeatureToolSearchAlwaysDeferMcpTools, Key: "tool_search_always_defer_mcp_tools", Stage: underDevelopment(), DefaultEnabled: false}`——**停在 0.136 状态**，是唯一待翻项。

### 规模裁定（spec 49 熔断条款）

- **裁定：不触发拆分，无需拆出独立 spec。** 依据：tool search 虽是 ~931 行的独立子系统（体量确实"大型"），但 **codexgo 已把该子系统整体移植到位（~589 行 + deferred 装载链）**，需求6 的剩余增量**不是**移植大型新功能，而是**单行默认翻转**：
  - `internal/features/feature.go:353` 由 `underDevelopment()/DefaultEnabled:false` 改为 `removed()/DefaultEnabled:true`，对齐 0.147 `ToolSearchAlwaysDeferMcpTools`。
- **需停下确认的边界（触规则5硬门槛①：验收/行为契约）**：翻转 always-defer 会改变 **MCP 工具对模型的暴露方式**（从直接暴露改为恒定 defer 到 tool_search 背后），属模型可见行为变更。须在实施前确认：(a) codexgo 的 deferred-MCP 路径（`McpToolToDeferredResponsesApiTool`）在"全部 MCP 工具都 defer"场景下行为正确；(b) 与在途 opendb-mvp 的 MCP 渲染面不冲突。此为 obvious 但非零风险的一行改动，建议进度报告显式登记而非静默翻。
- 证据汇总：0.147 子系统 931 行 / 0.136 子系统 537 行 / codexgo 已移植 589 行；真实增量 = 1 个 feature 默认值 + 其行为验证。

---

## 附：结论摘要与最大意外

1. **最大意外（需 maintainer 决策）**：需求2 的"工具目录缓存"在 0.147 是**进程内内存 LRU**（`Arc<Mutex<LruCache>>`，容量32/TTL 30min），**无磁盘文件、无 JSON 落盘**。spec 49 需求2 所写"`~/.codexgo` 下新缓存文件 + 格式对齐上游"与上游实际不符——源码里没有可抄的磁盘路径与 JSON 样例。需决定按上游做进程内缓存，还是自主设计磁盘持久化（后者超"对齐上游"范围，触规则5硬门槛）。
2. **需求6 裁定**：不拆分。tool search 子系统 codexgo 已整体移植（589 行），剩余仅 `feature.go:353` 一行默认翻转（always-defer MCP tools：underDevelopment/false → removed/true），但翻转改变 MCP 工具模型可见暴露方式，须实施前确认 deferred-MCP 路径正确性。
3. 其余需求性质：需求1（版本协商）= 常量升级 + 手工降级逻辑（无 rmcp SDK 兜底）；需求3（schema 保形）= 纯移植，`schemaChildKeys` 加 oneOf/allOf、预算 4000→5000B/深度 2→3、`JsonSchema` 加 OneOf/AllOf 字段，边界清晰低风险；需求4（可靠性）= 最大工程量，manager 一次性启动改造为可 reconcile（复用健康连接）+ 退避重连（1s→30s 指数）+ HTTP 重试 [250,1000]ms，全为 0.147 新增；需求5（OAuth 刷新）= codexgo 已忠实移植 token 格式与刷新判定，缺口是网络刷新调用 + 401 触发 + 0.145 事务锁（REFRESH_REQUEST_TIMEOUT 45s）。
4. 结构校正：spec 源参考写的 `core/src/mcp/` 目录两棵树均不存在；MCP 连接管理与工具目录实际在 `codex-mcp` crate（`connection_manager/*` + `tool_catalog_cache.rs` 等），协议 mode 在 `rmcp-client/src/protocol_mode.rs`，schema 在 `tools/src/json_schema.rs`。实施前务以本盘点路径为准。

