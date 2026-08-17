package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// handleUserInput admits an Op::UserInput submission, mirroring the Rust
// `user_input_or_turn` + `user_input_or_turn_inner` (0.147; spec 50 D0.2):
// the settings override is applied, then the input is STEERED into the running
// regular turn when there is one (it becomes pending input consumed at the next
// sampling boundary) and otherwise starts a new regular turn. A running review
// or compaction turn cannot be steered; that submission is rejected with a
// BadRequest error event instead of silently replacing the turn. The admission
// outcome is delivered to any [Codex.SubmitUserMessage] waiter.
//
// STUB: realtime text mirroring, MCP-server refresh, additional-context merging,
// and the thread-settings-applied echo are deferred.
func handleUserInput(sess *Session, subID string, op protocol.Op, clientUserMessageID *string) {
	admission, err := admitUserInput(sess, subID, op, clientUserMessageID)
	_, admissions := sess.queueState()
	admissions.complete(subID, admission, err)
}

// admitUserInput is the body of handleUserInput; it returns how the message
// was admitted or the error that rejected it (already reported as an event).
func admitUserInput(sess *Session, subID string, op protocol.Op, clientUserMessageID *string) (UserMessageAdmission, error) {
	update := userInputSettingsUpdate(op)

	tc, err := newTurnContext(sess.ctx, sess, subID, update)
	if err != nil {
		emitTurnContextError(sess, subID, err)
		return UserMessageAdmission{}, err
	}

	turnID, serr := sess.SteerInput(op.Items, "", clientUserMessageID)
	switch {
	case serr == nil:
		return UserMessageAdmission{Kind: UserMessageAdmissionSteered, TurnID: turnID}, nil
	case IsSteerInputError(serr, SteerInputNoActiveTurn):
		input := turnInputFromOp(op)
		spawnTask(sess, tc, TaskKindRegular, func(ctx context.Context) *string {
			return runTurn(ctx, sess, tc, input)
		})
		return UserMessageAdmission{Kind: UserMessageAdmissionStarted, TurnID: subID}, nil
	default:
		var se *SteerInputError
		if errors.As(serr, &se) {
			sess.SendEvent(subID, protocol.EventMsg{Type: protocol.EventMsgKindError, Error: se.ToErrorEvent()})
		}
		return UserMessageAdmission{}, fmt.Errorf("core: failed to admit user message: %w", serr)
	}
}

// handleInterAgentCommunication queues an inter-agent message in the mailbox
// and, when it asks to trigger a turn, lets the pending-work scheduler start a
// regular turn on an idle session. Mirrors the Rust `inter_agent_communication`
// handler + `maybe_start_turn_for_pending_work_with_sub_id`.
func handleInterAgentCommunication(sess *Session, subID string, op protocol.Op) {
	if op.Communication == nil {
		return
	}
	q, _ := sess.queueState()
	// STUB: the parent turn id of trigger-turn mail (upstream Submission
	// parent_turn_id) is not threaded through codexgo submissions yet.
	q.EnqueueMailboxCommunication(*op.Communication, nil)
	if op.Communication.TriggerTurn {
		maybeStartTurnForPendingWork(sess, subID)
	}
}

// maybeStartTurnForPendingWork starts a regular turn with EMPTY input when the
// session is idle and pending work (steer / trigger-turn mail) is queued; the
// turn loop drains the queue before its first sampling request. A running turn
// picks the work up itself. Mirrors `maybe_start_turn_for_pending_work_with_sub_id`.
func maybeStartTurnForPendingWork(sess *Session, subID string) {
	if at := sess.ActiveTurn(); at != nil && at.Task != nil {
		return
	}
	q, _ := sess.queueState()
	if !q.HasPendingInput(nil) {
		return
	}
	tc, err := newTurnContext(sess.ctx, sess, subID, nil)
	if err != nil {
		emitTurnContextError(sess, subID, err)
		return
	}
	spawnTask(sess, tc, TaskKindRegular, func(ctx context.Context) *string {
		return runTurn(ctx, sess, tc, nil)
	})
}

// handleCompact starts an inline compaction turn for an Op::Compact submission.
// It builds a default turn context and spawns a compact task.
//
// STUB: the actual summarization request, history replacement, and
// ContextCompacted event are owned by the compaction area agent. This handler
// wires the lifecycle (turn context + task spawn + terminal event) so the
// submission is acknowledged; the compaction body is a no-op placeholder that
// records the compaction prompt is available via tc.CompactPromptText().
func handleCompact(sess *Session, subID string) {
	tc, err := newTurnContext(sess.ctx, sess, subID, nil)
	if err != nil {
		emitTurnContextError(sess, subID, err)
		return
	}
	spawnTask(sess, tc, TaskKindCompact, func(ctx context.Context) *string {
		// STUB: inline-compaction body. The compaction area agent fills in the
		// summarization sampling request and history replacement here.
		return nil
	})
}

// handleReview starts a review turn for an Op::Review submission. It builds a
// default turn context and spawns a review task.
//
// STUB: spawning the dedicated child review thread (spawn_review_thread),
// resolving the review request/prompt, and the EnteredReviewMode/ExitedReviewMode
// events are owned by the review area agent. This handler wires the lifecycle so
// the submission is acknowledged; the review body is a no-op placeholder.
func handleReview(sess *Session, subID string, op protocol.Op) {
	tc, err := newTurnContext(sess.ctx, sess, subID, nil)
	if err != nil {
		emitTurnContextError(sess, subID, err)
		return
	}
	spawnTask(sess, tc, TaskKindReview, func(ctx context.Context) *string {
		// STUB: review body. The review area agent fills in the resolved review
		// request and child-thread orchestration here.
		return nil
	})
}

// userInputSettingsUpdate derives the settings update to apply when starting a
// user-input turn. It always forwards the turn-local final-output schema and the
// optional environments override; thread-settings overrides are mapped through
// the existing settingsUpdateFromOverrides helper when present. Returns nil when
// there is nothing to override.
func userInputSettingsUpdate(op protocol.Op) *SessionSettingsUpdate {
	var update SessionSettingsUpdate
	hasUpdate := false

	if op.ThreadSettings != (protocol.ThreadSettingsOverrides{}) {
		update = settingsUpdateFromOverrides(op.ThreadSettings)
		hasUpdate = true
	}

	// final_output_json_schema is a turn-local double-option override: the Op
	// always carries it (may be null), so we always forward it.
	schema := op.FinalOutputJSONSchema
	var schemaBytes []byte
	if len(schema) > 0 && string(schema) != "null" {
		schemaBytes = append([]byte(nil), schema...)
	}
	update.FinalOutputJSONSchema = &schemaBytes
	hasUpdate = true

	if op.Environments != nil {
		envs := append([]protocol.TurnEnvironmentSelection(nil), (*op.Environments)...)
		update.Environments = &envs
		hasUpdate = true
	}

	if !hasUpdate {
		return nil
	}
	return &update
}

// turnInputFromOp converts an Op::UserInput's items into the turn's input list.
// Mirrors the Rust task-input construction for the no-active-turn path
// (additional-context injection is deferred).
func turnInputFromOp(op protocol.Op) []turnInput {
	if len(op.Items) == 0 {
		return nil
	}
	return []turnInput{{
		UserContent: op.Items,
	}}
}

// emitTurnContextError emits an Error event when a turn context cannot be built.
// Mirrors the Rust `new_turn_with_sub_id` error emission.
func emitTurnContextError(sess *Session, subID string, err error) {
	sess.SendEvent(subID, protocol.EventMsg{
		Type: protocol.EventMsgKindError,
		Error: &protocol.ErrorEvent{
			Message: err.Error(),
		},
	})
}
