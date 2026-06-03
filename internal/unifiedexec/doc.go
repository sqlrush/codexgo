// Package unifiedexec ports codex's unified-exec subsystem
// (codex-rs/core/src/unified_exec) to Go: persistent, PTY-backed shell/exec
// sessions kept alive across tool calls.
//
// Faithful port of codex 0.136.0. The Rust subsystem manages interactive
// processes (create, reuse, buffer output with caps), spawning each PTY from a
// sandbox-prepared request and exposing write-stdin / read-output / resize /
// kill as a tool executor. The Rust flow additionally drives approvals, sandbox
// selection, network-approval, and event emission through core-private types
// (Session, TurnContext, ToolOrchestrator). Those layers live outside this
// package's public-API surface, so this port reuses the public spawn transports
// (internal/pty + internal/execserver) and accepts an already-prepared spawn
// description, keeping the policy/approval orchestration to the caller.
//
// The package is split across small files mirroring the Rust module layout:
//
//   - constants.go: yield/output caps and small helpers (clamp_yield_time,
//     resolve_max_tokens, generate_chunk_id, the UNIFIED_EXEC_ENV overlay).
//   - errors.go: the [Error] family (UnifiedExecError).
//   - state.go: [processState] (shared exit/failure state).
//   - headtail.go: [HeadTailBuffer] (capped head+tail output buffer).
//   - process.go: [Process] (PTY / exec-server process lifecycle + buffering).
//   - manager.go: [ProcessManager] (id allocation, reuse, LRU pruning,
//     deadline-bounded output collection, exec/write-stdin orchestration).
//   - executor.go: [Executor] (the tool-executor facade: ExecCommand,
//     WriteStdin, Resize, Kill).
package unifiedexec
