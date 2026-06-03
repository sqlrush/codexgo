package goal

import (
	"context"

	"github.com/sqlrush/codexgo/internal/ext/extensionapi"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/tools"
)

// GoalExtensionConfig is the per-thread goal enablement flag stored in the
// thread store. Mirrors the Rust `GoalExtensionConfig`.
type GoalExtensionConfig struct {
	Enabled bool
}

func goalExtensionConfigFromEnabled(enabled bool) GoalExtensionConfig {
	return GoalExtensionConfig{Enabled: enabled}
}

// GoalExtension wires the goal feature into the extension registry. Mirrors the
// Rust `GoalExtension<C>`. The type parameter C is the host configuration type.
type GoalExtension[C any] struct {
	stateDBs      StateRuntime
	eventEmitter  goalEventEmitter
	metrics       GoalMetrics
	threadManager ThreadManager
	goalsEnabled  GoalsEnabledFunc[C]
}

// newGoalExtensionWithHostCapabilities constructs a goal extension from host
// capabilities. Mirrors Rust `GoalExtension::new_with_host_capabilities`.
func newGoalExtensionWithHostCapabilities[C any](
	stateDBs StateRuntime,
	eventSink extensionapi.ExtensionEventSink,
	metricsClient MetricsClient,
	threadManager ThreadManager,
	goalsEnabled GoalsEnabledFunc[C],
) *GoalExtension[C] {
	return &GoalExtension[C]{
		stateDBs:      stateDBs,
		eventEmitter:  newGoalEventEmitter(eventSink),
		metrics:       NewGoalMetrics(metricsClient),
		threadManager: threadManager,
		goalsEnabled:  goalsEnabled,
	}
}

// ---------------------------------------------------------------------------
// ThreadLifecycleContributor
// ---------------------------------------------------------------------------

// OnThreadStart seeds the per-thread goal config, accounting state, and runtime.
// Mirrors the Rust `ThreadLifecycleContributor::on_thread_start` impl.
func (g *GoalExtension[C]) OnThreadStart(_ context.Context, input extensionapi.ThreadStartInput[C]) {
	enabled := g.goalsEnabled(input.Config)
	toolsAvailableForThread := input.PersistentThreadStateAvailable && !isReviewSubAgent(input.SessionSource)
	extensionapi.ExtensionDataInsert(input.ThreadStore, goalExtensionConfigFromEnabled(enabled))
	accountingState := extensionapi.ExtensionDataGetOrInit(input.ThreadStore, newGoalAccountingState)
	levelID := input.ThreadStore.LevelID()
	if levelID == "" {
		return
	}
	threadID := protocol.NewThreadID(levelID)
	runtime := extensionapi.ExtensionDataGetOrInit(input.ThreadStore, func() GoalRuntimeHandle {
		return newGoalRuntimeHandle(
			threadID,
			g.stateDBs,
			g.eventEmitter,
			g.metrics,
			g.threadManager,
			accountingState,
			goalRuntimeConfig{enabled: enabled, toolsAvailableForThread: toolsAvailableForThread},
		)
	})
	runtime.setEnabled(enabled)
}

// OnThreadResume restores accounting after a resume. Mirrors the Rust
// `on_thread_resume` impl.
func (g *GoalExtension[C]) OnThreadResume(ctx context.Context, input extensionapi.ThreadResumeInput) {
	runtime, ok := goalRuntimeHandle(input.ThreadStore)
	if !ok {
		return
	}
	_ = runtime.RestoreAfterResume(ctx)
}

// OnThreadIdle does nothing for goals.
func (g *GoalExtension[C]) OnThreadIdle(context.Context, extensionapi.ThreadIdleInput) {}

// OnThreadStop does nothing for goals.
func (g *GoalExtension[C]) OnThreadStop(context.Context, extensionapi.ThreadStopInput) {}

// ---------------------------------------------------------------------------
// ConfigContributor
// ---------------------------------------------------------------------------

// OnConfigChanged re-evaluates goal enablement after a config change. Mirrors
// the Rust `ConfigContributor::on_config_changed` impl.
func (g *GoalExtension[C]) OnConfigChanged(_, threadStore *extensionapi.ExtensionData, _ C, newConfig C) {
	enabled := g.goalsEnabled(newConfig)
	extensionapi.ExtensionDataInsert(threadStore, goalExtensionConfigFromEnabled(enabled))
	if runtime, ok := goalRuntimeHandle(threadStore); ok {
		runtime.setEnabled(enabled)
	}
}

// ---------------------------------------------------------------------------
// TurnLifecycleContributor
// ---------------------------------------------------------------------------

// OnTurnStart begins accounting for the turn and marks the active goal. Mirrors
// the Rust `on_turn_start` impl.
func (g *GoalExtension[C]) OnTurnStart(ctx context.Context, input extensionapi.TurnStartInput) {
	runtime, ok := goalRuntimeHandle(input.ThreadStore)
	if !ok || !runtime.isEnabled() {
		return
	}
	accounting := runtime.accounting()
	accounting.startTurn(input.TurnID, input.CollaborationMode.Mode, input.TokenUsageAtTurnStart)
	if input.CollaborationMode.Mode == protocol.ModeKindPlan {
		accounting.clearCurrentTurnGoal()
		return
	}
	goal, err := g.stateDBs.ThreadGoals().GetThreadGoal(ctx, runtime.threadID())
	if err != nil {
		return
	}
	if goal != nil && (goal.Status == StateGoalStatusActive || goal.Status == StateGoalStatusBudgetLimited) {
		accounting.markTurnGoalActive(input.TurnID, goal.GoalID)
	}
}

// OnTurnStop flushes turn progress and finalizes accounting. Mirrors the Rust
// `on_turn_stop` impl.
func (g *GoalExtension[C]) OnTurnStop(ctx context.Context, input extensionapi.TurnStopInput) {
	runtime, ok := goalRuntimeHandle(input.ThreadStore)
	if !ok || !runtime.isEnabled() {
		return
	}
	turnID := input.TurnStore.LevelID()
	if _, err := runtime.accountActiveGoalProgress(ctx, turnID, turnID+":turn-stop", GoalAccountingModeActiveOnly, budgetLimitedClearActive); err != nil {
		return
	}
	runtime.accounting().finishTurn(turnID)
}

// OnTurnAbort flushes turn progress and finalizes accounting after an abort.
// Mirrors the Rust `on_turn_abort` impl.
func (g *GoalExtension[C]) OnTurnAbort(ctx context.Context, input extensionapi.TurnAbortInput) {
	runtime, ok := goalRuntimeHandle(input.ThreadStore)
	if !ok || !runtime.isEnabled() {
		return
	}
	turnID := input.TurnStore.LevelID()
	if _, err := runtime.accountActiveGoalProgress(ctx, turnID, turnID+":turn-abort", GoalAccountingModeActiveOnly, budgetLimitedClearActive); err != nil {
		return
	}
	runtime.accounting().finishTurn(turnID)
}

// OnTurnError usage-limits the active goal on a usage-limit error. Mirrors the
// Rust `on_turn_error` impl.
func (g *GoalExtension[C]) OnTurnError(ctx context.Context, input extensionapi.TurnErrorInput) {
	if input.Error.Kind != protocol.CodexErrorInfoUsageLimitExceeded {
		return
	}
	runtime, ok := goalRuntimeHandle(input.ThreadStore)
	if !ok {
		return
	}
	_ = runtime.UsageLimitActiveGoalForTurn(ctx, input.TurnID)
}

// ---------------------------------------------------------------------------
// TokenUsageContributor
// ---------------------------------------------------------------------------

// OnTokenUsage records the latest token usage for accounting. Mirrors the Rust
// `on_token_usage` impl.
func (g *GoalExtension[C]) OnTokenUsage(_ context.Context, _, threadStore, turnStore *extensionapi.ExtensionData, tokenUsage protocol.TokenUsageInfo) {
	runtime, ok := goalRuntimeHandle(threadStore)
	if !ok || !runtime.isEnabled() {
		return
	}
	runtime.accounting().recordTokenUsage(turnStore.LevelID(), tokenUsage.TotalTokenUsage)
}

// ---------------------------------------------------------------------------
// ToolLifecycleContributor
// ---------------------------------------------------------------------------

// OnToolStart does nothing for goals.
func (g *GoalExtension[C]) OnToolStart(context.Context, extensionapi.ToolStartInput) {}

// OnToolFinish accounts goal progress after a counted tool call and steers the
// turn when a budget limit is first reached. Mirrors the Rust `on_tool_finish`
// impl.
func (g *GoalExtension[C]) OnToolFinish(ctx context.Context, input extensionapi.ToolFinishInput) {
	runtime, ok := goalRuntimeHandle(input.ThreadStore)
	if !ok {
		return
	}
	shouldCount := runtime.isEnabled() &&
		toolAttemptCountsForGoalProgress(input.Outcome) &&
		!(input.ToolName.Namespace == nil && input.ToolName.Name == UpdateGoalToolName)
	if !shouldCount {
		return
	}
	turnID := input.TurnID
	progress, err := runtime.accountActiveGoalProgress(ctx, turnID, input.CallID, GoalAccountingModeActiveOnly, budgetLimitedKeepActive)
	if err != nil || progress == nil {
		return
	}
	goal := progress.goal
	if goal.Status != protocol.ThreadGoalStatusBudgetLimited {
		return
	}
	if !runtime.accounting().markBudgetLimitReportedIfNew(progress.goalID) {
		return
	}
	item := budgetLimitSteeringItem(goal)
	runtime.injectActiveTurnSteering(ctx, item)
}

// ---------------------------------------------------------------------------
// ToolContributor
// ---------------------------------------------------------------------------

// Tools returns the goal tools when they are visible for the thread. Mirrors the
// Rust `ToolContributor::tools` impl.
func (g *GoalExtension[C]) Tools(_, threadStore *extensionapi.ExtensionData) []tools.ToolExecutor[tools.ToolCall] {
	runtime, ok := goalRuntimeHandle(threadStore)
	if !ok || !runtime.toolsVisible() {
		return nil
	}
	return []tools.ToolExecutor[tools.ToolCall]{
		newGoalToolExecutor(goalToolKindGet, runtime.threadID(), g.stateDBs, runtime.accounting(), g.eventEmitter, g.metrics),
		newGoalToolExecutor(goalToolKindCreate, runtime.threadID(), g.stateDBs, runtime.accounting(), g.eventEmitter, g.metrics),
		newGoalToolExecutor(goalToolKindUpdate, runtime.threadID(), g.stateDBs, runtime.accounting(), g.eventEmitter, g.metrics),
	}
}

// InstallWithBackend installs the goal extension into the registry, wiring it as
// every contributor it implements. Mirrors the Rust `install_with_backend`.
func InstallWithBackend[C any](
	registry *extensionapi.ExtensionRegistryBuilder[C],
	stateDBs StateRuntime,
	metricsClient MetricsClient,
	threadManager ThreadManager,
	goalsEnabled GoalsEnabledFunc[C],
) {
	extension := newGoalExtensionWithHostCapabilities(
		stateDBs,
		registry.EventSink(),
		metricsClient,
		threadManager,
		goalsEnabled,
	)
	registry.AddThreadLifecycleContributor(extension)
	registry.AddConfigContributor(extension)
	registry.AddTurnLifecycleContributor(extension)
	registry.AddTokenUsageContributor(extension)
	registry.AddToolLifecycleContributor(extension)
	registry.AddToolContributor(extension)
}

// goalRuntimeHandle reads the per-thread goal runtime from the thread store.
// Mirrors the Rust `goal_runtime_handle` helper.
func goalRuntimeHandle(threadStore *extensionapi.ExtensionData) (GoalRuntimeHandle, bool) {
	return extensionapi.ExtensionDataGet[GoalRuntimeHandle](threadStore)
}

// toolAttemptCountsForGoalProgress reports whether a finished tool call counts
// toward goal progress. Mirrors the Rust `tool_attempt_counts_for_goal_progress`.
func toolAttemptCountsForGoalProgress(outcome extensionapi.ToolCallOutcome) bool {
	switch outcome.Kind {
	case extensionapi.ToolCallOutcomeCompleted:
		return true
	case extensionapi.ToolCallOutcomeFailed:
		return outcome.HandlerExecuted
	default:
		return false
	}
}

// isReviewSubAgent reports whether the session source is the review sub-agent.
// Mirrors the Rust `SessionSource::SubAgent(SubAgentSource::Review)` match.
func isReviewSubAgent(source rollout.SessionSource) bool {
	return source.Kind == rollout.SessionSourceKindSubAgent &&
		source.SubAgent != nil &&
		source.SubAgent.Kind == rollout.SubAgentSourceKindReview
}
