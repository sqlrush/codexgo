# 10 — Exec Server & Filesystem Operations

| | |
|---|---|
| **Phase** | 3 — Execution & sandboxing |
| **Status** | Not started |
| **Depends on** | 02, 09 |
| **Size** | M |
| **Drop-in critical** | partial (fs op protocol) |

## 目标 / Goal
Port `codex-exec-server` and `codex-file-system`: the process-execution service and
the sandbox-aware filesystem operations (read/write/create/remove/copy/metadata/
watch) that back both the agent and the app-server `fs/*` methods.

## 源参考 / Source reference
- `reference-codex/codex-rs/exec-server/src/` (process exec, fs helper, env mgmt,
  axum/ws surface).
- `reference-codex/codex-rs/file-system/src/` (`FileSystem` trait, ops).
- Note: exec-server also runs as an arg0/argv1 helper (`FS_HELPER`).

## 功能需求 / Functional requirements
1. Process execution service: spawn commands (PTY or pipe), stream stdout/stderr,
   enforce token/time budgets, propagate cancellation, collect aggregated output
   with truncation (spec 01 rules).
2. Filesystem abstraction with the same operations and error mapping used by
   `fs/readFile`, `fs/writeFile`, `fs/createDirectory`, `fs/getMetadata`,
   `fs/readDirectory`, `fs/remove`, `fs/copy`, `fs/watch`/`fs/unwatch`
   (watch via spec 20).
3. Environment management: per-`environment_id` cwd/shell/env resolution; sticky
   session environments; `ShellEnvironmentPolicy` filtering of inherited env.
4. Optional standalone exec-server mode (`--listen ws://…` / `--remote`) for
   distributed execution.

## 验收方案 / Acceptance criteria
- `fs/*` operations produce responses matching the app-server schema (validated in
  spec 33) — metadata fields, error codes.
- Environment policy filtering yields the same effective env as Codex for a set of
  policy configs.
- Aggregated output + truncation matches Codex for long-running command fixtures.

## 风险与难点 / Risks
- The exec-server has both in-process and networked modes; keep the core logic
  transport-agnostic.
- Concurrent fs operations must be safe and ordered consistently.

## 非目标 / Non-goals
- The sandbox confinement itself (12–14); the tool schema (16).
