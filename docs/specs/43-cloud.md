# 43 — Cloud Features

| | |
|---|---|
| **Phase** | 10 — CLI & peripheral |
| **Status** | Not started |
| **Depends on** | 06, 08 |
| **Size** | L |
| **Drop-in critical** | partial (backend API + requirements cache) |

## 目标 / Goal
Port `codex-cloud-tasks(-client)`, `codex-cloud-requirements`, `codex-backend-client`,
and `codex-chatgpt`: browsing/applying remote Codex Cloud tasks and enforcing
workspace-managed requirements for Business/Enterprise accounts.

## 源参考 / Source reference
- `reference-codex/codex-rs/cloud-tasks/src/`, `cloud-tasks-client/src/`,
  `cloud-tasks-mock-client/src/`.
- `cloud-requirements/src/` (`CloudRequirementsLoader`), `backend-client/src/`,
  `chatgpt/src/`.

## 功能需求 / Functional requirements
1. **cloud-tasks** (`codex cloud` / `cloud-tasks`): list/fetch tasks from the
   ChatGPT backend (`https://chatgpt.com/backend-api`, `CODEX_CLOUD_TASKS_BASE_URL`
   override), view diffs, apply locally, report apply status. Types: `TaskId`,
   `TaskStatus`, `TaskSummary`, `ApplyOutcome`, `DiffSummary`. Mock mode via
   `CODEX_CLOUD_TASKS_MODE=mock`.
2. **cloud-requirements**: fetch `requirements.toml` for eligible accounts; 5-retry
   backoff, 15s timeout, HMAC-SHA256-validated `cloud-requirements-cache.json`,
   30min TTL / 5min refresh, **fail-closed** for eligible accounts; hook into the
   config builder (spec 04).
3. **backend-client**: REST client for `backend-api` endpoints
   (`CodeTaskDetailsResponse`, `ConfigFileResponse`, task list, sibling turns).
4. **chatgpt**: ChatGPT backend session/workspace access.

## 验收方案 / Acceptance criteria
- Cloud-tasks flow (auth → list → fetch diff → apply → report) works against the mock
  client and matches Codex's apply outcome on fixtures.
- `requirements.toml` cache file format (incl. HMAC) is byte-compatible (golden);
  TTL/refresh/fail-closed behavior matches.
- Backend request/response shapes match captures.

## 风险与难点 / Risks
- Real backend requires ChatGPT auth + network; rely on the mock client + captured
  responses for hermetic tests.
- Fail-closed logic for enterprise accounts must be exact (security-relevant).

## 非目标 / Non-goals
- The TUI for cloud (Codex's `cloud-tasks` uses its own ratatui screens — port under
  Phase 9 follow-up if needed); voice/realtime (47).
