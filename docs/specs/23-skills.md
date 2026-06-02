# 23 — Skills

| | |
|---|---|
| **Phase** | 6 — Extensibility |
| **Status** | Not started |
| **Depends on** | 04 |
| **Size** | M |
| **Drop-in critical** | ★ (SKILL.md format + discovery) |

## 目标 / Goal
Port `codex-skills` + `codex-core-skills`: the skills system (Claude-style
`SKILL.md`), discovery from built-in/plugin/user locations, enable/disable, implicit
invocation, and prompt injection.

## 源参考 / Source reference
- `reference-codex/codex-rs/skills/src/` (embedded built-in skills via `include_dir`).
- `reference-codex/codex-rs/core-skills/src/` (manager, loading, rendering,
  `build_available_skills`).
- `reference-codex/docs/skills.md`.

## 功能需求 / Functional requirements
1. `SKILL.md` format: YAML front-matter (`name`, `description`) + markdown body;
   support cross-skill references.
2. Discovery + precedence: built-in (embedded) → plugin skills → user skills under
   `~/.codex/skills/<name>/SKILL.md`; effective skill roots aggregated across active
   plugins; per-plugin disabled lists.
3. Implicit invocation: command-like skill names auto-discovered (e.g. `/code-review`);
   mention counting; token-budget truncation when rendering the available-skills
   list into instructions.
4. `build_available_skills` rendering (aliases, descriptions, paths) injected into
   the system/user instructions exactly as Codex does.

## 验收方案 / Acceptance criteria
- A user/plugin `SKILL.md` set is discovered with the same names/aliases/precedence
  as Codex.
- The rendered available-skills block matches Codex byte-for-byte for a fixture set
  (golden), including budget truncation.
- Enable/disable state resolves identically.

## 风险与难点 / Risks
- Front-matter parsing must tolerate the same variations Codex tolerates.
- Embedded built-in skills must be bundled identically (copy assets verbatim).

## 非目标 / Non-goals
- Hooks (24) and slash-command UI (39), though they interplay.
