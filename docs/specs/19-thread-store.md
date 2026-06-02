# 19 — Thread Store, History & Agent Graph

| | |
|---|---|
| **Phase** | 5 — Persistence |
| **Status** | Not started |
| **Depends on** | 17, 18 |
| **Size** | M |
| **Drop-in critical** | ★ (history.jsonl + thread semantics) |

## 目标 / Goal
Port `codex-thread-store`, `codex-message-history`, `codex-agent-graph-store`, and the
external-agent session importers: the storage-neutral thread API, the cross-session
message history file, and the spawned-agent topology.

## 源参考 / Source reference
- `reference-codex/codex-rs/thread-store/src/` (`ThreadStore` trait,
  `LocalThreadStore`/`InMemoryThreadStore`, params/result types).
- `reference-codex/codex-rs/message-history/src/` (`~/.codex/history.jsonl`).
- `agent-graph-store/`, `external-agent-sessions/`, `external-agent-migration/`.

## 功能需求 / Functional requirements
1. `ThreadStore` trait with create/read/list/append/archive/resume/search; returns
   `StoredThread`, paginated `ItemPage`, `StoredTurnItemsView`. `LocalThreadStore`
   reads/writes rollout JSONL (spec 17) + syncs metadata into state DB (spec 18);
   `InMemoryThreadStore` for tests.
2. Fork creates a new rollout file with `forked_from_id`; resume reuses path;
   archive flips `archived`/`archived_at`.
3. **message-history**: `~/.codex/history.jsonl`, one record per line
   `{session_id, ts (unix s), text}`; atomic `O_APPEND` writes; soft-cap rotation at
   80% of `max_bytes`; tail reads.
4. **agent-graph-store**: parent/child thread spawn edges.
5. External-agent session detection/import to rollout format with an import ledger.

## 验收方案 / Acceptance criteria
- `history.jsonl` written by `codexgo` is byte-compatible with Codex (golden);
  concurrent appends don't corrupt or interleave.
- Fork/resume/archive produce the same files + DB state as Codex (differential).
- Thread search/list parity for a seeded dataset.
- Rotation triggers at the same soft cap.

## 风险与难点 / Risks
- `O_APPEND` atomicity is platform-dependent; keep records small or guard writes.
- Metadata sync ordering between rollout and state DB must match to avoid drift.

## 非目标 / Non-goals
- The memory feature surface (spec 26); fuzzy thread ranking internals (spec 20).
