package goal

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/internal/ext/extensionapi"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

func newThreadStore() *extensionapi.ExtensionData {
	return extensionapi.NewExtensionData(testThreadUUID)
}

func TestOnThreadStartSeedsRuntime(t *testing.T) {
	ext := newGoalExtensionWithHostCapabilities[struct{}](
		newFakeStore(), &captureSink{}, nil, nil,
		func(struct{}) bool { return true },
	)
	store := newThreadStore()
	ext.OnThreadStart(context.Background(), extensionapi.ThreadStartInput[struct{}]{
		Config:                         struct{}{},
		PersistentThreadStateAvailable: true,
		ThreadStore:                    store,
	})

	cfg, ok := extensionapi.ExtensionDataGet[GoalExtensionConfig](store)
	if !ok || !cfg.Enabled {
		t.Fatalf("config = %+v ok=%v", cfg, ok)
	}
	runtime, ok := goalRuntimeHandle(store)
	if !ok {
		t.Fatal("runtime not seeded")
	}
	if !runtime.isEnabled() || !runtime.toolsVisible() {
		t.Errorf("runtime enabled=%v toolsVisible=%v", runtime.isEnabled(), runtime.toolsVisible())
	}
}

func TestOnThreadStartReviewSubAgentHidesTools(t *testing.T) {
	ext := newGoalExtensionWithHostCapabilities[struct{}](
		newFakeStore(), &captureSink{}, nil, nil,
		func(struct{}) bool { return true },
	)
	store := newThreadStore()
	ext.OnThreadStart(context.Background(), extensionapi.ThreadStartInput[struct{}]{
		PersistentThreadStateAvailable: true,
		SessionSource:                  reviewSubAgentSource(),
		ThreadStore:                    store,
	})
	runtime, ok := goalRuntimeHandle(store)
	if !ok {
		t.Fatal("runtime not seeded")
	}
	if runtime.toolsVisible() {
		t.Error("review sub-agent threads should not expose goal tools")
	}
}

func TestOnThreadStartDisabledByConfig(t *testing.T) {
	ext := newGoalExtensionWithHostCapabilities[bool](
		newFakeStore(), &captureSink{}, nil, nil,
		func(enabled bool) bool { return enabled },
	)
	store := newThreadStore()
	ext.OnThreadStart(context.Background(), extensionapi.ThreadStartInput[bool]{
		Config:                         false,
		PersistentThreadStateAvailable: true,
		ThreadStore:                    store,
	})
	runtime, _ := goalRuntimeHandle(store)
	if runtime.isEnabled() {
		t.Error("runtime should be disabled when config says so")
	}
	if cfg, _ := extensionapi.ExtensionDataGet[GoalExtensionConfig](store); cfg.Enabled {
		t.Error("config flag should be false")
	}
}

func TestOnConfigChangedTogglesEnablement(t *testing.T) {
	ext := newGoalExtensionWithHostCapabilities[bool](
		newFakeStore(), &captureSink{}, nil, nil,
		func(enabled bool) bool { return enabled },
	)
	store := newThreadStore()
	ext.OnThreadStart(context.Background(), extensionapi.ThreadStartInput[bool]{
		Config:                         true,
		PersistentThreadStateAvailable: true,
		ThreadStore:                    store,
	})
	ext.OnConfigChanged(nil, store, true, false)
	runtime, _ := goalRuntimeHandle(store)
	if runtime.isEnabled() {
		t.Error("runtime should be disabled after config change")
	}
}

func TestTokenUsageBudgetLimitTriggersSteering(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{
		ThreadID:    protocol.NewThreadID(testThreadUUID),
		GoalID:      "goal-1",
		Objective:   "do the thing",
		Status:      StateGoalStatusActive,
		TokenBudget: i64(100),
	}
	thread := &fakeThread{}
	tm := &fakeThreadManager{thread: thread}
	sink := &captureSink{}
	ext := newGoalExtensionWithHostCapabilities[struct{}](
		store, sink, nil, tm,
		func(struct{}) bool { return true },
	)

	threadStore := newThreadStore()
	ext.OnThreadStart(context.Background(), extensionapi.ThreadStartInput[struct{}]{
		Config:                         struct{}{},
		PersistentThreadStateAvailable: true,
		ThreadStore:                    threadStore,
	})

	turnStore := extensionapi.NewExtensionData("turn-1")
	ext.OnTurnStart(context.Background(), extensionapi.TurnStartInput{
		TurnID:            "turn-1",
		CollaborationMode: protocol.CollaborationMode{Mode: protocol.ModeKindDefault},
		ThreadStore:       threadStore,
		TurnStore:         turnStore,
	})

	// Record usage that exceeds the budget so the next accounting flips the goal
	// to budget_limited.
	ext.OnTokenUsage(context.Background(), nil, threadStore, turnStore, protocol.TokenUsageInfo{
		TotalTokenUsage: usage(150, 0, 0),
	})

	// A non-update tool finishing accounts progress and should steer the turn.
	ext.OnToolFinish(context.Background(), extensionapi.ToolFinishInput{
		TurnID:      "turn-1",
		CallID:      "call-1",
		ToolName:    protocol.PlainToolName("read_file"),
		ThreadStore: threadStore,
		TurnStore:   turnStore,
		Outcome:     extensionapi.CompletedToolCallOutcome(true),
	})

	if store.goal.Status != StateGoalStatusBudgetLimited {
		t.Fatalf("goal status = %v, want budget_limited", store.goal.Status)
	}
	injections := thread.injections()
	if len(injections) == 0 {
		t.Fatal("expected a steering injection on budget limit")
	}
}

func TestUpdateGoalToolFinishDoesNotDoubleCount(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "goal-1", Objective: "x", Status: StateGoalStatusActive, TokenBudget: i64(100)}
	ext := newGoalExtensionWithHostCapabilities[struct{}](
		store, &captureSink{}, nil, &fakeThreadManager{thread: &fakeThread{}},
		func(struct{}) bool { return true },
	)
	threadStore := newThreadStore()
	ext.OnThreadStart(context.Background(), extensionapi.ThreadStartInput[struct{}]{
		Config:                         struct{}{},
		PersistentThreadStateAvailable: true,
		ThreadStore:                    threadStore,
	})
	turnStore := extensionapi.NewExtensionData("turn-1")
	ext.OnTurnStart(context.Background(), extensionapi.TurnStartInput{
		TurnID:            "turn-1",
		CollaborationMode: protocol.CollaborationMode{Mode: protocol.ModeKindDefault},
		ThreadStore:       threadStore,
		TurnStore:         turnStore,
	})
	ext.OnTokenUsage(context.Background(), nil, threadStore, turnStore, protocol.TokenUsageInfo{TotalTokenUsage: usage(150, 0, 0)})

	before := store.accountCall
	// update_goal finishing must NOT account progress (it would double count).
	ext.OnToolFinish(context.Background(), extensionapi.ToolFinishInput{
		TurnID:      "turn-1",
		CallID:      "call-1",
		ToolName:    protocol.PlainToolName(UpdateGoalToolName),
		ThreadStore: threadStore,
		TurnStore:   turnStore,
		Outcome:     extensionapi.CompletedToolCallOutcome(true),
	})
	if store.accountCall != before {
		t.Errorf("update_goal tool finish accounted progress (%d -> %d)", before, store.accountCall)
	}
}

func TestOnTurnErrorUsageLimit(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "goal-1", Objective: "x", Status: StateGoalStatusActive}
	sink := &captureSink{}
	ext := newGoalExtensionWithHostCapabilities[struct{}](
		store, sink, nil, nil,
		func(struct{}) bool { return true },
	)
	threadStore := newThreadStore()
	ext.OnThreadStart(context.Background(), extensionapi.ThreadStartInput[struct{}]{
		Config:                         struct{}{},
		PersistentThreadStateAvailable: true,
		ThreadStore:                    threadStore,
	})
	turnStore := extensionapi.NewExtensionData("turn-1")
	ext.OnTurnStart(context.Background(), extensionapi.TurnStartInput{
		TurnID:            "turn-1",
		CollaborationMode: protocol.CollaborationMode{Mode: protocol.ModeKindDefault},
		ThreadStore:       threadStore,
		TurnStore:         turnStore,
	})
	// Mark the goal active for the turn so usage-limit applies.
	ext.OnTokenUsage(context.Background(), nil, threadStore, turnStore, protocol.TokenUsageInfo{TotalTokenUsage: usage(10, 0, 0)})

	ext.OnTurnError(context.Background(), extensionapi.TurnErrorInput{
		TurnID:      "turn-1",
		Error:       usageLimitError(),
		ThreadStore: threadStore,
		TurnStore:   turnStore,
	})
	if store.goal.Status != StateGoalStatusUsageLimited {
		t.Fatalf("goal status = %v, want usage_limited", store.goal.Status)
	}
}

func TestInstallWithBackendRegistersAllContributors(t *testing.T) {
	builder := extensionapi.NewExtensionRegistryBuilder[struct{}]()
	InstallWithBackend[struct{}](builder, newFakeStore(), nil, nil, func(struct{}) bool { return true })
	reg := builder.Build()

	if len(reg.ThreadLifecycleContributors()) != 1 {
		t.Error("expected one thread-lifecycle contributor")
	}
	if len(reg.ConfigContributors()) != 1 {
		t.Error("expected one config contributor")
	}
	if len(reg.TurnLifecycleContributors()) != 1 {
		t.Error("expected one turn-lifecycle contributor")
	}
	if len(reg.TokenUsageContributors()) != 1 {
		t.Error("expected one token-usage contributor")
	}
	if len(reg.ToolLifecycleContributors()) != 1 {
		t.Error("expected one tool-lifecycle contributor")
	}
	if len(reg.ToolContributors()) != 1 {
		t.Error("expected one tool contributor")
	}

	// The tool contributor exposes the three goal tools for an enabled thread.
	store := newThreadStore()
	reg.ThreadLifecycleContributors()[0].OnThreadStart(context.Background(), extensionapi.ThreadStartInput[struct{}]{
		Config:                         struct{}{},
		PersistentThreadStateAvailable: true,
		ThreadStore:                    store,
	})
	toolList := reg.ToolContributors()[0].Tools(nil, store)
	if len(toolList) != 3 {
		t.Fatalf("tools = %d, want 3", len(toolList))
	}
}

// reviewSubAgentSource builds a review sub-agent session source.
func reviewSubAgentSource() rollout.SessionSource {
	return rollout.SessionSource{
		Kind: rollout.SessionSourceKindSubAgent,
		SubAgent: &rollout.SubAgentSource{
			Kind: rollout.SubAgentSourceKindReview,
		},
	}
}

// usageLimitError builds a usage-limit CodexErrorInfo.
func usageLimitError() protocol.CodexErrorInfo {
	return protocol.CodexErrorInfo{Kind: protocol.CodexErrorInfoUsageLimitExceeded}
}
