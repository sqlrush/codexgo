# 48 — Parity Validation & Release Packaging

| | |
|---|---|
| **Phase** | 11 — Hardening |
| **Status** | Not started |
| **Depends on** | all (00–47) |
| **Size** | M |
| **Drop-in critical** | ★★ (proves the whole thesis) |

## 目标 / Goal
Tie the project together: a comprehensive end-to-end parity suite that runs real
scenarios against both `codex` 0.136.0 and `codexgo`, plus cross-platform release
packaging (including the multitool symlink names). This is where "100% parity" is
demonstrated, not just claimed.

## 源参考 / Source reference
- The parity harness from spec 00.
- Upstream release tooling: `reference-codex/codex-rs/justfile`, `scripts/`,
  release asset names (`*-{aarch64,x86_64}-{apple-darwin,unknown-linux-gnu,
  pc-windows-msvc}`), `codex-cli/` npm packaging.

## 功能需求 / Functional requirements
1. **End-to-end differential suite**: scripted user scenarios (init project, ask for
   a change, run commands under sandbox, apply a patch, approve, compact, resume,
   use MCP, use a skill, exec headless) run against both binaries with a recorded
   model backend; diff observable outputs (files, rollout, JSONL, exit code,
   protocol traffic). Aggregate a **parity scorecard** per subsystem.
2. **Golden-file CI gate**: all per-spec golden tests + the differential suite run in
   CI; a single `DEVIATIONS.md` enumerates every accepted deviation with rationale.
3. **Performance checks**: startup time, turn latency overhead, memory — within
   agreed bounds of Codex.
4. **Release packaging**: cross-compile darwin/linux/windows × amd64/arm64; produce
   the multitool binary with the same arg0 alias names; checksums/signing; optional
   npm/brew distribution parity.
5. **Version-delta tracking**: a documented process to diff a newer Codex release
   against 0.136.0 and file roadmap deltas.

## 验收方案 / Acceptance criteria
- The differential suite passes (or every diff is an entry in `DEVIATIONS.md` with
  sign-off) — the parity scorecard shows all subsystems green.
- Drop-in test: point `codexgo` at a real `~/.codex` previously used by Codex; it
  reads config/auth/sessions and operates without migration.
- Release artifacts build for the full platform matrix and the multitool aliases
  dispatch correctly.

## 风险与难点 / Risks
- A recorded/mock model backend is essential for deterministic differential runs;
  build it early (overlaps spec 00/06).
- Some deviations are unavoidable (cosmetic TUI, fuzzy ranking, JSON key order);
  the goal is *zero unexplained* diffs, not literally zero diffs.

## 非目标 / Non-goals
- New features beyond 0.136.0 parity (tracked as post-parity roadmap deltas).
