package goal

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// GoalRuntimeHandle is the per-thread goal runtime. Mirrors the Rust
// `GoalRuntimeHandle`; it is cheaply cloneable because all fields live behind a
// shared inner value.
type GoalRuntimeHandle struct {
	inner *goalRuntimeInner
}

// goalRuntimeConfig carries the per-thread runtime configuration. Mirrors Rust
// `GoalRuntimeConfig`.
type goalRuntimeConfig struct {
	enabled                 bool
	toolsAvailableForThread bool
}

type goalRuntimeInner struct {
	threadID                protocol.ThreadID
	stateDBs                StateRuntime
	eventEmitter            goalEventEmitter
	metrics                 GoalMetrics
	threadManager           ThreadManager
	accountingState         *goalAccountingState
	enabled                 atomic.Bool
	toolsAvailableForThread bool
}

// accountedGoalProgress is the result of accounting active goal progress.
// Mirrors Rust `AccountedGoalProgress`.
type accountedGoalProgress struct {
	goal   protocol.ThreadGoal
	goalID string
}

// PreviousGoalSnapshot is a minimal snapshot of a goal before an external
// mutation. Mirrors the Rust `PreviousGoalSnapshot`.
type PreviousGoalSnapshot struct {
	GoalID    string
	Status    StateThreadGoalStatus
	Objective string
}

// PreviousGoalSnapshotFromState builds a snapshot from a persisted goal. Mirrors
// the Rust `From<&codex_state::ThreadGoal> for PreviousGoalSnapshot`.
func PreviousGoalSnapshotFromState(goal StateThreadGoal) PreviousGoalSnapshot {
	return PreviousGoalSnapshot{
		GoalID:    goal.GoalID,
		Status:    goal.Status,
		Objective: goal.Objective,
	}
}

// newGoalRuntimeHandle constructs a runtime handle. Mirrors Rust
// `GoalRuntimeHandle::new`.
func newGoalRuntimeHandle(
	threadID protocol.ThreadID,
	stateDBs StateRuntime,
	eventEmitter goalEventEmitter,
	metrics GoalMetrics,
	threadManager ThreadManager,
	accountingState *goalAccountingState,
	config goalRuntimeConfig,
) GoalRuntimeHandle {
	inner := &goalRuntimeInner{
		threadID:                threadID,
		stateDBs:                stateDBs,
		eventEmitter:            eventEmitter,
		metrics:                 metrics,
		threadManager:           threadManager,
		accountingState:         accountingState,
		toolsAvailableForThread: config.toolsAvailableForThread,
	}
	inner.enabled.Store(config.enabled)
	return GoalRuntimeHandle{inner: inner}
}

func (h GoalRuntimeHandle) setEnabled(enabled bool) {
	h.inner.enabled.Store(enabled)
}

func (h GoalRuntimeHandle) isEnabled() bool {
	return h.inner.enabled.Load()
}

func (h GoalRuntimeHandle) toolsVisible() bool {
	return h.isEnabled() && h.inner.toolsAvailableForThread
}

func (h GoalRuntimeHandle) threadID() protocol.ThreadID {
	return h.inner.threadID
}

func (h GoalRuntimeHandle) accounting() *goalAccountingState {
	return h.inner.accountingState
}

// PrepareExternalGoalMutation flushes pending progress before an external goal
// mutation. Mirrors Rust `GoalRuntimeHandle::prepare_external_goal_mutation`.
func (h GoalRuntimeHandle) PrepareExternalGoalMutation(ctx context.Context) error {
	if !h.isEnabled() {
		return nil
	}
	if turnID := h.inner.accountingState.currentTurnID(); turnID != nil {
		_, err := h.accountActiveGoalProgress(
			ctx,
			*turnID,
			fmt.Sprintf("%s:external-goal-mutation", *turnID),
			GoalAccountingModeActiveOnly,
			budgetLimitedClearActive,
		)
		return err
	}
	_, err := h.accountIdleGoalProgress(
		ctx,
		fmt.Sprintf("%s:external-goal-mutation", h.inner.threadID),
		GoalAccountingModeActiveOnly,
		budgetLimitedClearActive,
	)
	return err
}

// ApplyExternalGoalSet reconciles accounting and metrics after a host sets a
// goal externally. Mirrors Rust `GoalRuntimeHandle::apply_external_goal_set`.
func (h GoalRuntimeHandle) ApplyExternalGoalSet(ctx context.Context, goal StateThreadGoal, previousGoal *PreviousGoalSnapshot) error {
	if !h.isEnabled() {
		return nil
	}

	replacedExistingGoal := previousGoal != nil && previousGoal.GoalID != goal.GoalID
	if previousGoal == nil || replacedExistingGoal {
		h.inner.metrics.recordCreated()
	}
	var previousStatus *StateThreadGoalStatus
	if previousGoal != nil && !replacedExistingGoal {
		status := previousGoal.Status
		previousStatus = &status
	}
	h.inner.metrics.recordResumedIfStatusChanged(previousStatus, goal.Status)
	h.inner.metrics.recordTerminalIfStatusChanged(previousStatus, &goal)

	shouldSteerActiveTurn := previousGoal == nil ||
		previousGoal.GoalID != goal.GoalID ||
		previousGoal.Status != StateGoalStatusActive ||
		previousGoal.Objective != goal.Objective

	switch goal.Status {
	case StateGoalStatusActive:
		if h.inner.accountingState.currentTurnID() != nil {
			h.inner.accountingState.markCurrentTurnGoalActive(goal.GoalID)
		} else {
			h.inner.accountingState.markIdleGoalActive(goal.GoalID)
		}
		if shouldSteerActiveTurn {
			item := objectiveUpdatedSteeringItem(protocolGoalFromState(goal))
			h.injectActiveTurnSteering(ctx, item)
		}
	case StateGoalStatusBudgetLimited:
		if h.inner.accountingState.currentTurnID() == nil {
			h.inner.accountingState.clearActiveGoal()
		}
	case StateGoalStatusPaused, StateGoalStatusBlocked, StateGoalStatusUsageLimited, StateGoalStatusComplete:
		h.inner.accountingState.clearActiveGoal()
	}
	return nil
}

// ApplyExternalGoalClear clears accounting after a host clears the goal
// externally. Mirrors Rust `GoalRuntimeHandle::apply_external_goal_clear`.
func (h GoalRuntimeHandle) ApplyExternalGoalClear(_ context.Context) error {
	if !h.isEnabled() {
		return nil
	}
	h.inner.accountingState.clearActiveGoal()
	return nil
}

// UsageLimitActiveGoalForTurn marks the active goal usage-limited for the turn.
// Mirrors Rust `GoalRuntimeHandle::usage_limit_active_goal_for_turn`.
func (h GoalRuntimeHandle) UsageLimitActiveGoalForTurn(ctx context.Context, turnID string) error {
	if !h.isEnabled() {
		return nil
	}
	if !h.inner.accountingState.turnIsCurrentActiveGoal(turnID) {
		return nil
	}

	progressEventID := fmt.Sprintf("%s:usage-limit-progress", turnID)
	if _, err := h.accountActiveGoalProgress(ctx, turnID, progressEventID, GoalAccountingModeActiveOnly, budgetLimitedClearActive); err != nil {
		return err
	}

	previousStatus, err := h.currentGoalStatusForMetrics(ctx, nil)
	if err != nil {
		return err
	}
	goal, err := h.inner.stateDBs.ThreadGoals().UsageLimitActiveThreadGoal(ctx, h.threadID())
	if err != nil {
		return fmt.Errorf("usage-limit active thread goal: %w", err)
	}
	if goal == nil {
		return nil
	}
	h.inner.metrics.recordTerminalIfStatusChanged(previousStatus, goal)
	h.inner.accountingState.clearActiveGoal()
	protocolGoal := protocolGoalFromState(*goal)
	turnIDCopy := turnID
	h.inner.eventEmitter.threadGoalUpdated(fmt.Sprintf("%s:usage-limit", turnID), &turnIDCopy, protocolGoal)
	return nil
}

// RestoreAfterResume reconciles accounting after a thread resume. Mirrors Rust
// `GoalRuntimeHandle::restore_after_resume`.
func (h GoalRuntimeHandle) RestoreAfterResume(ctx context.Context) error {
	if !h.isEnabled() {
		return nil
	}
	goal, err := h.inner.stateDBs.ThreadGoals().GetThreadGoal(ctx, h.threadID())
	if err != nil {
		return fmt.Errorf("get thread goal: %w", err)
	}
	if goal != nil && goal.Status == StateGoalStatusActive {
		h.inner.accountingState.markIdleGoalActive(goal.GoalID)
		h.inner.metrics.recordResumed()
		return nil
	}
	h.inner.accountingState.clearActiveGoal()
	return nil
}

// injectActiveTurnSteering injects a steering item into the active turn when one
// is running. Mirrors Rust `GoalRuntimeHandle::inject_active_turn_steering`.
// Any failure to resolve the live thread or inject is silently skipped.
func (h GoalRuntimeHandle) injectActiveTurnSteering(ctx context.Context, item protocol.ResponseItem) {
	if h.inner.threadManager == nil {
		return
	}
	thread, err := h.inner.threadManager.GetThread(ctx, h.inner.threadID)
	if err != nil || thread == nil {
		return
	}
	_ = thread.InjectIfRunning(ctx, []protocol.ResponseItem{item})
}

// accountActiveGoalProgress flushes pending turn progress to the goal store.
// Mirrors Rust `GoalRuntimeHandle::account_active_goal_progress`.
func (h GoalRuntimeHandle) accountActiveGoalProgress(
	ctx context.Context,
	turnID, eventID string,
	mode GoalAccountingMode,
	disposition budgetLimitedGoalDisposition,
) (*accountedGoalProgress, error) {
	accounting := h.accounting()
	snapshot := accounting.progressSnapshot(turnID)
	if snapshot == nil {
		return nil, nil
	}
	previousStatus, err := h.currentGoalStatusForMetrics(ctx, &snapshot.expectedGoalID)
	if err != nil {
		return nil, err
	}
	expectedGoalID := snapshot.expectedGoalID
	outcome, err := h.inner.stateDBs.ThreadGoals().AccountThreadGoalUsage(
		ctx, h.threadID(), snapshot.timeDeltaSeconds, snapshot.tokenDelta, mode, &expectedGoalID,
	)
	if err != nil {
		return nil, fmt.Errorf("account thread goal usage: %w", err)
	}
	if outcome.Kind != GoalAccountingOutcomeUpdated || outcome.Goal == nil {
		return nil, nil
	}
	stateGoal := *outcome.Goal
	goalID := stateGoal.GoalID
	h.inner.metrics.recordTerminalIfStatusChanged(previousStatus, &stateGoal)
	accounting.markProgressAccountedForStatus(turnID, snapshot, stateGoal.Status, disposition)
	protocolGoal := protocolGoalFromState(stateGoal)
	turnIDCopy := turnID
	h.inner.eventEmitter.threadGoalUpdated(eventID, &turnIDCopy, protocolGoal)
	return &accountedGoalProgress{goal: protocolGoal, goalID: goalID}, nil
}

// accountIdleGoalProgress flushes pending idle (wall-clock) progress. Mirrors
// Rust `GoalRuntimeHandle::account_idle_goal_progress`.
func (h GoalRuntimeHandle) accountIdleGoalProgress(
	ctx context.Context,
	eventID string,
	mode GoalAccountingMode,
	disposition budgetLimitedGoalDisposition,
) (*accountedGoalProgress, error) {
	accounting := h.accounting()
	snapshot := accounting.idleProgressSnapshot()
	if snapshot == nil {
		return nil, nil
	}
	previousStatus, err := h.currentGoalStatusForMetrics(ctx, &snapshot.expectedGoalID)
	if err != nil {
		return nil, err
	}
	expectedGoalID := snapshot.expectedGoalID
	outcome, err := h.inner.stateDBs.ThreadGoals().AccountThreadGoalUsage(
		ctx, h.threadID(), snapshot.timeDeltaSeconds, 0, mode, &expectedGoalID,
	)
	if err != nil {
		return nil, fmt.Errorf("account thread goal usage: %w", err)
	}
	if outcome.Kind != GoalAccountingOutcomeUpdated || outcome.Goal == nil {
		accounting.resetIdleProgressBaselineAndClearActiveGoal()
		return nil, nil
	}
	stateGoal := *outcome.Goal
	goalID := stateGoal.GoalID
	h.inner.metrics.recordTerminalIfStatusChanged(previousStatus, &stateGoal)
	accounting.markIdleProgressAccountedForStatus(snapshot, stateGoal.Status, disposition)
	protocolGoal := protocolGoalFromState(stateGoal)
	h.inner.eventEmitter.threadGoalUpdated(eventID, nil, protocolGoal)
	return &accountedGoalProgress{goal: protocolGoal, goalID: goalID}, nil
}

// currentGoalStatusForMetrics reads the current goal status when it matches the
// expected goal id. Mirrors Rust
// `GoalRuntimeHandle::current_goal_status_for_metrics`.
func (h GoalRuntimeHandle) currentGoalStatusForMetrics(ctx context.Context, expectedGoalID *string) (*StateThreadGoalStatus, error) {
	goal, err := h.inner.stateDBs.ThreadGoals().GetThreadGoal(ctx, h.threadID())
	if err != nil {
		return nil, fmt.Errorf("get thread goal: %w", err)
	}
	if goal == nil {
		return nil, nil
	}
	if expectedGoalID != nil && goal.GoalID != *expectedGoalID {
		return nil, nil
	}
	status := goal.Status
	return &status, nil
}
