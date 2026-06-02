# 04 — Configuration (`config.toml`)

| | |
|---|---|
| **Phase** | 1 — Contracts |
| **Status** | Not started |
| **Depends on** | 02, 03 |
| **Size** | L |
| **Drop-in critical** | ★★ (`config.toml` schema + precedence) |

## 目标 / Goal
Port `codex-config`: load/merge/validate `~/.codex/config.toml`, profiles, env
overrides, and `-c key=value` CLI overrides into the resolved `Config` consumed by
every subsystem. Must read existing Codex config files unchanged.

## 源参考 / Source reference
- `reference-codex/codex-rs/config/src/` (`ConfigToml`, `Config`, builder,
  `hook_config.rs`, profile handling).
- `reference-codex/docs/config.md`, `example-config.md`.

## 功能需求 / Functional requirements
1. Parse all top-level keys: `model`, `review_model`, `model_provider`,
   `model_context_window`, `model_providers` (map), `approvals`, `sandbox`,
   `mcp_servers`, `profiles`, `history`, `analytics`, `tui`, `hooks`,
   `auth_credentials_store`, `oauth_credentials_store`, and the rest of the schema.
2. **Precedence** (highest→lowest): CLI flags → `-c key=value` → env vars →
   active profile (`~/.codex/<name>.config.toml` / `[profiles.<name>]`) →
   `config.toml` → built-in defaults.
3. `CODEX_HOME` resolution; `config.local.toml` overlay; unknown-field handling
   with optional `--strict-config` (error) vs default (warn via `serde_ignored`).
4. Built-in defaults identical to Codex (default provider `openai`, default model,
   default sandbox/approval policy, file-based auth store).
5. Sub-schemas: `ModelProviderInfo`, `[mcp_servers.*]`, `[hooks]`
   (`HookEventsToml`/`MatcherGroup`), `[tui]` (theme/keymap/statusline),
   `PermissionProfile`, `ShellEnvironmentPolicy`.
6. Config mutation API for runtime writes (theme/keymap changes from the TUI) that
   preserve formatting/comments where Codex does (`toml_edit` analog).

## 验收方案 / Acceptance criteria
- Golden: load a corpus of real Codex `config.toml` files → resolved `Config`
  matches a captured reference dump (`codex debug` / app-server config read).
- Precedence test matrix: same inputs across all 6 layers resolve identically to
  Codex.
- Round-trip a config write (e.g. set `[tui].theme`) preserves unrelated
  formatting/comments.
- `--strict-config` rejects unknown keys; default mode warns and continues.

## 风险与难点 / Risks
- Comment/format-preserving edits need `toml_edit`-equivalent behavior; `go-toml/v2`
  is lossy on edit — may need a thin editor layer or `toml_edit` port.
- Provider validation rules (mutually-exclusive auth fields) must match exactly.

## 非目标 / Non-goals
- Cloud-requirements injection into config (spec 43) — hook the loader point only.
