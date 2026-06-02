# 17 — Rollout JSONL Persistence

| | |
|---|---|
| **Phase** | 5 — Persistence |
| **Status** | Not started |
| **Depends on** | 02 |
| **Size** | M |
| **Drop-in critical** | ★★ (session file format) |

## 目标 / Goal
Port `codex-rollout` (+ `rollout-trace`): the JSONL session/rollout files that record
a conversation and enable resume/fork/replay. These files are shared with Codex and
must be byte-compatible.

## 源参考 / Source reference
- `reference-codex/codex-rs/rollout/src/` (`RolloutRecorder`, `RolloutLine`/
  `RolloutItem`, persistence `policy`).
- `reference-codex/codex-rs/rollout-trace/src/`.

## 功能需求 / Functional requirements
1. File location/naming: `~/.codex/sessions/rollout-<YYYY-MM-DDTHH-MM-SS>-<uuid>.jsonl`;
   archived under `~/.codex/archived_sessions/`. Session index (`sessions/index.jsonl`
   name↔id mapping) where Codex maintains one.
2. Per-line `RolloutLine{ timestamp, <tagged item> }` record types:
   `session_meta` (header line 1: id, timestamp, cwd, originator, cli_version,
   source, model_provider, base_instructions, dynamic_tools, forked_from_id,
   agent_nickname/role/path, git info), `response_item`, `event_msg`, `compacted`
   (message + replacement_history), `turn_context` (per real user turn).
3. Persistence policy: `Limited` (default; excludes compaction-trigger/Other) vs
   `Extended` (truncates `ExecCommandEnd.aggregated_output` to 10KB; clears
   stdout/stderr/formatted_output). Memories skip developer-role messages.
4. Async background writer (mpsc-style): append `RolloutLine` to the active file;
   cheap clones sharing one writer; `append_rollout_item_to_path` for out-of-band.
5. Resume/replay: read a rollout into the in-memory conversation, restoring the
   `turn_context` baseline and history.

## 验收方案 / Acceptance criteria
- Golden round-trip: a Codex-produced rollout file read by `codexgo` and re-written
  is byte-identical (after canonicalizer for JSON key order).
- A `codexgo`-produced rollout is resumable by real Codex (differential resume).
- `Limited` vs `Extended` policy produces the same included/excluded lines and the
  same 10KB truncation as Codex.
- `session_meta` is always line 1 and carries `forked_from_id` on forks.

## 风险与难点 / Risks
- JSON key ordering: `encoding/json` differs from `serde`; define the canonicalizer
  and, where Codex relies on order, emit a deterministic field order.
- The exact set of persisted item types must track 0.136.0.

## 非目标 / Non-goals
- The SQLite metadata mirror (spec 18); thread organization (spec 19).
