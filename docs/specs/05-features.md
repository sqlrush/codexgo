# 05 — Feature Flags

| | |
|---|---|
| **Phase** | 1 — Contracts |
| **Status** | Not started |
| **Depends on** | 02 |
| **Size** | S |
| **Drop-in critical** | partial (flag names + defaults) |

## 目标 / Goal
Port `codex-features`: the typed feature-flag registry that gates experimental
behavior across the engine, TUI, and tools, plus the `--enable`/`--disable` CLI
and `codex features` surface.

## 源参考 / Source reference
- `reference-codex/codex-rs/features/src/` (flag enum, defaults, toml binding).
- Consumers: `core`, `tui`, `app-server`, `cli` (`features` subcommand).

## 功能需求 / Functional requirements
1. Enumerate all feature flags present in 0.136.0 with their default state and
   stability tier (e.g. `child_agents_md`, realtime, code-mode toggles, etc.).
2. Resolution precedence: CLI `--enable/--disable` → config `[features]` → defaults.
3. Typed accessor API used by other packages (no stringly-typed lookups at call
   sites).
4. Serialize enabled set for telemetry/session-meta the same way Codex does.

## 验收方案 / Acceptance criteria
- Flag name set + defaults match a captured `codex features list` dump exactly.
- `--enable X --disable X` precedence resolves identically to Codex.
- Unknown flag on CLI errors with the same class of message; unknown flag in config
  warns (consistent with spec 04 strictness).

## 风险与难点 / Risks
- Flags change frequently; pin to 0.136.0 and treat additions as roadmap deltas.

## 非目标 / Non-goals
- Implementing the behavior behind each flag — that lives in the owning spec.
