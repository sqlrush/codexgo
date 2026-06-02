# 31 — Core: API Facade & Manager Assembly

| | |
|---|---|
| **Phase** | 7 — Core engine |
| **Status** | Not started |
| **Depends on** | 21, 22, 23, 24, 25, 26, 27, 28, 29, 30 |
| **Size** | M |
| **Drop-in critical** | partial |

## 目标 / Goal
Port `codex-core-api` and complete the wiring: assemble all managers (MCP, skills,
plugins, hooks, models, environment, memories, guardian, extensions) into the `Codex`
engine and expose the stable public facade other binaries (app-server, exec,
mcp-server, TUI) consume.

## 源参考 / Source reference
- `reference-codex/codex-rs/core-api/src/` (re-exports + facade traits).
- `core/src/` manager construction in `session/mod.rs`; `codex-core-plugins`,
  `codex-core-skills` integration points.

## 功能需求 / Functional requirements
1. `core-api` public surface: re-export `Op`, `EventMsg`, thread lifecycle traits,
   and the `Codex` constructor inputs so downstream crates depend on a stable facade
   rather than `core` internals.
2. Manager assembly: construct and inject `McpManager` (21), `SkillsManager` (23),
   `PluginsManager` (22), hooks engine (24), `ModelsManager` (07),
   environment manager (10), memories (26), guardian/extensions (26) into
   `CodexSpawnArgs`.
3. Lifecycle ordering: initialize managers in the same order Codex does (so e.g.
   skills/plugins are available before the first turn), with the same error handling
   if a manager fails to start.
4. `thread-manager-sample`-style end-to-end smoke path for integration testing.

## 验收方案 / Acceptance criteria
- A full engine can be constructed and run a turn end-to-end (with mocks for the
  model) producing Codex-equivalent events/rollout.
- Manager init order + failure handling matches Codex (e.g. a failing MCP server
  degrades the same way).
- `core-api` exposes everything app-server/exec/tui need without leaking `core`
  internals.

## 风险与难点 / Risks
- This is the integration capstone of Phase 7; bugs here surface as cross-cutting
  differential failures. Land it behind a solid integration test.

## 非目标 / Non-goals
- The transports that expose the engine (Phase 8).
