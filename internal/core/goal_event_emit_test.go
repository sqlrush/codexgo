package core

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/ext/goal"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/state"
)

// sessionGoalEventSink is the test analogue of the headless host's goal event
// sink: it routes a pre-built extension event straight to the session's event
// stream via Session.EmitEvent, exactly as the wired cli goalEventSink does once
// it has resolved the session. It verifies the sink contract end-to-end without
// the ThreadManager late-binding plumbing (covered by the cli wiring).
type sessionGoalEventSink struct {
	sess *Session
}

func (s sessionGoalEventSink) Emit(event protocol.Event) {
	s.sess.EmitEvent(event)
}

// TestRunSamplingRequestCreateGoalEmitsThreadGoalUpdated drives a create_goal
// function call through a full sampling request and asserts the goal extension's
// thread_goal_updated accounting event reaches the session event stream — the
// behavior the headless goal event sink wires (Goal B). The event correlation id
// is the originating tool call id, matching the Rust GoalEventEmitter.
func TestRunSamplingRequestCreateGoalEmitsThreadGoalUpdated(t *testing.T) {
	runtime, err := state.InitRuntime(context.Background(), t.TempDir(), "test-provider")
	if err != nil {
		t.Fatalf("init state runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })

	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evFunctionCall("call-goal", "create_goal", `{"objective":"finish the port"}`),
		evCompleted(true, nil),
	}})

	sess, evCh, cancel := turnTestSession(t, mc, nil)
	defer cancel()

	threadID := protocol.NewThreadID("00000000-0000-0000-0000-000000000777")
	goalTools := goal.NewToolExecutors(
		threadID,
		goal.NewStateRuntimeBridge(runtime),
		sessionGoalEventSink{sess: sess},
		nil,
	)
	router, err := BuiltinToolRouter(BuiltinToolDeps{
		Exec:      &mockExecService{},
		GoalTools: goalTools,
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	// Swap the real goal router in (turnTestSession seeds a nil router).
	sess.services.ToolRouter = router

	tc, _ := newTurnContext(sess.ctx, sess, "turn-goal", nil)
	installActiveTurn(sess, tc)

	if _, rerr := runSamplingRequest(sess.ctx, sess, tc); rerr != nil {
		t.Fatalf("runSamplingRequest: %v", rerr)
	}
	cancel()

	events := drainEvents(evCh)
	var updated *protocol.ThreadGoalUpdatedEvent
	var updatedID string
	for _, ev := range events {
		if ev.Msg.Type == protocol.EventMsgKindThreadGoalUpdated {
			updated = ev.Msg.ThreadGoalUpdated
			updatedID = ev.ID
			break
		}
	}
	if updated == nil {
		t.Fatalf("no thread_goal_updated event on the session stream; saw %d events", len(events))
	}
	if updatedID != "call-goal" {
		t.Fatalf("thread_goal_updated event id = %q, want the originating call id call-goal", updatedID)
	}
	if updated.Goal.Objective != "finish the port" {
		t.Fatalf("thread_goal_updated objective = %q, want %q", updated.Goal.Objective, "finish the port")
	}
	if updated.ThreadID != threadID {
		t.Fatalf("thread_goal_updated thread id = %v, want %v", updated.ThreadID, threadID)
	}
}
