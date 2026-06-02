# 24 — Hooks

| | |
|---|---|
| **Phase** | 6 — Extensibility |
| **Status** | Not started |
| **Depends on** | 04 |
| **Size** | M |
| **Drop-in critical** | ★ (hook config schema + dispatch) |

## 目标 / Goal
Port `codex-hooks`: the lifecycle-hook engine that runs user/plugin commands at
defined agent events, with matchers, outcomes, and state persistence.

## 源参考 / Source reference
- `reference-codex/codex-rs/hooks/src/` (event types, matchers, handlers, state).
- `reference-codex/codex-rs/config/src/hook_config.rs`
  (`HookEventsToml`, `MatcherGroup`, `HooksFile`).

## 功能需求 / Functional requirements
1. The 10 event types: `PreToolUse`, `PermissionRequest`, `PostToolUse`,
   `PreCompact`, `PostCompact`, `SessionStart`, `UserPromptSubmit`,
   `SubagentStart`, `SubagentStop`, `Stop`.
2. Config schema (TOML `[hooks]` / `hooks.json` from plugins): per-event arrays of
   `{command, args, env, matcher{toolName, source, …}}`; env interpolation
   (`$tool.name`, etc.).
3. Dispatch: evaluate matchers, run hook commands (subprocess) with the right
   environment/payload, collect outcomes, merge outcomes (e.g. suppress/modify),
   persist hook state hashes (`state.hooks.<event>.<hook_id>`).
4. Sourcing from config + plugins (spec 22), with `external-agent-migration`
   compatibility for imported hook configs.

## 验收方案 / Acceptance criteria
- Hook config parsing (TOML + plugin JSON) matches Codex (golden).
- For a scripted session, the same hooks fire in the same order with the same
  payload/env as Codex (differential), and outcomes merge identically.
- Hook state persistence keys/values match.

## 风险与难点 / Risks
- Matcher semantics and outcome-merge precedence are subtle; mirror exactly.
- Subprocess env/payload contract must match (hooks are user-authored against it).

## 非目标 / Non-goals
- The events themselves are emitted by core (specs 27–30); this owns the engine.
