# 40 — TUI Onboarding, Status & Resume Picker

| | |
|---|---|
| **Phase** | 9 — TUI |
| **Status** | Not started |
| **Depends on** | 37, 08 |
| **Size** | M |
| **Drop-in critical** | partial (UX parity) |

## 目标 / Goal
Port the remaining TUI screens: onboarding/login/trust flow, the status surface
(token usage, rate limits, account), and the resume/fork session picker.

## 源参考 / Source reference
- `reference-codex/codex-rs/tui/src/onboarding/` (login + trust screens),
  `tui/src/status/`, `status_indicator_widget.rs`, `key_hint.rs`,
  `tui/src/resume_picker/`.

## 功能需求 / Functional requirements
1. **Onboarding**: first-run login (ChatGPT OAuth / API key, spec 08), workspace
   trust prompt, default-sandbox setup; same steps/order as Codex.
2. **Status**: token usage, rate-limit display (from spec 06 headers), account info,
   model/provider; the running-task spinner (`status_indicator_widget`).
3. **Resume picker**: list sessions (spec 19) with names/metadata for
   `/resume`/`--resume`/`--fork`; select to resume/fork.
4. Key-hint footer reflecting context.

## 验收方案 / Acceptance criteria
- First-run flow reaches an authenticated, ready state via the same steps as Codex
  (manual/scripted walkthrough + snapshot of each screen).
- Status surface shows token/rate-limit/account values consistent with engine state.
- Resume picker lists the same sessions Codex would and resumes the correct rollout.

## 风险与难点 / Risks
- Onboarding ties together auth (08), config (04), sandbox setup (12); sequence and
  persisted side effects must match.

## 非目标 / Non-goals
- The auth mechanics themselves (08); chat surface (37).
