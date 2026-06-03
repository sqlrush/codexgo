package core

import (
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// submissionCounter backs newSubmissionID. It is process-global because ids must
// be unique across all sessions in a process.
var submissionCounter atomic.Uint64

// runSubmissionLoop drains the submission queue and dispatches each op to its
// handler, preserving FIFO ordering and submission_id/turn_id correlation. It is
// the Go analogue of the Rust `submission_loop`.
//
// The loop runs on a dedicated goroutine until an [protocol.OpShutdown] is
// received or the submission channel/session context closes. On exit it closes
// loopDone (so waiters unblock) and the event queue (so consumers see EOF).
func (c *Codex) runSubmissionLoop() {
	defer close(c.loopDone)
	defer close(c.rxEvent)
	defer c.session.cancel()

	sess := c.session
	for {
		select {
		case sub, ok := <-c.txSub:
			if !ok {
				// Channel closed without explicit shutdown: still tear down.
				teardownSession(sess)
				return
			}
			if c.dispatch(sub) {
				// Shutdown requested: stop draining.
				return
			}
		case <-sess.ctx.Done():
			teardownSession(sess)
			return
		}
	}
}

// dispatch routes one submission to its handler. It returns true when the op
// requests shutdown (terminating the loop). Mirrors the Rust submission_loop
// match arms (turn-running subset).
func (c *Codex) dispatch(sub protocol.Submission) (shutdown bool) {
	sess := c.session
	switch sub.Op.Type {
	case protocol.OpShutdown:
		handleShutdown(sess, sub.ID)
		return true

	case protocol.OpInterrupt:
		handleInterrupt(sess)
		return false

	case protocol.OpUserInput:
		handleUserInput(sess, sub.ID, sub.Op)
		return false

	case protocol.OpExecApproval:
		handleApproval(sess, sub.Op, true)
		return false

	case protocol.OpPatchApproval:
		handleApproval(sess, sub.Op, false)
		return false

	case protocol.OpCompact:
		handleCompact(sess, sub.ID)
		return false

	case protocol.OpReview:
		handleReview(sess, sub.ID, sub.Op)
		return false

	case protocol.OpThreadSettings:
		handleThreadSettings(sess, sub.ID, sub.Op)
		return false

	default:
		// Unknown / deferred ops are ignored (the Op enum is non-exhaustive to
		// allow forward-compatible extensions).
		//
		// STUB: realtime_conversation_*, inter_agent_communication,
		// resolve_elicitation, request_*_response, dynamic_tool_response,
		// refresh_mcp_servers, reload_user_config, set_thread_memory_mode,
		// thread_rollback, approve_guardian_denied_action, and
		// run_user_shell_command are handled by area agents.
		return false
	}
}

// handleShutdown interrupts any active turn, fires the session-end hook, tears
// down managers, and emits ShutdownComplete. Mirrors the Rust `shutdown`.
func handleShutdown(sess *Session, subID string) {
	handleInterrupt(sess)
	teardownSession(sess)
	sess.SendEvent(subID, protocol.EventMsg{Type: protocol.EventMsgKindShutdownComplete})
}

// teardownSession runs session-end lifecycle: fire the session-end hook and shut
// down the MCP manager. It is idempotent-safe to call once on loop exit.
func teardownSession(sess *Session) {
	if h := sess.services.HooksEngine; h != nil {
		_ = h.Fire(sess.ctx, HookSessionEnd, nil)
	}
	if m := sess.services.McpManager; m != nil {
		_ = m.Shutdown(sess.ctx)
	}
}

// handleInterrupt cancels the active turn, if any, mirroring the Rust
// `interrupt_task`. The turn runner observes the cancellation and emits a
// TurnAborted event.
func handleInterrupt(sess *Session) {
	at := sess.ActiveTurn()
	if at == nil || at.Task == nil {
		return
	}
	at.Task.Cancel()
	at.State.ClearPendingWaiters()
}

// handleApproval resolves a pending exec/patch approval by delivering the
// decision to the waiting tool. Mirrors the Rust `exec_approval` / `patch_approval`.
func handleApproval(sess *Session, op protocol.Op, isExec bool) {
	at := sess.ActiveTurn()
	if at == nil {
		return
	}
	if op.Decision == nil {
		return
	}
	at.State.ResolvePendingApproval(op.ID, *op.Decision)
}

// handleThreadSettings applies a persistent thread-settings override and emits a
// ThreadSettingsApplied (or Error) event. Mirrors the Rust
// `update_thread_settings` (reduced surface).
func handleThreadSettings(sess *Session, subID string, op protocol.Op) {
	update := settingsUpdateFromOverrides(op.ThreadSettings)
	cfg := sess.UpdateConfiguration(update)
	sess.SendEvent(subID, protocol.EventMsg{
		Type: protocol.EventMsgKindThreadSettingsApplied,
		ThreadSettingsApplied: &protocol.ThreadSettingsAppliedEvent{
			ThreadSettings: threadSettingsSnapshot(cfg),
		},
	})
}

// settingsUpdateFromOverrides maps the wire ThreadSettingsOverrides onto a core
// [SessionSettingsUpdate]. Only the turn-running-relevant fields are mapped.
//
// STUB: cwd normalization, permission-profile projection, sandbox-policy
// derivation, and double-option effort/service-tier are owned by the
// config/permissions area; here we forward the simple overrides.
func settingsUpdateFromOverrides(o protocol.ThreadSettingsOverrides) SessionSettingsUpdate {
	u := SessionSettingsUpdate{
		ApprovalPolicy:      o.ApprovalPolicy,
		ApprovalsReviewer:   o.ApprovalsReviewer,
		WindowsSandboxLevel: o.WindowsSandboxLevel,
		CollaborationMode:   o.CollaborationMode,
		Personality:         o.Personality,
		AppServerClientName: nil,
	}
	if o.Summary != nil {
		u.ReasoningSummary = o.Summary
	}
	if o.Cwd != nil {
		cwd := string(*o.Cwd)
		u.Cwd = &cwd
	}
	if o.WorkspaceRoots != nil {
		roots := make([]string, 0, len(*o.WorkspaceRoots))
		for _, r := range *o.WorkspaceRoots {
			roots = append(roots, string(r))
		}
		u.WorkspaceRoots = &roots
	}
	return u
}

// threadSettingsSnapshot projects the effective configuration into the wire
// ThreadSettingsSnapshot. Permission-profile / reasoning fields are left as
// their raw (omitted) form for the permissions/config area to fill in.
func threadSettingsSnapshot(cfg SessionConfiguration) protocol.ThreadSettingsSnapshot {
	return protocol.ThreadSettingsSnapshot{
		Model:             cfg.Model(),
		ModelProviderID:   cfg.ProviderID,
		ServiceTier:       cfg.ServiceTier,
		ApprovalPolicy:    cfg.ApprovalPolicy,
		ApprovalsReviewer: cfg.ApprovalsReviewer,
		Cwd:               protocol.AbsolutePath(cfg.Cwd),
		Personality:       cfg.Personality,
		CollaborationMode: cfg.CollaborationMode,
	}
}
