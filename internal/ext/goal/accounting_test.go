package goal

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func usage(input, cached, output int64) protocol.TokenUsage {
	return protocol.TokenUsage{
		InputTokens:       input,
		CachedInputTokens: cached,
		OutputTokens:      output,
		TotalTokens:       input + output,
	}
}

func TestGoalTokenDeltaForUsage(t *testing.T) {
	tests := []struct {
		name  string
		usage protocol.TokenUsage
		want  int64
	}{
		{"input minus cached plus output", usage(100, 30, 20), 90},
		// Rust saturating_sub on i64 clamps at i64 bounds, not zero, so a cached
		// count above input yields a negative billable input: 10-50+5 = -35.
		{"cached exceeds input goes negative", usage(10, 50, 5), -35},
		{"negative output clamped", protocol.TokenUsage{InputTokens: 10, OutputTokens: -3}, 10},
		{"all zero", usage(0, 0, 0), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := goalTokenDeltaForUsage(tc.usage); got != tc.want {
				t.Errorf("goalTokenDeltaForUsage = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestStartTurnPlanModeDoesNotAccountTokens(t *testing.T) {
	s := newGoalAccountingState()
	s.startTurn("turn-plan", protocol.ModeKindPlan, usage(0, 0, 0))
	// Plan turns never account tokens, so no delta is recorded.
	if d := s.recordTokenUsage("turn-plan", usage(100, 0, 50)); d != nil {
		t.Fatalf("plan turn recorded a delta: %+v", d)
	}
}

func TestRecordTokenUsageProducesDelta(t *testing.T) {
	s := newGoalAccountingState()
	s.startTurn("turn", protocol.ModeKindDefault, usage(0, 0, 0))
	d := s.recordTokenUsage("turn", usage(100, 20, 30))
	if d == nil {
		t.Fatal("expected a delta")
	}
	// input(100) - cached(20) + output(30) = 110
	if d.turnDelta != 110 {
		t.Errorf("turnDelta = %d, want 110", d.turnDelta)
	}
	// Unknown turn yields no delta.
	if s.recordTokenUsage("missing", usage(1, 0, 1)) != nil {
		t.Error("unknown turn should not record")
	}
}

func TestProgressSnapshotRequiresActiveGoal(t *testing.T) {
	s := newGoalAccountingState()
	s.startTurn("turn", protocol.ModeKindDefault, usage(0, 0, 0))
	s.recordTokenUsage("turn", usage(100, 0, 0))
	// No active goal yet -> no snapshot.
	if snap := s.progressSnapshot("turn"); snap != nil {
		t.Fatalf("snapshot without active goal: %+v", snap)
	}
	s.markTurnGoalActive("turn", "goal-1")
	snap := s.progressSnapshot("turn")
	if snap == nil {
		t.Fatal("expected snapshot after marking active goal")
	}
	if snap.expectedGoalID != "goal-1" {
		t.Errorf("expectedGoalID = %q", snap.expectedGoalID)
	}
	if snap.tokenDelta != 100 {
		t.Errorf("tokenDelta = %d, want 100", snap.tokenDelta)
	}
}

func TestMarkProgressAccountedAdvancesBaseline(t *testing.T) {
	s := newGoalAccountingState()
	s.startTurn("turn", protocol.ModeKindDefault, usage(0, 0, 0))
	s.markCurrentTurnGoalActive("goal-1")
	s.recordTokenUsage("turn", usage(100, 0, 0))
	snap := s.progressSnapshot("turn")
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	s.markProgressAccountedForStatus("turn", snap, StateGoalStatusActive, budgetLimitedKeepActive)
	// Baseline advanced; no further delta until more usage arrives.
	if again := s.progressSnapshot("turn"); again != nil {
		t.Fatalf("expected no further snapshot, got %+v", again)
	}
	// More usage produces a fresh delta.
	s.recordTokenUsage("turn", usage(150, 0, 0))
	if again := s.progressSnapshot("turn"); again == nil || again.tokenDelta != 50 {
		t.Fatalf("expected delta 50, got %+v", again)
	}
}

func TestClearAndFinishTurn(t *testing.T) {
	s := newGoalAccountingState()
	s.startTurn("turn", protocol.ModeKindDefault, usage(0, 0, 0))
	s.markCurrentTurnGoalActive("goal-1")
	if id := s.clearCurrentTurnGoal(); id == nil || *id != "turn" {
		t.Fatalf("clearCurrentTurnGoal = %v", id)
	}
	if s.turnIsCurrentActiveGoal("turn") {
		t.Error("goal should be cleared")
	}
	s.finishTurn("turn")
	if s.currentTurnID() != nil {
		t.Error("current turn id should be cleared after finish")
	}
}

func TestBudgetLimitReportedOncePerGoal(t *testing.T) {
	s := newGoalAccountingState()
	if !s.markBudgetLimitReportedIfNew("goal-1") {
		t.Fatal("first report should be new")
	}
	if s.markBudgetLimitReportedIfNew("goal-1") {
		t.Fatal("second report for same goal should not be new")
	}
	if !s.markBudgetLimitReportedIfNew("goal-2") {
		t.Fatal("different goal should be new")
	}
}

func TestMarkTurnGoalActiveResetsBudgetReportForDifferentGoal(t *testing.T) {
	s := newGoalAccountingState()
	s.startTurn("turn", protocol.ModeKindDefault, usage(0, 0, 0))
	s.markBudgetLimitReportedIfNew("goal-1")
	// Marking a different goal active clears the prior budget-limit report.
	s.markTurnGoalActive("turn", "goal-2")
	if !s.markBudgetLimitReportedIfNew("goal-2") {
		t.Fatal("budget report should have been reset for goal-2")
	}
}

func TestSaturatingArithmetic(t *testing.T) {
	if got := saturatingSubI64(5, 8); got != -3 {
		t.Errorf("saturatingSubI64(5,8) = %d, want -3", got)
	}
	if got := saturatingSubI64(maxInt64, -1); got != maxInt64 {
		t.Errorf("saturatingSubI64 overflow not clamped: %d", got)
	}
	if got := saturatingAddI64(maxInt64, 1); got != maxInt64 {
		t.Errorf("saturatingAddI64 overflow not clamped: %d", got)
	}
	if got := maxI64(3, 7); got != 7 {
		t.Errorf("maxI64(3,7) = %d", got)
	}
}
