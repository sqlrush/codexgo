# 20 — Git, File Search & File Watcher

| | |
|---|---|
| **Phase** | 5 — Persistence |
| **Status** | Not started |
| **Depends on** | 01 |
| **Size** | M |
| **Drop-in critical** | partial |

## 目标 / Goal
Port `codex-git-utils`, `codex-file-search`, and `codex-file-watcher`: git
introspection/diffing, fuzzy file search (`@mention`), and filesystem watching.

## 源参考 / Source reference
- `reference-codex/codex-rs/git-utils/src/` (gix-based: `collect_git_info`,
  `apply_git_patch`, repo root, diffs, recent commits, `GitInfo`).
- `reference-codex/codex-rs/file-search/src/` (nucleo matcher, `ignore` walk,
  `FileMatch`).
- `reference-codex/codex-rs/file-watcher/src/` (notify-based subscriber/coalescing).

## 功能需求 / Functional requirements
1. **git-utils** (via `go-git`): HEAD sha, branch, remote URL → `GitInfo` (embedded
   in rollout `session_meta`); has-changes, diff-to-remote, recent commits, apply
   patch with staged-path tracking, repo-root discovery.
2. **file-search** (`@mention`): walk respecting `.gitignore` (`ignore` analog),
   fuzzy match + score + highlight indices; background/streaming query injection;
   case/normalization options. Used by app-server `fuzzyFileSearch` and the TUI.
3. **file-watcher**: subscribe to paths, coalesce rapid events with debounce,
   ref-count watches, fallback watch on missing targets (parent create/delete);
   backs app-server `fs/watch`.

## 验收方案 / Acceptance criteria
- `GitInfo` collected for a repo matches Codex (sha/branch/url) for the same HEAD.
- File-search results for a query set match Codex's path set; ranking parity is
  best-effort with documented deviations.
- Watcher delivers create/modify/delete/rename events for a scripted sequence with
  equivalent coalescing.

## 风险与难点 / Risks
- `nucleo` ranking is hard to match exactly — document scoring deviations; keep the
  *result set* identical even if order differs slightly.
- `go-git` lacks some `gix` features; verify diff/apply parity on real repos; fall
  back to invoking system `git` only if a feature is truly unavailable (note as a
  deviation, since the project prefers native).

## 非目标 / Non-goals
- The TUI popup that consumes search (spec 38).
