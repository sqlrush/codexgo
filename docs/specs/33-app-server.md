# 33 — App-Server, Transport & Daemon

| | |
|---|---|
| **Phase** | 8 — Headless server & exec |
| **Status** | Not started |
| **Depends on** | 31, 32 |
| **Size** | L |
| **Drop-in critical** | ★★ (the server clients connect to) |

## 目标 / Goal
Port `codex-app-server` + `app-server-transport` + `app-server-daemon` +
`app-server-client`: the JSON-RPC server that drives the core engine over
stdio/UDS/WebSocket, plus the daemon and the in-process client used by `exec`/TUI.

## 源参考 / Source reference
- `reference-codex/codex-rs/app-server/src/` (request processor, thread/turn mgmt,
  manager wiring).
- `app-server-transport/src/` (stdio/UDS/WebSocket listeners, remote-control),
  `app-server-daemon/src/` (lifecycle, socket path, signals),
  `app-server-client/src/` (in-process + remote client), `uds/`, `stdio-to-uds/`.

## 功能需求 / Functional requirements
1. JSON-RPC request processor implementing all methods from spec 32 against the
   engine facade (spec 31): thread/turn lifecycle, fs ops (spec 10), plugins/skills/
   hooks/mcp/account/config/review/feedback.
2. Transports: stdio (newline-delimited JSON-RPC), UDS (`codex_uds`, socket path
   resolved from `CODEX_HOME`), WebSocket (`ws://`/`wss://` with policy-based auth:
   API key or OAuth). `stdio-to-uds` relay.
3. Streaming `ServerNotification`s (engine events → clients) with correct
   correlation; per-scope request serialization locks (ordering).
4. Daemon: startup/shutdown, control socket management, signal handling,
   remote-control channel.
5. `app-server-client`: `InProcessAppServerClient` (embeds the engine; used by
   `exec`/TUI) and a remote client (`--remote`).

## 验收方案 / Acceptance criteria
- A reference Codex client (or captured client traffic) drives `codexgo` app-server
  over stdio and gets byte-identical responses/notifications (differential).
- UDS + WebSocket transports interoperate with Codex clients; auth policies enforced
  identically.
- Per-scope ordering matches Codex under concurrent requests.
- Daemon socket path + lifecycle match (a Codex client can find/connect to it).

## 风险与难点 / Risks
- Concurrency + ordering correctness across transports is subtle; mirror the scope
  locks exactly.
- WebSocket auth (API key / OAuth) must match Codex's policy semantics.

## 非目标 / Non-goals
- `exec` CLI formatting (34); `mcp-server` (35); the TUI (Phase 9).
