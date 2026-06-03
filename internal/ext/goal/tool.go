package goal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// maxThreadGoalObjectiveChars caps a goal objective length. Mirrors the Rust
// `MAX_THREAD_GOAL_OBJECTIVE_CHARS` constant.
const maxThreadGoalObjectiveChars = 4000

// goalToolKind selects which goal tool a [goalToolExecutor] handles. Mirrors the
// Rust `GoalToolKind`.
type goalToolKind int

const (
	goalToolKindGet goalToolKind = iota
	goalToolKindCreate
	goalToolKindUpdate
)

// goalToolExecutor is the runtime for one goal tool. Mirrors the Rust
// `GoalToolExecutor`.
type goalToolExecutor struct {
	kind            goalToolKind
	threadID        protocol.ThreadID
	stateDB         StateRuntime
	accountingState *goalAccountingState
	eventEmitter    goalEventEmitter
	metrics         GoalMetrics
}

// createGoalRequest is the create_goal tool input. Mirrors the Rust
// `CreateGoalRequest` (`rename_all = "snake_case"`).
type createGoalRequest struct {
	Objective   string `json:"objective"`
	TokenBudget *int64 `json:"token_budget"`
}

// updateGoalArgs is the update_goal tool input. Mirrors the Rust
// `UpdateGoalArgs` (`rename_all = "snake_case"`).
type updateGoalArgs struct {
	Status protocol.ThreadGoalStatus `json:"status"`
}

// goalToolResponse is the JSON body returned by every goal tool. Mirrors the
// Rust `GoalToolResponse` (`rename_all = "camelCase"`).
type goalToolResponse struct {
	Goal                   *protocol.ThreadGoal `json:"goal"`
	RemainingTokens        *int64               `json:"remainingTokens"`
	CompletionBudgetReport *string              `json:"completionBudgetReport"`
}

// completionBudgetReportMode controls whether a completion budget report is
// included. Mirrors the Rust `CompletionBudgetReport` enum.
type completionBudgetReportMode int

const (
	completionBudgetReportInclude completionBudgetReportMode = iota
	completionBudgetReportOmit
)

func newGoalToolExecutor(kind goalToolKind, threadID protocol.ThreadID, stateDB StateRuntime, accountingState *goalAccountingState, eventEmitter goalEventEmitter, metrics GoalMetrics) *goalToolExecutor {
	return &goalToolExecutor{
		kind:            kind,
		threadID:        threadID,
		stateDB:         stateDB,
		accountingState: accountingState,
		eventEmitter:    eventEmitter,
		metrics:         metrics,
	}
}

// ToolName returns the concrete tool name. Mirrors Rust `tool_name`.
func (e *goalToolExecutor) ToolName() protocol.ToolName {
	switch e.kind {
	case goalToolKindGet:
		return protocol.PlainToolName(GetGoalToolName)
	case goalToolKindCreate:
		return protocol.PlainToolName(CreateGoalToolName)
	default:
		return protocol.PlainToolName(UpdateGoalToolName)
	}
}

// Spec returns the model-visible tool spec. Mirrors Rust `spec`.
func (e *goalToolExecutor) Spec() tools.ToolSpec {
	switch e.kind {
	case goalToolKindGet:
		return createGetGoalTool()
	case goalToolKindCreate:
		return createCreateGoalTool()
	default:
		return createUpdateGoalTool()
	}
}

// Exposure reports the tool is surfaced directly to the model. The Rust
// ToolExecutor default exposure is Direct.
func (e *goalToolExecutor) Exposure() tools.ToolExposure {
	return tools.ToolExposureDirect
}

// SupportsParallelToolCalls reports the tool runs sequentially. The Rust default
// is false.
func (e *goalToolExecutor) SupportsParallelToolCalls() bool {
	return false
}

// Handle dispatches the invocation to the kind-specific handler. Mirrors Rust
// `handle`.
func (e *goalToolExecutor) Handle(ctx context.Context, invocation tools.ToolCall) (tools.ToolOutput, error) {
	switch e.kind {
	case goalToolKindGet:
		return e.handleGet(ctx, invocation)
	case goalToolKindCreate:
		return e.handleCreate(ctx, invocation)
	default:
		return e.handleUpdate(ctx, invocation)
	}
}

func (e *goalToolExecutor) handleGet(ctx context.Context, invocation tools.ToolCall) (tools.ToolOutput, error) {
	if _, err := invocation.FunctionArguments(); err != nil {
		return nil, err
	}
	goal, err := e.stateDB.ThreadGoals().GetThreadGoal(ctx, e.threadID)
	if err != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to read goal: %v", err))
	}
	return goalResponse(protocolGoalPtrFromState(goal), completionBudgetReportOmit)
}

func (e *goalToolExecutor) handleCreate(ctx context.Context, invocation tools.ToolCall) (tools.ToolOutput, error) {
	args, err := invocation.FunctionArguments()
	if err != nil {
		return nil, err
	}
	var request createGoalRequest
	if err := parseArguments(args, &request); err != nil {
		return nil, err
	}
	request.Objective = strings.TrimSpace(request.Objective)
	if msg := validateThreadGoalObjective(request.Objective); msg != "" {
		return nil, tools.RespondToModelError(msg)
	}
	if msg := validateGoalBudget(request.TokenBudget); msg != "" {
		return nil, tools.RespondToModelError(msg)
	}

	goal, err := e.stateDB.ThreadGoals().InsertThreadGoal(ctx, e.threadID, request.Objective, StateGoalStatusActive, request.TokenBudget)
	if err != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to create goal: %v", err))
	}
	if goal == nil {
		return nil, tools.RespondToModelError(
			"cannot create a new goal because this thread already has a goal; use update_goal only when the existing goal is complete",
		)
	}
	e.fillEmptyThreadPreviewIfPossible(ctx, *goal)
	turnID := e.accountingState.markCurrentTurnGoalActive(goal.GoalID)
	e.metrics.recordCreated()
	protocolGoal := protocolGoalFromState(*goal)
	e.emitGoalUpdatedFromToolCall(invocation, turnID, protocolGoal)
	return goalResponse(&protocolGoal, completionBudgetReportOmit)
}

func (e *goalToolExecutor) handleUpdate(ctx context.Context, invocation tools.ToolCall) (tools.ToolOutput, error) {
	args, err := invocation.FunctionArguments()
	if err != nil {
		return nil, err
	}
	var parsed updateGoalArgs
	if err := parseArguments(args, &parsed); err != nil {
		return nil, err
	}
	if parsed.Status != protocol.ThreadGoalStatusComplete && parsed.Status != protocol.ThreadGoalStatusBlocked {
		return nil, tools.RespondToModelError(
			"update_goal can only mark the existing goal complete or blocked; pause, resume, budget-limited, and usage-limited status changes are controlled by the user or system",
		)
	}

	var mode GoalAccountingMode
	switch parsed.Status {
	case protocol.ThreadGoalStatusComplete:
		mode = GoalAccountingModeActiveOrComplete
	case protocol.ThreadGoalStatusBlocked:
		mode = GoalAccountingModeActiveOrStopped
	}
	if _, err := e.accountActiveGoalProgress(ctx, mode, invocation.CallID, budgetLimitedClearActive); err != nil {
		return nil, err
	}

	previousStatus, err := e.currentGoalStatusForMetrics(ctx, nil)
	if err != nil {
		return nil, err
	}
	newStatus := stateStatusFromProtocol(parsed.Status)
	goal, err := e.stateDB.ThreadGoals().UpdateThreadGoal(ctx, e.threadID, GoalUpdate{Status: &newStatus})
	if err != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to update goal: %v", err))
	}
	if goal == nil {
		return nil, tools.RespondToModelError("cannot update goal because this thread has no goal")
	}
	e.metrics.recordTerminalIfStatusChanged(previousStatus, goal)
	protocolGoal := protocolGoalFromState(*goal)
	turnID := e.accountingState.clearCurrentTurnGoal()
	e.emitGoalUpdatedFromToolCall(invocation, turnID, protocolGoal)
	reportMode := completionBudgetReportOmit
	if parsed.Status == protocol.ThreadGoalStatusComplete {
		reportMode = completionBudgetReportInclude
	}
	return goalResponse(&protocolGoal, reportMode)
}

func (e *goalToolExecutor) emitGoalUpdatedFromToolCall(invocation tools.ToolCall, turnID *string, goal protocol.ThreadGoal) {
	e.eventEmitter.threadGoalUpdated(invocation.CallID, turnID, goal)
}

// accountActiveGoalProgress flushes pending turn progress within a tool handler.
// Mirrors the Rust `GoalToolExecutor::account_active_goal_progress`.
func (e *goalToolExecutor) accountActiveGoalProgress(ctx context.Context, mode GoalAccountingMode, eventID string, disposition budgetLimitedGoalDisposition) (*protocol.ThreadGoal, error) {
	turnIDPtr := e.accountingState.currentTurnID()
	if turnIDPtr == nil {
		return nil, nil
	}
	turnID := *turnIDPtr
	snapshot := e.accountingState.progressSnapshot(turnID)
	if snapshot == nil {
		return nil, nil
	}
	previousStatus, err := e.currentGoalStatusForMetrics(ctx, &snapshot.expectedGoalID)
	if err != nil {
		return nil, err
	}
	expectedGoalID := snapshot.expectedGoalID
	outcome, err := e.stateDB.ThreadGoals().AccountThreadGoalUsage(ctx, e.threadID, snapshot.timeDeltaSeconds, snapshot.tokenDelta, mode, &expectedGoalID)
	if err != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to account goal progress: %v", err))
	}
	if outcome.Kind != GoalAccountingOutcomeUpdated || outcome.Goal == nil {
		return nil, nil
	}
	stateGoal := *outcome.Goal
	e.metrics.recordTerminalIfStatusChanged(previousStatus, &stateGoal)
	e.accountingState.markProgressAccountedForStatus(turnID, snapshot, stateGoal.Status, disposition)
	protocolGoal := protocolGoalFromState(stateGoal)
	turnIDCopy := turnID
	e.eventEmitter.threadGoalUpdated(eventID, &turnIDCopy, protocolGoal)
	return &protocolGoal, nil
}

func (e *goalToolExecutor) currentGoalStatusForMetrics(ctx context.Context, expectedGoalID *string) (*StateThreadGoalStatus, error) {
	goal, err := e.stateDB.ThreadGoals().GetThreadGoal(ctx, e.threadID)
	if err != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to read goal metrics status: %v", err))
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

func (e *goalToolExecutor) fillEmptyThreadPreviewIfPossible(ctx context.Context, goal StateThreadGoal) {
	// Failures are intentionally ignored, matching the Rust warn-and-continue.
	_, _ = e.stateDB.SetThreadPreviewIfEmpty(ctx, e.threadID, goal.Objective)
}

// parseArguments decodes JSON tool arguments, returning a model-facing error on
// failure. Mirrors the Rust `parse_arguments`.
func parseArguments(arguments string, out any) error {
	if err := json.Unmarshal([]byte(arguments), out); err != nil {
		return tools.RespondToModelError(err.Error())
	}
	return nil
}

// validateGoalBudget rejects non-positive budgets. Mirrors the Rust
// `validate_goal_budget`; returns an empty string when valid.
func validateGoalBudget(value *int64) string {
	if value != nil && *value <= 0 {
		return "goal budgets must be positive when provided"
	}
	return ""
}

// validateThreadGoalObjective rejects empty or oversized objectives. Mirrors the
// Rust `validate_thread_goal_objective` (ported here because the protocol package
// does not yet expose it); returns an empty string when valid.
func validateThreadGoalObjective(value string) string {
	if value == "" {
		return "goal objective must not be empty"
	}
	if len([]rune(value)) > maxThreadGoalObjectiveChars {
		return fmt.Sprintf("goal objective must be at most %d characters", maxThreadGoalObjectiveChars)
	}
	return ""
}

// goalResponse builds the JSON tool output. Mirrors the Rust `goal_response`.
func goalResponse(goal *protocol.ThreadGoal, reportMode completionBudgetReportMode) (tools.ToolOutput, error) {
	value, err := json.Marshal(newGoalToolResponse(goal, reportMode))
	if err != nil {
		return nil, tools.FatalError(err.Error())
	}
	out := tools.NewJsonToolOutput(value)
	return out, nil
}

// newGoalToolResponse computes the response fields. Mirrors the Rust
// `GoalToolResponse::new`.
func newGoalToolResponse(goal *protocol.ThreadGoal, reportMode completionBudgetReportMode) goalToolResponse {
	var remainingTokens *int64
	if goal != nil && goal.TokenBudget != nil {
		remaining := *goal.TokenBudget - goal.TokensUsed
		if remaining < 0 {
			remaining = 0
		}
		remainingTokens = &remaining
	}
	var report *string
	if reportMode == completionBudgetReportInclude && goal != nil && goal.Status == protocol.ThreadGoalStatusComplete {
		report = completionBudgetReport(goal)
	}
	return goalToolResponse{
		Goal:                   goal,
		RemainingTokens:        remainingTokens,
		CompletionBudgetReport: report,
	}
}

// completionBudgetReport returns the completion budget report text when the goal
// carries budget/time data. Mirrors the Rust `completion_budget_report`.
func completionBudgetReport(goal *protocol.ThreadGoal) *string {
	if goal.TokenBudget == nil && goal.TimeUsedSeconds <= 0 {
		return nil
	}
	report := "Goal achieved. Report final usage from this tool result's structured goal fields. If `goal.tokenBudget` is present, include token usage from `goal.tokensUsed` and `goal.tokenBudget`. If `goal.timeUsedSeconds` is greater than 0, summarize elapsed time in a concise, human-friendly form appropriate to the response language."
	return &report
}

// protocolGoalFromState converts a persisted goal into the protocol form.
// Mirrors the Rust `protocol_goal_from_state`.
func protocolGoalFromState(goal StateThreadGoal) protocol.ThreadGoal {
	return protocol.ThreadGoal{
		ThreadID:        goal.ThreadID,
		Objective:       goal.Objective,
		Status:          protocolStatusFromState(goal.Status),
		TokenBudget:     goal.TokenBudget,
		TokensUsed:      goal.TokensUsed,
		TimeUsedSeconds: goal.TimeUsedSeconds,
		CreatedAt:       unixSeconds(goal.CreatedAt),
		UpdatedAt:       unixSeconds(goal.UpdatedAt),
	}
}

// protocolGoalPtrFromState converts an optional persisted goal.
func protocolGoalPtrFromState(goal *StateThreadGoal) *protocol.ThreadGoal {
	if goal == nil {
		return nil
	}
	out := protocolGoalFromState(*goal)
	return &out
}

// unixSeconds returns the unix-second timestamp for a wall-clock time, mirroring
// the Rust `DateTime::timestamp`.
func unixSeconds(t time.Time) int64 {
	return t.Unix()
}

// protocolStatusFromState maps a persisted status to the protocol status.
// Mirrors the Rust `protocol_status_from_state`.
func protocolStatusFromState(status StateThreadGoalStatus) protocol.ThreadGoalStatus {
	switch status {
	case StateGoalStatusActive:
		return protocol.ThreadGoalStatusActive
	case StateGoalStatusPaused:
		return protocol.ThreadGoalStatusPaused
	case StateGoalStatusBlocked:
		return protocol.ThreadGoalStatusBlocked
	case StateGoalStatusUsageLimited:
		return protocol.ThreadGoalStatusUsageLimited
	case StateGoalStatusBudgetLimited:
		return protocol.ThreadGoalStatusBudgetLimited
	default:
		return protocol.ThreadGoalStatusComplete
	}
}

// stateStatusFromProtocol maps a protocol status to the persisted status.
// Mirrors the Rust `state_status_from_protocol`.
func stateStatusFromProtocol(status protocol.ThreadGoalStatus) StateThreadGoalStatus {
	switch status {
	case protocol.ThreadGoalStatusActive:
		return StateGoalStatusActive
	case protocol.ThreadGoalStatusPaused:
		return StateGoalStatusPaused
	case protocol.ThreadGoalStatusBlocked:
		return StateGoalStatusBlocked
	case protocol.ThreadGoalStatusUsageLimited:
		return StateGoalStatusUsageLimited
	case protocol.ThreadGoalStatusBudgetLimited:
		return StateGoalStatusBudgetLimited
	default:
		return StateGoalStatusComplete
	}
}
