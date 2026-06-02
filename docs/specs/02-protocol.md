# 02 — Wire Protocol (`codex-protocol`)

| | |
|---|---|
| **Phase** | 1 — Contracts |
| **Status** | Not started |
| **Depends on** | 01 |
| **Size** | L |
| **Drop-in critical** | ★★ (the central wire contract) |

## 目标 / Goal
Port `codex-protocol` — the Submission/`Op` (client→engine) and `Event`/`EventMsg`
(engine→client) model plus `ResponseItem` and shared config enums. This is the
backbone every other layer serializes to/from; it must be JSON-compatible with
Codex.

## 源参考 / Source reference
- `reference-codex/codex-rs/protocol/src/protocol.rs` (~5.5K lines) — `Submission`,
  `Op`, `Event`, `EventMsg`.
- `protocol/src/` config/event subtypes; `config_types` (AskForApproval,
  PermissionProfile, SandboxPolicy, ShellEnvironmentPolicy, WebSearchMode,
  AutoCompactTokenLimitScope).
- `reference-codex/codex-rs/docs/protocol_v1.md` (narrative + turn sequence).

## 功能需求 / Functional requirements
1. **Submission/Op**: `Submission{ id, op }`; `Op` variants incl. `UserTurn`,
   `UserInput`, `ThreadSettings`, `Interrupt`, `Compact`, `ExecApproval`,
   `PatchApproval`, `UserInputAnswer`, `RequestPermissionsResponse`,
   `DynamicToolResponse`, `ResolveElicitation`, `ThreadRollback`, `Review`,
   `Shutdown`, `RefreshMcpServers`, realtime ops. Serialized with the same
   tagging (`#[serde(tag=...)]`) and field names.
2. **Event/EventMsg**: `Event{ id, event }`; ~50 `EventMsg` variants incl.
   `TurnStarted`/`TurnComplete`, `AgentMessage`(+`ContentDelta`),
   `AgentReasoning`, `ExecCommandBegin`/`OutputDelta`/`End`, `ExecApprovalRequest`,
   `ApplyPatchApprovalRequest`, `PatchApplyBegin`/`Updated`/`End`,
   `RequestUserInput`, `RequestPermissions`, `ElicitationRequest`, `TokenCount`,
   `Error`/`Warning`/`GuardianWarning`, `SessionConfigured`, `TurnAborted`,
   `ContextCompacted`, `ThreadRolledBack`, realtime events.
3. **v1↔v2 wire aliases**: accept/emit both `task_started`/`turn_started` and
   `task_complete`/`turn_complete`.
4. **ResponseItem**: message/reasoning/tool-call/local-shell-call item types used by
   the model and persisted in rollout.
5. **Forward compatibility**: tolerate unknown variants/fields without erroring
   (mirror Rust `#[non_exhaustive]` behavior) — preserve unknown fields on
   round-trip where feasible.

## 验收方案 / Acceptance criteria
- Golden round-trip: every captured `Op` and `EventMsg` sample from Codex 0.136.0
  unmarshals and re-marshals to byte-identical JSON (after canonicalization).
- Unknown-variant fixture deserializes without error and is preserved on re-emit.
- v1 and v2 alias fixtures both parse to the same internal type and re-emit in the
  requested dialect.
- `config_types` enum string values match Codex exactly (e.g. `AskForApproval`
  values) — table test against captured config.

## 风险与难点 / Risks
- The enum surface is large and evolves; lock to 0.136.0 and track deltas.
- Preserving unknown fields requires `json.RawMessage` capture patterns.
- `ts-rs`/`schemars`-generated schemas are the source of truth for field names;
  derive Go struct tags directly from captured JSON, not from guesswork.

## 非目标 / Non-goals
- App-server JSON-RPC method envelope (spec 32) — this is the inner payload model.
