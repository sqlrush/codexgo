# 18 — State SQLite Database

| | |
|---|---|
| **Phase** | 5 — Persistence |
| **Status** | Not started |
| **Depends on** | 02, 17 |
| **Size** | L |
| **Drop-in critical** | ★★ (SQLite schema) |

## 目标 / Goal
Port `codex-state`: the SQLite-backed metadata mirror (threads/logs/jobs/memories)
that indexes rollout files and powers listing/search/backfill. Schema and migrations
must be compatible so `codexgo` and Codex can share the same database files.

## 源参考 / Source reference
- `reference-codex/codex-rs/state/src/` (sqlx models, `migrations/` ~33 `.sql`
  files, integrity check / recovery).
- `cli/src/state_db_recovery.rs`.

## 功能需求 / Functional requirements
1. Databases under `CODEX_HOME` (or `CODEX_SQLITE_HOME`): `state_5.sqlite`,
   `logs_2.sqlite`, `memories_1.sqlite`, `goals_1.sqlite`.
2. Apply the **identical migration set** (port the `.sql` files verbatim; manage
   versions in an `_sqlx_migrations`-compatible table).
3. Core tables/columns matching Codex: `threads` (id, rollout_path, timestamps,
   source, model_provider, cwd, title, archived, git sha/branch/url, tokens_used,
   has_user_event, agent_nickname/role/path, cli_version, reasoning_effort,
   thread_source), `logs`, `stage1_outputs`, `jobs`, `agent_jobs`,
   `agent_job_items`, `backfill_state`, `dynamic_tools`, `memories`.
4. Backfill orchestration: `ThreadMetadataBuilder` extracts metadata from rollout
   `session_meta`; `apply_rollout_item` ingestion; watermark + lease (900s);
   idempotent re-ingestion.
5. Ordered listing indices (`created_at DESC, id DESC`; `updated_at DESC, id DESC`).
6. Integrity check + recovery path (`codex state-db-recovery` equivalent).

## 验收方案 / Acceptance criteria
- A `state_5.sqlite` created by `codexgo` opens cleanly in Codex and vice versa;
  `PRAGMA user_version`/migration table agree (golden DB diff on schema).
- Ingesting the same rollout twice leaves the DB unchanged (idempotency).
- Listing/order queries return the same ordering as Codex for a seeded dataset.
- Integrity check detects a deliberately corrupted DB.

## 风险与难点 / Risks
- Use a pure-Go SQLite (`modernc.org/sqlite`) to keep the binary cgo-free; verify it
  reads sqlx-produced files and supports the needed PRAGMAs.
- `sqlx` compile-time query checks have no Go analog; use careful parameterized
  queries (optionally `sqlc` codegen).
- Concurrent write/lease semantics must match (single-writer + lease expiry).

## 非目标 / Non-goals
- Higher-level thread API (spec 19); memory feature semantics (spec 26).
