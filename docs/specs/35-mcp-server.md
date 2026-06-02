# 35 — MCP Server (`codex mcp-server`)

| | |
|---|---|
| **Phase** | 8 — Headless server & exec |
| **Status** | Not started |
| **Depends on** | 31, 32 |
| **Size** | M |
| **Drop-in critical** | ★ (MCP server interface) |

## 目标 / Goal
Port `codex-mcp-server`: Codex exposed **as** an MCP server (`codex mcp-server`),
so other agents/tools can drive Codex over MCP (stdio JSON-RPC).

## 源参考 / Source reference
- `reference-codex/codex-rs/mcp-server/src/` (rmcp server, v2 RPCs, notifications,
  approvals, v1 compatibility methods).
- `reference-codex/codex-rs/docs/codex_mcp_interface.md`.

## 功能需求 / Functional requirements
1. MCP server over stdio (JSON-RPC 2.0) exposing the v2 surface: `thread/*`,
   `turn/*`, `account/*`, `config/*`, `model/list`, `app/list`,
   `collaborationMode/list`.
2. Streaming notifications `codex/event/*` for live agent events.
3. Server→client approval requests (`applyPatchApproval`, `execCommandApproval`).
4. Tool responses as MCP `CallToolResult` with `structuredContent` mirror.
5. v1 compatibility methods: `getConversationSummary`, `getAuthStatus`,
   `gitDiffToRemote`, `fuzzyFileSearch`.
6. `--strict-config` handling.

## 验收方案 / Acceptance criteria
- An MCP client (or captured traffic) driving `codexgo mcp-server` gets
  byte-identical responses/notifications vs `codex mcp-server` (differential).
- Approval round-trips (server→client) match.
- v1 compatibility methods return Codex-equivalent payloads.

## 风险与难点 / Risks
- Shares the MCP stack with spec 21 (client); ensure a single, correct MCP
  implementation serves both roles.
- Notification stream ordering must match the engine's event order.

## 非目标 / Non-goals
- Codex as MCP *client* (spec 21); non-stdio transports unless Codex supports them.
