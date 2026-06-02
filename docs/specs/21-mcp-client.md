# 21 — MCP Client

| | |
|---|---|
| **Phase** | 6 — Extensibility |
| **Status** | Not started |
| **Depends on** | 06, 16 |
| **Size** | L |
| **Drop-in critical** | ★ (MCP config + tool namespacing) |

## 目标 / Goal
Port `codex-mcp` + `codex-rmcp-client`: Codex as an MCP **client** connecting to
external MCP servers (stdio/HTTP/in-process), discovering and calling their tools,
handling OAuth, resources, and elicitation.

## 源参考 / Source reference
- `reference-codex/codex-rs/mcp/src/` (`McpConnectionManager`, lifecycle, namespacing).
- `reference-codex/codex-rs/rmcp-client/src/` (rmcp wrapper, transports, OAuth, keyring).
- `reference-codex/codex-rs/docs/codex_mcp_interface.md`.

## 功能需求 / Functional requirements
1. Read `[mcp_servers.*]` config (command/args, env, transport, bearer-token env,
   `startup_timeout_sec`, OAuth). Support stdio (child process), streamable HTTP,
   and in-process transports.
2. Connection manager: startup with timeouts, connection reuse, capability +
   tool discovery, resource reads (`read_mcp_resource`), auth-status snapshots.
3. **Tool namespacing**: `mcp__<server>__<tool>` (sanitized) exposed through the
   tools framework (spec 16) with deferred loading.
4. OAuth login flow (`perform_oauth_login`) with credentials stored in the system
   keyring per `oauth_credentials_store`; elicitation/confirmation bridging to the
   approval flow (spec 30).

## 验收方案 / Acceptance criteria
- Connect to a reference MCP server (stdio + HTTP) and discover the same tool set
  with the same namespaced names as Codex.
- A tool call round-trips with the same request/result mapping (MCP
  `CallToolResult` ↔ tool output).
- `[mcp_servers]` config parsing matches Codex (golden config).
- OAuth credential entries are compatible with Codex's `oauth_credentials_store`.

## 风险与难点 / Risks
- No `rmcp` in Go — use `modelcontextprotocol/go-sdk` or implement the JSON-RPC MCP
  client; verify protocol version negotiation matches.
- Tool name sanitization rules must match exactly to avoid name drift.

## 非目标 / Non-goals
- Codex *as* an MCP server (spec 35).
