package core

import (
	"context"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// Rollout persistence hooks (Rust session/mod.rs `record_conversation_items`,
// `record_into_history`, `persist_rollout_items`, and the `send_event`
// persistence branch). Every write goes through the injected
// [SessionServices.RolloutRecorder]; a nil recorder means the host has no
// persistence for the thread and the hooks are no-ops. Recorder errors are
// swallowed like the Rust recorder (which logs and continues): a turn must not
// fail because durable storage hiccuped.

// eventPersistenceMode reports the session's event persistence mode
// (SessionConfiguration.PersistExtendedHistory → Extended, else Limited).
func (s *Session) eventPersistenceMode() rollout.EventPersistenceMode {
	// Read without the state lock: SendEvent may run while a caller holds it,
	// and the mode is fixed at spawn (Rust reads it from the immutable config).
	return s.eventPersistence
}

// persistRolloutItems appends items to the thread's rollout after applying the
// persistence policy (filter + sanitize). Mirrors Rust `persist_rollout_items`.
func (s *Session) persistRolloutItems(ctx context.Context, items []rollout.RolloutItem) {
	if s.services.RolloutRecorder == nil || len(items) == 0 {
		return
	}
	persisted := rollout.PersistedRolloutItems(items, s.eventPersistenceMode())
	if len(persisted) == 0 {
		return
	}
	_ = s.services.RolloutRecorder.Record(ctx, persisted)
}

// persistResponseItems persists response items produced or consumed by the
// model (Rust `record_conversation_items` persistence half).
func (s *Session) persistResponseItems(items []protocol.ResponseItem) {
	if s.services.RolloutRecorder == nil || len(items) == 0 {
		return
	}
	rolloutItems := make([]rollout.RolloutItem, 0, len(items))
	for _, item := range items {
		rolloutItems = append(rolloutItems, rollout.NewResponseItem(item))
	}
	s.persistRolloutItems(s.ctx, rolloutItems)
}

// persistEventMsg persists an emitted event when the policy keeps it (Rust
// `send_event` → `should_persist_event_msg`).
func (s *Session) persistEventMsg(msg protocol.EventMsg) {
	if s.services.RolloutRecorder == nil {
		return
	}
	s.persistRolloutItems(s.ctx, []rollout.RolloutItem{rollout.NewEventMsgItem(msg)})
}

// persistTurnContext records the turn's model-visible context at turn start
// (Rust `run_task` → `persist_rollout_items(TurnContext)`), so resume/fork can
// rebuild the request shape without replaying every event.
func (s *Session) persistTurnContext(ctx context.Context, tc *TurnContext) {
	if s.services.RolloutRecorder == nil || tc == nil {
		return
	}
	item, err := tc.ToTurnContextItem()
	if err != nil {
		return
	}
	s.persistRolloutItems(ctx, []rollout.RolloutItem{rollout.NewTurnContextItem(item)})
}

// eventPersistenceFor maps the session configuration to its persistence mode.
func eventPersistenceFor(cfg SessionConfiguration) rollout.EventPersistenceMode {
	if cfg.PersistExtendedHistory {
		return rollout.EventPersistenceModeExtended
	}
	return rollout.EventPersistenceModeLimited
}
