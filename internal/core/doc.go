// Package core is a faithful Go port of the codex-core agent engine (the
// "turn-running" subset). It implements the Codex facade — a submission-queue
// (SQ) / event-queue (EQ) pair — together with the session, turn, and
// turn-context state machinery that drives a synchronous turn end-to-end
// against a pluggable model client.
//
// # Architecture
//
// The design mirrors the Rust `codex_core` crate:
//
//   - [Codex] is the high-level handle returned by [Spawn]. Callers push
//     [protocol.Op] submissions via [Codex.Submit] and consume
//     [protocol.Event] values via [Codex.NextEvent]. A background goroutine
//     (the submission loop) drains the SQ and routes each op to a handler,
//     preserving submission_id/turn_id correlation and FIFO ordering.
//
//   - [Session] owns the long-lived, thread-safe conversation state shared
//     across turns: the [SessionState] (history, token usage, permission
//     profile, environment handles), the active-turn slot, and the injected
//     manager dependencies ([SessionServices]).
//
//   - [ActiveTurn] / [RunningTask] / [TurnState] model the at-most-one running
//     task per session, its pending-approval maps, and its cancellation token
//     hierarchy (mirroring tokio's CancellationToken parent/child links).
//
//   - [TurnContext] is the per-turn read-only snapshot (model info, tools,
//     permission profile, environments, approval policy, collaboration mode)
//     handed to the turn runner.
//
// # Dependency injection
//
// To stay testable and to avoid import cycles, core depends on the concrete
// implementations of cross-cutting managers through small interfaces defined in
// managers.go ([ModelClient], [ToolRouter], [McpManager], [SkillsManager],
// [PluginsManager], [HooksEngine], [ModelsManager], [ExecService],
// [RolloutRecorder]). Callers (area agents, tests) inject concrete impls via
// [CodexSpawnArgs].
//
// # Immutability and concurrency
//
// Conversation history and session configuration follow immutable-update
// patterns: mutators return new copies rather than mutating shared values in
// place. All shared mutable state is guarded by a mutex; streaming and the
// SQ/EQ use channels and goroutines with context.Context for cancellation.
//
// # Deferred scope
//
// The following areas are intentionally stubbed or omitted in this foundation
// (see the package-level STUB comments): goals deep integration, multi_agents,
// unified_exec, realtime_conversation (only the conversation-history surface is
// modeled), apps, attestation, connectors-in-core, remote compaction
// (compact_remote / compact_v2 — only inline compaction is contemplated), and
// agents_md beyond the basics.
package core
