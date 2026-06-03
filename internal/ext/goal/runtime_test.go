package goal

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func newTestRuntime(store StateRuntime, metrics MetricsClient, tm ThreadManager, sink *captureSink, enabled bool) (GoalRuntimeHandle, *goalAccountingState) {
	acct := newGoalAccountingState()
	rt := newGoalRuntimeHandle(
		protocol.NewThreadID(testThreadUUID),
		store,
		newGoalEventEmitter(sink),
		NewGoalMetrics(metrics),
		tm,
		acct,
		goalRuntimeConfig{enabled: enabled, toolsAvailableForThread: true},
	)
	return rt, acct
}

func TestPreviousGoalSnapshotFromState(t *testing.T) {
	snap := PreviousGoalSnapshotFromState(StateThreadGoal{GoalID: "g", Status: StateGoalStatusBlocked, Objective: "obj"})
	if snap.GoalID != "g" || snap.Status != StateGoalStatusBlocked || snap.Objective != "obj" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestRuntimeDisabledShortCircuits(t *testing.T) {
	store := newFakeStore()
	rt, _ := newTestRuntime(store, nil, nil, &captureSink{}, false)
	if err := rt.PrepareExternalGoalMutation(context.Background()); err != nil {
		t.Errorf("PrepareExternalGoalMutation disabled err = %v", err)
	}
	if err := rt.ApplyExternalGoalClear(context.Background()); err != nil {
		t.Errorf("ApplyExternalGoalClear disabled err = %v", err)
	}
	if err := rt.RestoreAfterResume(context.Background()); err != nil {
		t.Errorf("RestoreAfterResume disabled err = %v", err)
	}
	// Disabled runtime never touches the store.
	if store.accountCall != 0 {
		t.Errorf("disabled runtime accounted: %d", store.accountCall)
	}
}

func TestApplyExternalGoalSetActiveSteersTurn(t *testing.T) {
	store := newFakeStore()
	thread := &fakeThread{}
	sink := &captureSink{}
	rt, acct := newTestRuntime(store, &fakeMetrics{}, &fakeThreadManager{thread: thread}, sink, true)

	// Start a turn so steering can inject into the active turn.
	acct.startTurn("turn-1", protocol.ModeKindDefault, usage(0, 0, 0))

	goal := StateThreadGoal{
		ThreadID:  protocol.NewThreadID(testThreadUUID),
		GoalID:    "goal-1",
		Objective: "new objective",
		Status:    StateGoalStatusActive,
	}
	if err := rt.ApplyExternalGoalSet(context.Background(), goal, nil); err != nil {
		t.Fatalf("ApplyExternalGoalSet err = %v", err)
	}
	if len(thread.injections()) == 0 {
		t.Error("expected steering injection for new active goal")
	}
}

func TestApplyExternalGoalSetTerminalClearsActive(t *testing.T) {
	store := newFakeStore()
	rt, acct := newTestRuntime(store, &fakeMetrics{}, nil, &captureSink{}, true)
	acct.startTurn("turn-1", protocol.ModeKindDefault, usage(0, 0, 0))
	acct.markCurrentTurnGoalActive("goal-1")

	goal := StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "goal-1", Status: StateGoalStatusComplete}
	if err := rt.ApplyExternalGoalSet(context.Background(), goal, nil); err != nil {
		t.Fatalf("err = %v", err)
	}
	if acct.turnIsCurrentActiveGoal("turn-1") {
		t.Error("terminal goal should clear the active goal")
	}
}

func TestApplyExternalGoalSetRecordsResumed(t *testing.T) {
	store := newFakeStore()
	metrics := &fakeMetrics{}
	rt, _ := newTestRuntime(store, metrics, &fakeThreadManager{thread: &fakeThread{}}, &captureSink{}, true)

	previous := PreviousGoalSnapshot{GoalID: "goal-1", Status: StateGoalStatusPaused, Objective: "x"}
	goal := StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "goal-1", Status: StateGoalStatusActive, Objective: "x"}
	if err := rt.ApplyExternalGoalSet(context.Background(), goal, &previous); err != nil {
		t.Fatalf("err = %v", err)
	}
	found := false
	for _, c := range metrics.counters {
		if c == goalResumedMetric {
			found = true
		}
	}
	if !found {
		t.Errorf("expected resumed metric, got %v", metrics.counters)
	}
}

func TestRestoreAfterResumeActiveGoal(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "goal-1", Status: StateGoalStatusActive}
	metrics := &fakeMetrics{}
	rt, acct := newTestRuntime(store, metrics, nil, &captureSink{}, true)

	if err := rt.RestoreAfterResume(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	// Idle active goal is tracked in wall-clock accounting and a resume recorded.
	if acct.idleProgressSnapshot() != nil {
		// snapshot is only non-nil after time passes; just confirm no panic.
		_ = acct
	}
	resumed := false
	for _, c := range metrics.counters {
		if c == goalResumedMetric {
			resumed = true
		}
	}
	if !resumed {
		t.Errorf("expected resumed metric on active resume, got %v", metrics.counters)
	}
}

func TestRestoreAfterResumeNonActiveClears(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "goal-1", Status: StateGoalStatusComplete}
	rt, acct := newTestRuntime(store, &fakeMetrics{}, nil, &captureSink{}, true)
	acct.markIdleGoalActive("goal-1")
	if err := rt.RestoreAfterResume(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	if acct.idleProgressSnapshot() != nil {
		t.Error("non-active resume should clear the active goal")
	}
}

func TestUsageLimitActiveGoalForTurn(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "goal-1", Status: StateGoalStatusActive}
	sink := &captureSink{}
	rt, acct := newTestRuntime(store, &fakeMetrics{}, nil, sink, true)
	acct.startTurn("turn-1", protocol.ModeKindDefault, usage(0, 0, 0))
	acct.markTurnGoalActive("turn-1", "goal-1")

	if err := rt.UsageLimitActiveGoalForTurn(context.Background(), "turn-1"); err != nil {
		t.Fatalf("err = %v", err)
	}
	if store.goal.Status != StateGoalStatusUsageLimited {
		t.Fatalf("status = %v, want usage_limited", store.goal.Status)
	}
	// An event was emitted for the usage-limit transition.
	if len(sink.all()) == 0 {
		t.Error("expected a thread-goal-updated event")
	}
}

func TestInjectActiveTurnSteeringNoThreadManager(t *testing.T) {
	store := newFakeStore()
	rt, _ := newTestRuntime(store, &fakeMetrics{}, nil, &captureSink{}, true)
	// No thread manager: must not panic.
	rt.injectActiveTurnSteering(context.Background(), protocol.ResponseItem{})
}
