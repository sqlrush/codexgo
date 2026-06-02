# 27 — Core: Session / Thread Lifecycle

| | |
|---|---|
| **Phase** | 7 — Core engine |
| **Status** | Not started |
| **Depends on** | 04, 06, 16, 17 |
| **Size** | L |
| **Drop-in critical** | partial (event/op behavior) |

## 目标 / Goal
Port the session/conversation/thread lifecycle of `codex-core`: the `Codex` facade,
`Session` state, the submission-queue/event-queue pattern, and thread
create/fork/resume — the spine the turn loop runs on.

## 源参考 / Source reference
- `reference-codex/codex-rs/core/src/session/mod.rs` (`Session`, `Codex`,
  `CodexSpawnArgs`, event channel, status watcher).
- `core/src/thread_manager.rs`, `core/src/codex_thread.rs`,
  `core/src/realtime_conversation.rs`, `core/src/state/session.rs`.

## 功能需求 / Functional requirements
1. `Codex` facade API: `spawn(args)`, `submit(Op)`, `next_event()`,
   `shutdown_and_wait()`, status/config watchers. `CodexSpawnArgs` wiring (config,
   auth, models/environment/skills/plugins/mcp managers).
2. Submission queue (SQ) → event queue (EQ) loop: accept `Op`s, route to handlers,
   emit `Event`s in the same order/correlation (submission_id/turn_id) as Codex.
3. Conversation history manager (thread-safe, shared across turns): user/assistant/
   tool/reasoning items, context-compaction markers, turn/submission linkage.
4. Thread lifecycle: create (with `session_meta`), fork (copy history,
   `forked_from_id`), resume (replay rollout), config snapshot
   (`thread_config_snapshot`), rollback.
5. Session-level state: token usage aggregation, agent status, permission profiles,
   environment manager, goal runtime, network-proxy handle, MCP manager handle.

## 验收方案 / Acceptance criteria
- For a scripted sequence of `Op`s, the emitted `Event` stream matches Codex in
  order, types, and correlation IDs (differential capture).
- Fork/resume reconstruct the same in-memory history + rollout files as Codex.
- `SessionConfigured` and config-snapshot contents match captures.
- Clean shutdown drains and terminates like Codex (no lost events).

## 风险与难点 / Risks
- Concurrency model: Rust tokio channels/watch + `Arc<Mutex/RwLock>` → Go
  goroutines/channels + `sync` + `context`. Preserve ordering guarantees and
  cancellation hierarchy.
- The SQ/EQ ordering is observable; subtle races would break differential tests.

## 非目标 / Non-goals
- The turn execution body (spec 28); compaction (29); approvals (30).
