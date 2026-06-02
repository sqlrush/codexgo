# 26 — Extensions, Connectors & Memories

| | |
|---|---|
| **Phase** | 6 — Extensibility |
| **Status** | Not started |
| **Depends on** | 16, 18 |
| **Size** | L |
| **Drop-in critical** | ★ (memory store + extension behavior) |

## 目标 / Goal
Port the `ext/*` extensions, `codex-connectors`, the memory read/write feature, and
`collaboration-mode-templates`: the first-party extensions layered on the tool/agent
framework.

## 源参考 / Source reference
- `reference-codex/codex-rs/ext/`: `extension-api` (trait), `goal`, `guardian`,
  `image-generation`, `web-search`, `memories`.
- `reference-codex/codex-rs/connectors/`, `memories/read/`, `memories/write/`,
  `collaboration-mode-templates/`.

## 功能需求 / Functional requirements
1. **extension-api**: the async extension trait (tool/request interception); the
   registration/dispatch mechanism core uses to load extensions.
2. **goal**: goal/intention tracking state machine (`/goal`), persisted per thread.
3. **guardian**: the automated security reviewer that assesses tool use/patches and
   feeds the approval flow (spec 30) with risk assessments / `GuardianWarning`.
4. **image-generation**: image-gen tool integration (OpenAI image API).
5. **web-search**: the web-search extension (request shaping; results are
   server-side via Responses API).
6. **memories**: skill tools `list` / `read` (20K token budget) / `search` /
   `add_ad_hoc_note`, backed by `memories_1.sqlite` (spec 18); developer-role
   messages excluded; templates under `ext/memories/templates/`.
7. **connectors**: SaaS app connector metadata/filtering/accessibility.
8. **collaboration-mode-templates**: tri-state preset masks (reasoning_effort,
   developer_instructions, model) for collaboration modes.

## 验收方案 / Acceptance criteria
- Memory storage written by `codexgo` is readable by Codex (DB-level compat,
  spec 18) and the skill tools return equivalent results.
- Guardian assessments emit the same event types and gate the same operations on a
  labeled fixture set.
- Collaboration-mode preset merging matches Codex (tri-state field resolution).
- Connector metadata + image-gen/web-search request shapes match captures.

## 风险与难点 / Risks
- Guardian's risk logic may itself call the model; capture deterministic cases.
- Memory ranking/search parity is best-effort; document deviations.

## 非目标 / Non-goals
- The TUI surfaces for these (`/memories`, `/goal`, `/personality`) — spec 39.
