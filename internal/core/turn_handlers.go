package core

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// handleUserInput starts a regular user-driven turn for an Op::UserInput
// submission. It builds the turn context (applying any thread-settings override
// and the turn-local final-output schema / environments), then spawns a regular
// task that runs the turn loop against the model client. Mirrors the Rust
// `user_input_or_turn_inner` (no-active-turn / new-turn subset).
//
// STUB: steering input into an already-running turn (steer_input), realtime text
// mirroring, MCP-server refresh, additional-context merging, and the
// thread-settings-applied echo are deferred to the relevant area agents. If a
// turn is already running, this submission replaces it (spawnTask cancels the
// previous task).
func handleUserInput(sess *Session, subID string, op protocol.Op) {
	update := userInputSettingsUpdate(op)

	tc, err := newTurnContext(sess.ctx, sess, subID, update)
	if err != nil {
		emitTurnContextError(sess, subID, err)
		return
	}

	input := turnInputFromOp(op)
	spawnTask(sess, tc, TaskKindRegular, func(ctx context.Context) *string {
		return runTurn(ctx, sess, tc, input)
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
