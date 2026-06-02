# 32 — App-Server Protocol (JSON-RPC definitions)

| | |
|---|---|
| **Phase** | 8 — Headless server & exec |
| **Status** | Not started |
| **Depends on** | 02 |
| **Size** | L |
| **Drop-in critical** | ★★ (JSON-RPC method surface) |

## 目标 / Goal
Port `codex-app-server-protocol`: the JSON-RPC 2.0 method/request/response/
notification definitions that clients (IDEs, the TUI, `exec`) use to drive Codex.
Must match method names and payload schemas exactly.

## 源参考 / Source reference
- `reference-codex/codex-rs/app-server-protocol/src/protocol/` (the
  `client_request_definitions!` macro, `ClientRequest`/`ClientResponse`,
  `ServerNotification`, serialization scopes).
- `reference-codex/codex-rs/docs/protocol_v1.md`.

## 功能需求 / Functional requirements
1. Define every JSON-RPC method with its exact wire name, e.g.:
   - Thread: `thread/start`, `thread/resume`, `thread/fork`, `thread/archive`,
     `thread/list`, `thread/read`, `thread/setName`, `thread/turnsList`,
     `thread/injectItems`, `thread/rollback`, `thread/settingsUpdate`,
     `thread/goalSet|Get|Clear`, `thread/metadataUpdate`, `thread/memoryModeSet`,
     `thread/unsubscribe`.
   - Turn: `turn/start`, `turn/steer`, `turn/interrupt`.
   - Filesystem: `fs/readFile`, `fs/writeFile`, `fs/createDirectory`,
     `fs/getMetadata`, `fs/readDirectory`, `fs/remove`, `fs/copy`, `fs/watch`,
     `fs/unwatch`.
   - Plugins/apps/skills/hooks/marketplace, account/auth, review, MCP, config
     (`model/list`, `permissionProfile/list`, `collaborationMode/list`,
     `experimentalFeature/list`), feedback — full set from the macro.
2. `Initialize` handshake (v1 compatibility) as the required first request.
3. `ServerNotification` event stream types (async, not request/response), incl.
   `codex/event/*`.
4. Request serialization scopes (global / thread / command-process) for ordering.
5. Generate/emit JSON Schema + TypeScript-equivalent types (parity with `schemars`/
   `ts-rs`) for client compatibility; `app-server generate-ts` / `generate-json-schema`.

## 验收方案 / Acceptance criteria
- Golden: request/response/notification JSON for every method round-trips
  byte-identically against captured Codex traffic.
- Generated JSON Schema matches Codex's `generate-json-schema` output (semantic
  equality).
- `Initialize` handshake matches v1 exactly.

## 风险与难点 / Risks
- The macro generates a large enum; in Go this is a hand-written or code-generated
  dispatch table. Drive field names directly from captured JSON, not guesses.
- Scopes affect concurrency (spec 33) — define them precisely here.

## 非目标 / Non-goals
- The server that implements these methods (spec 33).
