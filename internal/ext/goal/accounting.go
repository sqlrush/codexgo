package goal

import (
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// budgetLimitedGoalDisposition controls whether budget-limited accounting clears
// the active goal. Mirrors the Rust `BudgetLimitedGoalDisposition`.
type budgetLimitedGoalDisposition int

const (
	budgetLimitedKeepActive budgetLimitedGoalDisposition = iota
	budgetLimitedClearActive
)

// goalProgressSnapshot is a captured turn-progress delta. Mirrors the Rust
// `GoalProgressSnapshot`.
type goalProgressSnapshot struct {
	currentTokenUsage protocol.TokenUsage
	expectedGoalID    string
	timeDeltaSeconds  int64
	tokenDelta        int64
}

// idleGoalProgressSnapshot is a captured idle (wall-clock) progress delta.
// Mirrors the Rust `IdleGoalProgressSnapshot`.
type idleGoalProgressSnapshot struct {
	expectedGoalID   string
	timeDeltaSeconds int64
}

// recordedTokenDelta reports the per-turn and thread-wide unflushed token
// deltas. Mirrors the Rust `RecordedTokenDelta`.
type recordedTokenDelta struct {
	turnDelta            int64
	threadUnflushedDelta int64
}

// goalAccountingState tracks per-turn token accounting and wall-clock time for
// the active goal. Mirrors the Rust `GoalAccountingState`. All access is guarded
// by mu; the value is shared via *goalAccountingState.
type goalAccountingState struct {
	mu sync.Mutex

	curTurnID                 *string
	turns                     map[string]*goalTurnAccounting
	wallClock                 goalWallClockAccounting
	budgetLimitReportedGoalID *string
}

type goalTurnAccounting struct {
	currentTokenUsage       protocol.TokenUsage
	lastAccountedTokenUsage protocol.TokenUsage
	activeGoalID            *string
	accountTokens           bool
}

type goalWallClockAccounting struct {
	lastAccountedAt time.Time
	activeGoalID    *string
}

// newGoalAccountingState creates an empty accounting state. Mirrors the Rust
// `GoalAccountingState::default`.
func newGoalAccountingState() *goalAccountingState {
	return &goalAccountingState{
		turns:     make(map[string]*goalTurnAccounting),
		wallClock: goalWallClockAccounting{lastAccountedAt: time.Now()},
	}
}

// startTurn registers accounting for a new turn. Mirrors Rust
// `GoalAccountingState::start_turn`.
func (s *goalAccountingState) startTurn(turnID string, mode protocol.ModeKind, tokenUsageAtTurnStart protocol.TokenUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := turnID
	s.curTurnID = &id
	s.turns[turnID] = newGoalTurnAccounting(tokenUsageAtTurnStart, mode != protocol.ModeKindPlan)
}

// currentTurnID returns the active turn id, if any. Mirrors Rust
// `GoalAccountingState::current_turn_id`.
func (s *goalAccountingState) currentTurnID() *string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStringPtr(s.curTurnID)
}

// turnIsCurrentActiveGoal reports whether the turn is current and tracking an
// active goal. Mirrors Rust `GoalAccountingState::turn_is_current_active_goal`.
func (s *goalAccountingState) turnIsCurrentActiveGoal(turnID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ptrEqStr(s.curTurnID, turnID) {
		return false
	}
	turn, ok := s.turns[turnID]
	if !ok {
		return false
	}
	return turn.accountTokens && turn.activeGoalID != nil
}

// recordTokenUsage records the latest total token usage for a turn, returning
// the accountable delta when positive. Mirrors Rust
// `GoalAccountingState::record_token_usage`.
func (s *goalAccountingState) recordTokenUsage(turnID string, totalUsage protocol.TokenUsage) *recordedTokenDelta {
	s.mu.Lock()
	defer s.mu.Unlock()
	turn, ok := s.turns[turnID]
	if !ok {
		return nil
	}
	turn.currentTokenUsage = totalUsage
	if !turn.accountTokens {
		return nil
	}
	delta := turn.tokenDeltaSinceLastAccounting()
	if delta <= 0 {
		return nil
	}
	return &recordedTokenDelta{
		turnDelta:            delta,
		threadUnflushedDelta: s.threadUnflushedTokenDelta(),
	}
}

// markTurnGoalActive marks the goal active for a specific turn. Mirrors Rust
// `GoalAccountingState::mark_turn_goal_active`.
func (s *goalAccountingState) markTurnGoalActive(turnID, goalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ptrEqStr(s.budgetLimitReportedGoalID, goalID) {
		s.budgetLimitReportedGoalID = nil
	}
	turn, ok := s.turns[turnID]
	if !ok {
		return
	}
	id := goalID
	turn.activeGoalID = &id
	if ptrEqStr(s.curTurnID, turnID) {
		s.wallClock.markActiveGoal(goalID)
	}
}

// markCurrentTurnGoalActive marks the goal active for the current turn and resets
// its token baseline, returning the current turn id. Mirrors Rust
// `GoalAccountingState::mark_current_turn_goal_active`.
func (s *goalAccountingState) markCurrentTurnGoalActive(goalID string) *string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.curTurnID == nil {
		return nil
	}
	turnID := *s.curTurnID
	if !ptrEqStr(s.budgetLimitReportedGoalID, goalID) {
		s.budgetLimitReportedGoalID = nil
	}
	turn, ok := s.turns[turnID]
	if !ok {
		return nil
	}
	id := goalID
	turn.activeGoalID = &id
	turn.resetBaselineToCurrent()
	s.wallClock.markActiveGoal(goalID)
	out := turnID
	return &out
}

// markIdleGoalActive marks the wall-clock active goal while idle. Mirrors Rust
// `GoalAccountingState::mark_idle_goal_active`.
func (s *goalAccountingState) markIdleGoalActive(goalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ptrEqStr(s.budgetLimitReportedGoalID, goalID) {
		s.budgetLimitReportedGoalID = nil
	}
	s.wallClock.markActiveGoal(goalID)
}

// clearCurrentTurnGoal clears the active goal for the current turn, returning the
// turn id. Mirrors Rust `GoalAccountingState::clear_current_turn_goal`.
func (s *goalAccountingState) clearCurrentTurnGoal() *string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.curTurnID == nil {
		return nil
	}
	turnID := *s.curTurnID
	if turn, ok := s.turns[turnID]; ok {
		turn.activeGoalID = nil
	}
	s.wallClock.clearActiveGoal()
	s.budgetLimitReportedGoalID = nil
	out := turnID
	return &out
}

// clearActiveGoal clears the active goal everywhere. Mirrors Rust
// `GoalAccountingState::clear_active_goal`.
func (s *goalAccountingState) clearActiveGoal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.curTurnID != nil {
		if turn, ok := s.turns[*s.curTurnID]; ok {
			turn.activeGoalID = nil
		}
	}
	s.wallClock.clearActiveGoal()
	s.budgetLimitReportedGoalID = nil
}

// progressSnapshot captures a turn-progress delta, or nil when none. Mirrors
// Rust `GoalAccountingState::progress_snapshot`.
func (s *goalAccountingState) progressSnapshot(turnID string) *goalProgressSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	turn, ok := s.turns[turnID]
	if !ok || !turn.accountTokens {
		return nil
	}
	if turn.activeGoalID == nil {
		return nil
	}
	expectedGoalID := *turn.activeGoalID
	tokenDelta := turn.tokenDeltaSinceLastAccounting()
	var timeDeltaSeconds int64
	if ptrEqStr(s.wallClock.activeGoalID, expectedGoalID) {
		timeDeltaSeconds = s.wallClock.timeDeltaSinceLastAccounting()
	}
	if timeDeltaSeconds == 0 && tokenDelta <= 0 {
		return nil
	}
	return &goalProgressSnapshot{
		currentTokenUsage: turn.currentTokenUsage,
		expectedGoalID:    expectedGoalID,
		timeDeltaSeconds:  timeDeltaSeconds,
		tokenDelta:        tokenDelta,
	}
}

// idleProgressSnapshot captures an idle wall-clock delta, or nil. Mirrors Rust
// `GoalAccountingState::idle_progress_snapshot`.
func (s *goalAccountingState) idleProgressSnapshot() *idleGoalProgressSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wallClock.activeGoalID == nil {
		return nil
	}
	expectedGoalID := *s.wallClock.activeGoalID
	timeDeltaSeconds := s.wallClock.timeDeltaSinceLastAccounting()
	if timeDeltaSeconds == 0 {
		return nil
	}
	return &idleGoalProgressSnapshot{
		expectedGoalID:   expectedGoalID,
		timeDeltaSeconds: timeDeltaSeconds,
	}
}

// markProgressAccountedForStatus records that a turn-progress snapshot was
// flushed. Mirrors Rust `GoalAccountingState::mark_progress_accounted_for_status`.
func (s *goalAccountingState) markProgressAccountedForStatus(turnID string, snapshot *goalProgressSnapshot, status StateThreadGoalStatus, disposition budgetLimitedGoalDisposition) {
	clear := shouldClearActiveGoal(status, disposition)
	s.mu.Lock()
	defer s.mu.Unlock()
	if turn, ok := s.turns[turnID]; ok {
		turn.lastAccountedTokenUsage = snapshot.currentTokenUsage
		if clear {
			turn.activeGoalID = nil
		}
	}
	s.wallClock.markAccounted(snapshot.timeDeltaSeconds)
	if clear {
		s.wallClock.clearActiveGoal()
	}
	if status != StateGoalStatusBudgetLimited {
		s.budgetLimitReportedGoalID = nil
	}
}

// finishTurn removes accounting for a finished turn. Mirrors Rust
// `GoalAccountingState::finish_turn`.
func (s *goalAccountingState) finishTurn(turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.turns, turnID)
	if ptrEqStr(s.curTurnID, turnID) {
		s.curTurnID = nil
	}
}

// markIdleProgressAccountedForStatus records that an idle snapshot was flushed.
// Mirrors Rust `GoalAccountingState::mark_idle_progress_accounted_for_status`.
func (s *goalAccountingState) markIdleProgressAccountedForStatus(snapshot *idleGoalProgressSnapshot, status StateThreadGoalStatus, disposition budgetLimitedGoalDisposition) {
	clear := shouldClearActiveGoal(status, disposition)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wallClock.markAccounted(snapshot.timeDeltaSeconds)
	if clear {
		s.wallClock.clearActiveGoal()
	}
	if status != StateGoalStatusBudgetLimited {
		s.budgetLimitReportedGoalID = nil
	}
}

// resetIdleProgressBaselineAndClearActiveGoal resets the idle baseline and clears
// the active goal. Mirrors Rust
// `GoalAccountingState::reset_idle_progress_baseline_and_clear_active_goal`.
func (s *goalAccountingState) resetIdleProgressBaselineAndClearActiveGoal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wallClock.resetBaseline()
	s.wallClock.clearActiveGoal()
	s.budgetLimitReportedGoalID = nil
}

// markBudgetLimitReportedIfNew records a budget-limit report once per goal,
// returning whether this was the first report. Mirrors Rust
// `GoalAccountingState::mark_budget_limit_reported_if_new`.
func (s *goalAccountingState) markBudgetLimitReportedIfNew(goalID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ptrEqStr(s.budgetLimitReportedGoalID, goalID) {
		return false
	}
	id := goalID
	s.budgetLimitReportedGoalID = &id
	return true
}

// threadUnflushedTokenDelta sums positive accountable deltas across turns. The
// caller must hold mu. Mirrors Rust `GoalAccountingInner::thread_unflushed_token_delta`.
func (s *goalAccountingState) threadUnflushedTokenDelta() int64 {
	var total int64
	for _, turn := range s.turns {
		if !turn.accountTokens {
			continue
		}
		total = saturatingAddI64(total, maxI64(turn.tokenDeltaSinceLastAccounting(), 0))
	}
	return total
}

func newGoalTurnAccounting(currentTokenUsage protocol.TokenUsage, accountTokens bool) *goalTurnAccounting {
	return &goalTurnAccounting{
		currentTokenUsage:       currentTokenUsage,
		lastAccountedTokenUsage: currentTokenUsage,
		accountTokens:           accountTokens,
	}
}

func (t *goalTurnAccounting) resetBaselineToCurrent() {
	t.lastAccountedTokenUsage = t.currentTokenUsage
}

func (t *goalTurnAccounting) tokenDeltaSinceLastAccounting() int64 {
	return tokenDeltaSinceLastAccounting(t.lastAccountedTokenUsage, t.currentTokenUsage)
}

func (w *goalWallClockAccounting) timeDeltaSinceLastAccounting() int64 {
	elapsed := time.Since(w.lastAccountedAt).Seconds()
	if elapsed < 0 {
		return 0
	}
	if elapsed > float64(maxInt64) {
		return maxInt64
	}
	return int64(elapsed)
}

func (w *goalWallClockAccounting) markAccounted(accountedSeconds int64) {
	if accountedSeconds <= 0 {
		return
	}
	w.lastAccountedAt = w.lastAccountedAt.Add(time.Duration(accountedSeconds) * time.Second)
}

func (w *goalWallClockAccounting) resetBaseline() {
	w.lastAccountedAt = time.Now()
}

func (w *goalWallClockAccounting) markActiveGoal(goalID string) {
	if !ptrEqStr(w.activeGoalID, goalID) {
		w.resetBaseline()
		id := goalID
		w.activeGoalID = &id
	}
}

func (w *goalWallClockAccounting) clearActiveGoal() {
	w.activeGoalID = nil
	w.resetBaseline()
}

// tokenDeltaSinceLastAccounting computes the element-wise saturating delta and
// reduces it to a goal token delta. Mirrors the free Rust function of the same
// name.
func tokenDeltaSinceLastAccounting(last, current protocol.TokenUsage) int64 {
	delta := protocol.TokenUsage{
		InputTokens:           saturatingSubI64(current.InputTokens, last.InputTokens),
		CachedInputTokens:     saturatingSubI64(current.CachedInputTokens, last.CachedInputTokens),
		OutputTokens:          saturatingSubI64(current.OutputTokens, last.OutputTokens),
		ReasoningOutputTokens: saturatingSubI64(current.ReasoningOutputTokens, last.ReasoningOutputTokens),
		TotalTokens:           saturatingSubI64(current.TotalTokens, last.TotalTokens),
	}
	return goalTokenDeltaForUsage(delta)
}

// goalTokenDeltaForUsage reduces a token usage to its goal-accountable delta.
// Mirrors the Rust `goal_token_delta_for_usage`.
func goalTokenDeltaForUsage(usage protocol.TokenUsage) int64 {
	billable := saturatingSubI64(usage.InputTokens, usage.CachedInputTokens)
	return saturatingAddI64(billable, maxI64(usage.OutputTokens, 0))
}

// shouldClearActiveGoal mirrors the Rust `should_clear_active_goal` helper.
func shouldClearActiveGoal(status StateThreadGoalStatus, disposition budgetLimitedGoalDisposition) bool {
	switch status {
	case StateGoalStatusActive:
		return false
	case StateGoalStatusBudgetLimited:
		return disposition == budgetLimitedClearActive
	case StateGoalStatusPaused, StateGoalStatusBlocked, StateGoalStatusUsageLimited, StateGoalStatusComplete:
		return true
	default:
		return true
	}
}
