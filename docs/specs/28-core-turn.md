# 28 — Core: Turn Execution Loop

| | |
|---|---|
| **Phase** | 7 — Core engine |
| **Status** | Not started |
| **Depends on** | 27 |
| **Size** | XL |
| **Drop-in critical** | ★★ (the agent loop behavior) |

## 目标 / Goal
Port the heart of `codex-core`: the turn execution loop that samples the model,
streams output, dispatches tool calls, records results, and handles
interrupt/cancellation. This is the behavior that *is* the agent.

## 源参考 / Source reference
- `reference-codex/codex-rs/core/src/session/turn.rs` (`run_turn`, streaming
  adaptation, function-call collection, interrupts).
- `core/src/session/turn_context.rs` (`TurnContext`), `core/src/tools/router.rs`
  (`ToolRouter`, `ToolCall`), `core/src/client.rs` (`ModelClient`/session),
  `core/src/event_mapping.rs` (ResponseItem→TurnItem), `core/src/state/turn.rs`
  (`TurnState`, `ActiveTurn`, `RunningTask`, `TaskKind`).

## 功能需求 / Functional requirements
1. `run_turn` loop: build `TurnContext` (model_info, tools, permission profile,
   environments, approval policy, collaboration mode); call the model (spec 06);
   stream deltas → emit `AgentMessageContentDelta`/`AgentReasoning*`; collect
   function calls; execute via `ToolRouter`; record tool outputs back into the
   conversation; loop until no more tool calls / end-turn.
2. Tool routing: parse `ToolCall` (Function / ToolSearch / Custom), parallel vs
   sequential execution per tool, error handling, timeout/cancellation propagation,
   output truncation, turn-diff tracking.
3. Streaming adaptation: map Responses API stream events into internal `TurnItem`s
   and emit the corresponding `EventMsg`s with correct ordering.
4. Token accounting per turn (input/output/total) → `TokenCount` events and session
   aggregation; routing headers/metadata (turn metadata, sticky routing).
5. Interrupt (`Op::Interrupt`) aborts the active turn without killing the session;
   `Op::Shutdown` terminates everything. `TaskKind` Regular/Review/Compact.
6. Retry/backoff on transport/stream errors consistent with spec 06.

## 验收方案 / Acceptance criteria
- Differential: for recorded model responses (replayed via a mock Responses
  endpoint), `codexgo` emits the same `EventMsg` sequence and the same final
  conversation/rollout as Codex.
- Tool-call execution order (parallel/sequential) and tool output recording match.
- Interrupt mid-turn produces the same `TurnAborted`/partial state as Codex.
- Token counts per turn match captured `TokenCount` events.

## 风险与难点 / Risks
- Highest-complexity spec alongside 13. The streaming/tool/cancellation interplay
  is where subtle divergences hide; lean hard on replay-based differential tests.
- Cancellation hierarchy (`CancellationToken` child tokens) → `context` trees;
  ensure background tool processes are handled like Codex on interrupt.

## 非目标 / Non-goals
- Compaction (29) and approval gating (30), invoked from here but specified
  separately.
