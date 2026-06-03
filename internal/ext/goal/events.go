package goal

import (
	"github.com/sqlrush/codexgo/internal/ext/extensionapi"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// goalEventEmitter emits ThreadGoalUpdated events via the host event sink.
// Mirrors the Rust `GoalEventEmitter`.
type goalEventEmitter struct {
	sink extensionapi.ExtensionEventSink
}

// newGoalEventEmitter builds an emitter over a host event sink. Mirrors Rust
// `GoalEventEmitter::new`.
func newGoalEventEmitter(sink extensionapi.ExtensionEventSink) goalEventEmitter {
	if sink == nil {
		sink = extensionapi.NoopExtensionEventSink{}
	}
	return goalEventEmitter{sink: sink}
}

// threadGoalUpdated emits a thread-goal-updated event correlated with eventID.
// Mirrors Rust `GoalEventEmitter::thread_goal_updated`.
func (e goalEventEmitter) threadGoalUpdated(eventID string, turnID *string, goal protocol.ThreadGoal) {
	e.sink.Emit(protocol.Event{
		ID: eventID,
		Msg: protocol.EventMsg{
			Type: protocol.EventMsgKindThreadGoalUpdated,
			ThreadGoalUpdated: &protocol.ThreadGoalUpdatedEvent{
				ThreadID: goal.ThreadID,
				TurnID:   turnID,
				Goal:     goal,
			},
		},
	})
}
