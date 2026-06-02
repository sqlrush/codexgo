# 03 — Exec Policy Engine (Starlark)

| | |
|---|---|
| **Phase** | 1 — Contracts |
| **Status** | Not started |
| **Depends on** | 01 |
| **Size** | M |
| **Drop-in critical** | ★ (policy files + decision semantics) |

## 目标 / Goal
Port `codex-execpolicy`: the Starlark-based, prefix-rule engine that classifies a
command as `allow` / `prompt` / `forbidden`, including `host_executable` basename
resolution. Used by `codex-protocol`, `config`, and `core`.

## 源参考 / Source reference
- `reference-codex/codex-rs/execpolicy/` (`src/`, `README.md`).
- `reference-codex/codex-rs/execpolicy-legacy/` (older Starlark engine — port only
  if still referenced; document if dropped).
- `reference-codex/docs/execpolicy.md`.

## 功能需求 / Functional requirements
1. Evaluate Starlark policy files exposing `prefix_rule(pattern, decision,
   justification, match, not_match)` and `host_executable(name, paths)` via
   `go.starlark.net`.
2. Matching semantics:
   - Ordered token match; `[...]` token = OR alternatives.
   - Exact first-token (absolute path) match wins; otherwise **basename fallback**
     resolves the program name.
   - `host_executable(name, paths)` restricts basename fallback to listed paths;
     absence → unrestricted fallback.
   - Strictest decision wins (`forbidden` > `prompt` > `allow`).
3. Emit the same result JSON: `matchedRules[].prefixRuleMatch{ matchedPrefix,
   decision, resolvedProgram?, justification? }` and top-level `decision`.
4. Built-in/default baseline policy bundled (embedded) identically to Codex.
5. `match`/`not_match` examples in a policy act as self-tests at load time.

## 验收方案 / Acceptance criteria
- For the bundled default policy, every `match`/`not_match` example evaluates
  correctly (load-time self-test passes).
- Golden differential: a corpus of commands evaluated by `codex execpolicy check`
  produces identical decision JSON from `codexgo`.
- Basename fallback + `host_executable` restriction reproduced on path-variant
  command fixtures (e.g. `/usr/bin/git` vs `/opt/homebrew/bin/git`).

## 风险与难点 / Risks
- `go.starlark.net` dialect differences from `starlark-rust` (e.g. type coercion,
  error messages). Validate the specific builtins/grammar Codex policies use.
- The legacy engine uses `allocative`/`derive_more`; confirm whether it is reachable
  at runtime before investing.

## 非目标 / Non-goals
- Actually enforcing sandboxing (spec 12–14) — this spec only *decides*.
