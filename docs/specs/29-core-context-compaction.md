# 29 — Core: Context Management & Auto-Compaction

| | |
|---|---|
| **Phase** | 7 — Core engine |
| **Status** | Not started |
| **Depends on** | 28 |
| **Size** | L |
| **Drop-in critical** | ★ (compaction triggers + history rewrite) |

## 目标 / Goal
Port context-window management and auto-compaction: token-threshold triggers,
pre/mid/post-turn compaction, summarization, history replacement, and remote
compaction endpoints.

## 源参考 / Source reference
- `reference-codex/codex-rs/core/src/compact.rs`, `compact_remote.rs`,
  `compact_remote_v2.rs`.
- `core/src/state/auto_compact_window.rs` (`AutoCompactWindowSnapshot`),
  `core/src/session/turn.rs` (`auto_compact_token_status`),
  context-injection logic (`InitialContextInjection`).

## 功能需求 / Functional requirements
1. Token accounting/windows: `auto_compact_scope_tokens`, `*_limit`,
   `full_context_window_limit`, window ordinal; per-scope evaluation
   (`AutoCompactTokenLimitScope`).
2. Trigger evaluation: pre-turn, mid-turn, post-turn decisions on whether to
   compact, matching Codex thresholds.
3. Compaction execution: inline summarization (prompt templates) and remote compact
   endpoints (v1/v2) with inline fallback; produce a `compacted` rollout line with
   `replacement_history`; emit `ContextCompacted`.
4. Context injection: skill/plugin/tool/instruction fragments per turn and
   reinjection after compaction; workspace-root resolution; `Compact` op handling.

## 验收方案 / Acceptance criteria
- For sequences crossing the threshold (recorded token usages), `codexgo` triggers
  compaction at the same point and produces the same `compacted` line +
  `ContextCompacted` event as Codex.
- Post-compaction history (replacement + reinjected fragments) matches Codex.
- Remote-compact path and inline fallback both produce equivalent results.

## 风险与难点 / Risks
- Thresholds and scope arithmetic must be exact; off-by-one in token math shifts the
  trigger point and breaks differential tests.
- Summarization prompts must be copied verbatim.

## 非目标 / Non-goals
- The base turn loop (28); manual `/compact` UI (39).
