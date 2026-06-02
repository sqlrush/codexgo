# 30 — Core: Approvals & Guardian

| | |
|---|---|
| **Phase** | 7 — Core engine |
| **Status** | Not started |
| **Depends on** | 28 |
| **Size** | M |
| **Drop-in critical** | partial (approval protocol) |

## 目标 / Goal
Port the approval/security flow of `codex-core`: routing exec/patch/permission/
elicitation requests to the client, applying `AskForApproval` policy, and integrating
the guardian reviewer.

## 源参考 / Source reference
- `reference-codex/codex-rs/core/src/codex_delegate.rs` (approval routing, timeouts).
- `core/src/guardian/review_session.rs` (guardian integration),
  `core/src/state/turn.rs` (`pending_approvals`, `notify_approval`,
  `PendingRequestPermissions`).

## 功能需求 / Functional requirements
1. Approval requests: emit `ExecApprovalRequest` / `ApplyPatchApprovalRequest` /
   `RequestPermissions` / `ElicitationRequest` with deterministic approval IDs;
   await the matching `Op` response (`ExecApproval`, `PatchApproval`,
   `RequestPermissionsResponse`, `ResolveElicitation`).
2. `AskForApproval` × sandbox policy decisioning (interplay with spec 12) to decide
   when approval is required vs auto-approved/sandboxed.
3. `ReviewDecision` model (Approved / Denied / ApprovedForSession / …) and
   session-scoped approval memory.
4. Approval timeouts (default ~30s) with the same fallback behavior.
5. Guardian: route risky operations through the reviewer (spec 26), surface
   `GuardianWarning`/assessment, and gate accordingly.

## 验收方案 / Acceptance criteria
- Approval request/response round-trips use the same event/op shapes and IDs as
  Codex (differential).
- `ApprovedForSession` suppresses subsequent prompts for matching operations, like
  Codex.
- Timeout behavior matches (same default, same outcome on no response).
- Guardian gating fires on the same operations for a labeled set.

## 风险与难点 / Risks
- Deterministic approval-ID generation must match for event/response correlation.
- Timeout + cancellation interplay with the turn loop (28) must be race-free.

## 非目标 / Non-goals
- The TUI approval overlay (38) and headless auto-approval policies beyond core.
