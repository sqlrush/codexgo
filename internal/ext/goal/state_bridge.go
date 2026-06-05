package goal

// state_bridge adapts the SQLite-backed state.StateRuntime goal store onto the
// goal extension's StateRuntime/ThreadGoalsStore interfaces, and exposes the
// tool-executor trio for hosts that wire goal tools without the full extension
// registry (the headless exec/CLI path). Once the extension registry is wired
// into the engine, InstallWithBackend supersedes NewToolExecutors.

import (
	"context"

	"github.com/sqlrush/codexgo/internal/ext/extensionapi"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/state"
	"github.com/sqlrush/codexgo/internal/tools"
)

// stateRuntimeBridge implements [StateRuntime] over a *state.StateRuntime.
type stateRuntimeBridge struct {
	runtime *state.StateRuntime
}

// NewStateRuntimeBridge adapts the host state runtime onto the goal extension's
// [StateRuntime] interface.
func NewStateRuntimeBridge(runtime *state.StateRuntime) StateRuntime {
	return stateRuntimeBridge{runtime: runtime}
}

// ThreadGoals returns the SQLite-backed goal store.
func (b stateRuntimeBridge) ThreadGoals() ThreadGoalsStore {
	return goalStoreBridge{store: b.runtime.ThreadGoals()}
}

// SetThreadPreviewIfEmpty defers to the state runtime's thread-preview helper.
func (b stateRuntimeBridge) SetThreadPreviewIfEmpty(ctx context.Context, threadID protocol.ThreadID, preview string) (bool, error) {
	return b.runtime.SetThreadPreviewIfEmpty(ctx, threadID, preview)
}

// goalStoreBridge implements [ThreadGoalsStore] over a *state.GoalStore,
// mapping between the goal extension's mirror types and the state package's.
type goalStoreBridge struct {
	store *state.GoalStore
}

func (b goalStoreBridge) GetThreadGoal(ctx context.Context, threadID protocol.ThreadID) (*StateThreadGoal, error) {
	goal, err := b.store.GetThreadGoal(ctx, threadID.String())
	if err != nil {
		return nil, err
	}
	return goalFromState(goal), nil
}

func (b goalStoreBridge) InsertThreadGoal(ctx context.Context, threadID protocol.ThreadID, objective string, status StateThreadGoalStatus, tokenBudget *int64) (*StateThreadGoal, error) {
	goal, err := b.store.InsertThreadGoal(ctx, threadID.String(), objective, statusToState(status), tokenBudget)
	if err != nil {
		return nil, err
	}
	return goalFromState(goal), nil
}

func (b goalStoreBridge) UpdateThreadGoal(ctx context.Context, threadID protocol.ThreadID, update GoalUpdate) (*StateThreadGoal, error) {
	stateUpdate := state.GoalUpdate{
		Objective:      update.Objective,
		TokenBudget:    update.TokenBudget,
		ExpectedGoalID: update.ExpectedGoalID,
	}
	if update.Status != nil {
		status := statusToState(*update.Status)
		stateUpdate.Status = &status
	}
	goal, err := b.store.UpdateThreadGoal(ctx, threadID.String(), stateUpdate)
	if err != nil {
		return nil, err
	}
	return goalFromState(goal), nil
}

func (b goalStoreBridge) AccountThreadGoalUsage(ctx context.Context, threadID protocol.ThreadID, timeDeltaSeconds, tokenDelta int64, mode GoalAccountingMode, expectedGoalID *string) (GoalAccountingOutcome, error) {
	outcome, err := b.store.AccountThreadGoalUsage(ctx, threadID.String(), timeDeltaSeconds, tokenDelta, accountingModeToState(mode), expectedGoalID)
	if err != nil {
		return GoalAccountingOutcome{}, err
	}
	kind := GoalAccountingOutcomeUnchanged
	if outcome.Kind == state.GoalAccountingOutcomeUpdated {
		kind = GoalAccountingOutcomeUpdated
	}
	return GoalAccountingOutcome{Kind: kind, Goal: goalFromState(outcome.Goal)}, nil
}

func (b goalStoreBridge) UsageLimitActiveThreadGoal(ctx context.Context, threadID protocol.ThreadID) (*StateThreadGoal, error) {
	goal, err := b.store.UsageLimitActiveThreadGoal(ctx, threadID.String())
	if err != nil {
		return nil, err
	}
	return goalFromState(goal), nil
}

// NewToolExecutors builds the get/create/update goal tool executor trio over
// the given goal store for one thread. It is the headless-host bridge used
// until the extension registry (InstallWithBackend) is wired into the engine:
// the executors share a fresh accounting state (so update_goal's pending-turn
// progress flush is a no-op until turn lifecycle accounting lands), an
// optional event sink (nil drops goal-updated events), and an optional metrics
// client (nil disables telemetry).
func NewToolExecutors(threadID protocol.ThreadID, stateDB StateRuntime, eventSink extensionapi.ExtensionEventSink, metricsClient MetricsClient) []tools.ToolExecutor[tools.ToolCall] {
	if eventSink == nil {
		eventSink = extensionapi.NoopExtensionEventSink{}
	}
	emitter := newGoalEventEmitter(eventSink)
	metrics := NewGoalMetrics(metricsClient)
	accounting := newGoalAccountingState()
	return []tools.ToolExecutor[tools.ToolCall]{
		newGoalToolExecutor(goalToolKindGet, threadID, stateDB, accounting, emitter, metrics),
		newGoalToolExecutor(goalToolKindCreate, threadID, stateDB, accounting, emitter, metrics),
		newGoalToolExecutor(goalToolKindUpdate, threadID, stateDB, accounting, emitter, metrics),
	}
}

// goalFromState maps a state-package goal row onto the extension's mirror type.
func goalFromState(goal *state.ThreadGoal) *StateThreadGoal {
	if goal == nil {
		return nil
	}
	return &StateThreadGoal{
		ThreadID:        protocol.NewThreadID(goal.ThreadID),
		GoalID:          goal.GoalID,
		Objective:       goal.Objective,
		Status:          statusFromState(goal.Status),
		TokenBudget:     goal.TokenBudget,
		TokensUsed:      goal.TokensUsed,
		TimeUsedSeconds: goal.TimeUsedSeconds,
		CreatedAt:       goal.CreatedAt,
		UpdatedAt:       goal.UpdatedAt,
	}
}

// statusFromState maps a state status onto the extension's mirror status. Both
// enums are closed six-variant mirrors of the Rust `ThreadGoalStatus`; a new
// variant must be added to both sides (and this switch) together — the
// Complete fallthrough only ever sees GoalStatusComplete.
func statusFromState(status state.ThreadGoalStatus) StateThreadGoalStatus {
	switch status {
	case state.GoalStatusActive:
		return StateGoalStatusActive
	case state.GoalStatusPaused:
		return StateGoalStatusPaused
	case state.GoalStatusBlocked:
		return StateGoalStatusBlocked
	case state.GoalStatusUsageLimited:
		return StateGoalStatusUsageLimited
	case state.GoalStatusBudgetLimited:
		return StateGoalStatusBudgetLimited
	case state.GoalStatusComplete:
		return StateGoalStatusComplete
	default:
		return StateGoalStatusComplete
	}
}

// statusToState maps the extension's mirror status onto the state status. See
// the closed-enum note on statusFromState.
func statusToState(status StateThreadGoalStatus) state.ThreadGoalStatus {
	switch status {
	case StateGoalStatusActive:
		return state.GoalStatusActive
	case StateGoalStatusPaused:
		return state.GoalStatusPaused
	case StateGoalStatusBlocked:
		return state.GoalStatusBlocked
	case StateGoalStatusUsageLimited:
		return state.GoalStatusUsageLimited
	case StateGoalStatusBudgetLimited:
		return state.GoalStatusBudgetLimited
	case StateGoalStatusComplete:
		return state.GoalStatusComplete
	default:
		return state.GoalStatusComplete
	}
}

// accountingModeToState maps the extension's accounting mode onto the state
// package's.
func accountingModeToState(mode GoalAccountingMode) state.GoalAccountingMode {
	switch mode {
	case GoalAccountingModeActiveStatusOnly:
		return state.GoalAccountingModeActiveStatusOnly
	case GoalAccountingModeActiveOnly:
		return state.GoalAccountingModeActiveOnly
	case GoalAccountingModeActiveOrComplete:
		return state.GoalAccountingModeActiveOrComplete
	default:
		return state.GoalAccountingModeActiveOrStopped
	}
}
