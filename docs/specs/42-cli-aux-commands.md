# 42 — Auxiliary Subcommands

| | |
|---|---|
| **Phase** | 10 — CLI & peripheral |
| **Status** | Not started |
| **Depends on** | 41 |
| **Size** | M |
| **Drop-in critical** | ★ (doctor JSON, debug dumps) |

## 目标 / Goal
Port the diagnostic/utility subcommands: `doctor`, `sandbox`, `apply`,
`resume`/`fork`/`archive`/`unarchive`, `features`, `update`, and the `debug …`
family, plus `state-db-recovery`.

## 源参考 / Source reference
- `reference-codex/codex-rs/cli/src/`: `doctor.rs`, `debug_sandbox.rs`,
  `sandbox_setup.rs`, `state_db_recovery.rs`, `wsl_paths.rs`, `exit_status.rs`,
  `marketplace_cmd.rs`, `mcp_cmd.rs`, `plugin_cmd.rs`, `remote_control_cmd.rs`,
  `app_cmd.rs`.
- `reference-codex/codex-rs/install-context/` (install-method detection for doctor).

## 功能需求 / Functional requirements
1. **doctor**: non-destructive diagnostics — system info, install method (standalone/
   npm/bun/brew/other), config load+validate, auth status, terminal detection,
   thread-state inventory, git detection, provider health (Ollama/LM Studio/OpenAI),
   update availability, background-server reachability. `--json` + `--verbose`;
   exit 0/1.
2. **sandbox**: run a command under the host sandbox (`--profile`), using specs
   12–14; `debug sandbox` variants.
3. **apply**: `git apply` the latest Codex diff (spec 11).
4. **resume/fork/archive/unarchive**: session management over the thread store
   (spec 19), picker or `--last`/id.
5. **features**: list/enable/disable (spec 05).
6. **update**: self-update for release builds (match Codex's mechanism/guard).
7. **debug**: `models [--bundled]`, `prompt-input`, `trace-reduce`, `clear-memories`,
   `app-server send-message-v2`, etc. — JSON dumps used by tests/tooling.
8. **state-db-recovery**: integrity check + repair (spec 18).

## 验收方案 / Acceptance criteria
- `doctor --json` output matches Codex's schema/fields (golden, with environment-
  dependent values normalized).
- `debug models --bundled` and `debug prompt-input` dumps match Codex byte-for-byte
  (golden) — these are key fixtures for other specs.
- `apply` produces the same working-tree result as `codex apply` for a captured diff.
- Session management subcommands produce the same files/DB state as Codex.

## 风险与难点 / Risks
- `doctor` touches many subsystems; keep checks read-only and deterministic where
  possible.
- `update` may be release-channel specific; gate to release builds like Codex.

## 非目标 / Non-goals
- The TUI resume picker (40); the actual cloud/marketplace/mcp bodies (43/22/21).
