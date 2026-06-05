package state

// Tests for the SQLite-backed thread-goal store, mirroring the Rust
// `state/src/runtime/goals.rs` test scenarios (insert/update/get round-trips,
// budget-limit promotion, goal-version scoping, usage accounting modes).

import (
	"context"
	"testing"
)

// newGoalTestRuntime builds a state runtime rooted at a temp Codex home,
// mirroring the Rust `test_runtime` helper.
func newGoalTestRuntime(t *testing.T) *StateRuntime {
	t.Helper()
	runtime, err := InitRuntime(context.Background(), t.TempDir(), "test-provider")
	if err != nil {
		t.Fatalf("init state runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime
}

func goalTestThreadID() string { return "00000000-0000-0000-0000-000000000123" }

func int64Ptr(v int64) *int64 { return &v }

// TestInsertAndGetThreadGoal mirrors the insert/get round-trip of
// `replace_update_and_get_thread_goal` plus
// `insert_thread_goal_does_not_replace_existing_goal`.
func TestInsertAndGetThreadGoal(t *testing.T) {
	runtime := newGoalTestRuntime(t)
	ctx := context.Background()
	threadID := goalTestThreadID()

	inserted, err := runtime.ThreadGoals().InsertThreadGoal(ctx, threadID, "optimize the benchmark", GoalStatusActive, int64Ptr(100_000))
	if err != nil {
		t.Fatalf("insert goal: %v", err)
	}
	if inserted == nil {
		t.Fatal("insert should return the new goal")
	}
	if inserted.Objective != "optimize the benchmark" {
		t.Errorf("objective = %q", inserted.Objective)
	}
	if inserted.Status != GoalStatusActive {
		t.Errorf("status = %v, want active", inserted.Status)
	}
	if inserted.TokenBudget == nil || *inserted.TokenBudget != 100_000 {
		t.Errorf("token budget = %v, want 100000", inserted.TokenBudget)
	}
	if inserted.GoalID == "" {
		t.Error("goal id should be assigned")
	}

	got, err := runtime.ThreadGoals().GetThreadGoal(ctx, threadID)
	if err != nil {
		t.Fatalf("get goal: %v", err)
	}
	if got == nil || got.GoalID != inserted.GoalID {
		t.Fatalf("get returned %+v, want goal %s", got, inserted.GoalID)
	}

	// A second insert must not replace the existing goal.
	second, err := runtime.ThreadGoals().InsertThreadGoal(ctx, threadID, "different objective", GoalStatusActive, nil)
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if second != nil {
		t.Errorf("second insert should return nil, got %+v", second)
	}

	// A goal for an unknown thread reads as nil.
	none, err := runtime.ThreadGoals().GetThreadGoal(ctx, "00000000-0000-0000-0000-000000000999")
	if err != nil {
		t.Fatalf("get missing goal: %v", err)
	}
	if none != nil {
		t.Errorf("missing goal = %+v, want nil", none)
	}
}

// TestInsertThreadGoalAppliesBudgetLimitImmediately mirrors
// `insert_thread_goal_applies_budget_limit_immediately`.
func TestInsertThreadGoalAppliesBudgetLimitImmediately(t *testing.T) {
	runtime := newGoalTestRuntime(t)
	ctx := context.Background()

	inserted, err := runtime.ThreadGoals().InsertThreadGoal(ctx, goalTestThreadID(), "stay within budget", GoalStatusActive, int64Ptr(0))
	if err != nil {
		t.Fatalf("insert goal: %v", err)
	}
	if inserted == nil {
		t.Fatal("goal should be inserted")
	}
	if inserted.Status != GoalStatusBudgetLimited {
		t.Errorf("status = %v, want budget_limited", inserted.Status)
	}
	if inserted.TokensUsed != 0 || inserted.TimeUsedSeconds != 0 {
		t.Errorf("usage = %d tokens / %d s, want 0/0", inserted.TokensUsed, inserted.TimeUsedSeconds)
	}
}

// TestUpdateThreadGoalStatus covers terminal status updates plus the
// expected-goal-id scoping of `update_thread_goal_ignores_replaced_goal_version`.
func TestUpdateThreadGoalStatus(t *testing.T) {
	runtime := newGoalTestRuntime(t)
	ctx := context.Background()
	threadID := goalTestThreadID()

	inserted, err := runtime.ThreadGoals().InsertThreadGoal(ctx, threadID, "ship the feature", GoalStatusActive, nil)
	if err != nil || inserted == nil {
		t.Fatalf("insert goal: %v / %+v", err, inserted)
	}

	complete := GoalStatusComplete
	updated, err := runtime.ThreadGoals().UpdateThreadGoal(ctx, threadID, GoalUpdate{Status: &complete})
	if err != nil {
		t.Fatalf("update goal: %v", err)
	}
	if updated == nil || updated.Status != GoalStatusComplete {
		t.Fatalf("updated = %+v, want complete", updated)
	}

	// An update scoped to a stale goal id must not match.
	stale := "not-the-goal-id"
	blocked := GoalStatusBlocked
	missed, err := runtime.ThreadGoals().UpdateThreadGoal(ctx, threadID, GoalUpdate{Status: &blocked, ExpectedGoalID: &stale})
	if err != nil {
		t.Fatalf("stale update: %v", err)
	}
	if missed != nil {
		t.Errorf("stale update = %+v, want nil", missed)
	}

	// Updating a thread with no goal returns nil.
	none, err := runtime.ThreadGoals().UpdateThreadGoal(ctx, "00000000-0000-0000-0000-000000000999", GoalUpdate{Status: &complete})
	if err != nil {
		t.Fatalf("update missing: %v", err)
	}
	if none != nil {
		t.Errorf("update missing = %+v, want nil", none)
	}
}

// TestUpdateThreadGoalPausedAndBlockedPreserveBudgetLimited mirrors
// `pausing_budget_limited_goal_preserves_terminal_status` /
// `blocking_budget_limited_goal_preserves_terminal_status`: paused/blocked
// updates must not clobber a budget_limited goal.
func TestUpdateThreadGoalPausedAndBlockedPreserveBudgetLimited(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status ThreadGoalStatus
	}{
		{name: "paused", status: GoalStatusPaused},
		{name: "blocked", status: GoalStatusBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newGoalTestRuntime(t)
			ctx := context.Background()
			threadID := goalTestThreadID()

			if _, err := runtime.ThreadGoals().InsertThreadGoal(ctx, threadID, "over budget", GoalStatusActive, int64Ptr(0)); err != nil {
				t.Fatalf("insert goal: %v", err)
			}

			status := tc.status
			updated, err := runtime.ThreadGoals().UpdateThreadGoal(ctx, threadID, GoalUpdate{Status: &status})
			if err != nil {
				t.Fatalf("update goal: %v", err)
			}
			if updated == nil || updated.Status != GoalStatusBudgetLimited {
				t.Fatalf("updated = %+v, want budget_limited preserved", updated)
			}
		})
	}
}

// TestActivatingGoalOverBudgetKeepsItBudgetLimited mirrors
// `activating_goal_already_over_budget_keeps_it_budget_limited`.
func TestActivatingGoalOverBudgetKeepsItBudgetLimited(t *testing.T) {
	runtime := newGoalTestRuntime(t)
	ctx := context.Background()
	threadID := goalTestThreadID()

	if _, err := runtime.ThreadGoals().InsertThreadGoal(ctx, threadID, "over budget", GoalStatusActive, int64Ptr(10)); err != nil {
		t.Fatalf("insert goal: %v", err)
	}
	// Burn past the budget.
	outcome, err := runtime.ThreadGoals().AccountThreadGoalUsage(ctx, threadID, 0, 25, GoalAccountingModeActiveOnly, nil)
	if err != nil {
		t.Fatalf("account usage: %v", err)
	}
	if outcome.Kind != GoalAccountingOutcomeUpdated || outcome.Goal.Status != GoalStatusBudgetLimited {
		t.Fatalf("outcome = %+v, want budget_limited", outcome)
	}

	active := GoalStatusActive
	updated, err := runtime.ThreadGoals().UpdateThreadGoal(ctx, threadID, GoalUpdate{Status: &active})
	if err != nil {
		t.Fatalf("re-activate: %v", err)
	}
	if updated == nil || updated.Status != GoalStatusBudgetLimited {
		t.Fatalf("updated = %+v, want budget_limited kept", updated)
	}
}

// TestAccountThreadGoalUsage covers the accumulation + budget-limit promotion of
// `usage_accounting_updates_active_goals_and_accounts_budget_limited_in_flight_usage`
// and the zero-delta / scoped-goal-id Unchanged outcomes.
func TestAccountThreadGoalUsage(t *testing.T) {
	runtime := newGoalTestRuntime(t)
	ctx := context.Background()
	threadID := goalTestThreadID()

	inserted, err := runtime.ThreadGoals().InsertThreadGoal(ctx, threadID, "track usage", GoalStatusActive, int64Ptr(100))
	if err != nil || inserted == nil {
		t.Fatalf("insert goal: %v / %+v", err, inserted)
	}

	// Zero deltas leave the goal unchanged.
	outcome, err := runtime.ThreadGoals().AccountThreadGoalUsage(ctx, threadID, 0, 0, GoalAccountingModeActiveOnly, nil)
	if err != nil {
		t.Fatalf("zero-delta accounting: %v", err)
	}
	if outcome.Kind != GoalAccountingOutcomeUnchanged || outcome.Goal == nil {
		t.Fatalf("zero-delta outcome = %+v, want unchanged with goal", outcome)
	}

	// Usage below budget accumulates and stays active.
	outcome, err = runtime.ThreadGoals().AccountThreadGoalUsage(ctx, threadID, 30, 40, GoalAccountingModeActiveOnly, nil)
	if err != nil {
		t.Fatalf("accounting: %v", err)
	}
	if outcome.Kind != GoalAccountingOutcomeUpdated {
		t.Fatalf("outcome = %+v, want updated", outcome)
	}
	if outcome.Goal.TokensUsed != 40 || outcome.Goal.TimeUsedSeconds != 30 {
		t.Errorf("usage = %d tokens / %d s, want 40/30", outcome.Goal.TokensUsed, outcome.Goal.TimeUsedSeconds)
	}
	if outcome.Goal.Status != GoalStatusActive {
		t.Errorf("status = %v, want active", outcome.Goal.Status)
	}

	// Crossing the budget promotes the goal to budget_limited.
	outcome, err = runtime.ThreadGoals().AccountThreadGoalUsage(ctx, threadID, 0, 60, GoalAccountingModeActiveOnly, nil)
	if err != nil {
		t.Fatalf("budget-crossing accounting: %v", err)
	}
	if outcome.Kind != GoalAccountingOutcomeUpdated || outcome.Goal.Status != GoalStatusBudgetLimited {
		t.Fatalf("outcome = %+v, want budget_limited", outcome)
	}
	if outcome.Goal.TokensUsed != 100 {
		t.Errorf("tokens used = %d, want 100", outcome.Goal.TokensUsed)
	}

	// A stale expected goal id leaves the row unchanged.
	stale := "not-the-goal-id"
	outcome, err = runtime.ThreadGoals().AccountThreadGoalUsage(ctx, threadID, 0, 5, GoalAccountingModeActiveOnly, &stale)
	if err != nil {
		t.Fatalf("stale accounting: %v", err)
	}
	if outcome.Kind != GoalAccountingOutcomeUnchanged {
		t.Fatalf("stale outcome = %+v, want unchanged", outcome)
	}
}

// TestAccountThreadGoalUsageModes mirrors
// `active_status_only_usage_accounting_does_not_update_budget_limited_goals` and
// `stopped_usage_accounting_promotes_paused_goal_over_budget`: the accounting
// mode picks which statuses are eligible.
func TestAccountThreadGoalUsageModes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		seedStatus  ThreadGoalStatus
		mode        GoalAccountingMode
		wantUpdated bool
	}{
		{name: "active-status-only skips budget_limited", seedStatus: GoalStatusBudgetLimited, mode: GoalAccountingModeActiveStatusOnly, wantUpdated: false},
		{name: "active-only accounts budget_limited", seedStatus: GoalStatusBudgetLimited, mode: GoalAccountingModeActiveOnly, wantUpdated: true},
		{name: "active-only skips paused", seedStatus: GoalStatusPaused, mode: GoalAccountingModeActiveOnly, wantUpdated: false},
		{name: "active-or-stopped accounts paused", seedStatus: GoalStatusPaused, mode: GoalAccountingModeActiveOrStopped, wantUpdated: true},
		{name: "active-or-complete accounts complete", seedStatus: GoalStatusComplete, mode: GoalAccountingModeActiveOrComplete, wantUpdated: true},
		{name: "active-only skips complete", seedStatus: GoalStatusComplete, mode: GoalAccountingModeActiveOnly, wantUpdated: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newGoalTestRuntime(t)
			ctx := context.Background()
			threadID := goalTestThreadID()

			if _, err := runtime.ThreadGoals().InsertThreadGoal(ctx, threadID, "mode goal", GoalStatusActive, nil); err != nil {
				t.Fatalf("insert goal: %v", err)
			}
			seed := tc.seedStatus
			if seed != GoalStatusActive {
				if _, err := runtime.ThreadGoals().UpdateThreadGoal(ctx, threadID, GoalUpdate{Status: &seed}); err != nil {
					t.Fatalf("seed status: %v", err)
				}
			}

			outcome, err := runtime.ThreadGoals().AccountThreadGoalUsage(ctx, threadID, 1, 1, tc.mode, nil)
			if err != nil {
				t.Fatalf("account usage: %v", err)
			}
			gotUpdated := outcome.Kind == GoalAccountingOutcomeUpdated
			if gotUpdated != tc.wantUpdated {
				t.Errorf("updated = %v, want %v (outcome %+v)", gotUpdated, tc.wantUpdated, outcome)
			}
		})
	}
}

// TestUsageLimitActiveThreadGoal mirrors
// `usage_limit_active_thread_goal_updates_active_or_budget_limited_goals`.
func TestUsageLimitActiveThreadGoal(t *testing.T) {
	for _, tc := range []struct {
		name       string
		seedStatus ThreadGoalStatus
		wantHit    bool
	}{
		{name: "active goal usage-limits", seedStatus: GoalStatusActive, wantHit: true},
		{name: "budget_limited goal usage-limits", seedStatus: GoalStatusBudgetLimited, wantHit: true},
		{name: "complete goal unchanged", seedStatus: GoalStatusComplete, wantHit: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newGoalTestRuntime(t)
			ctx := context.Background()
			threadID := goalTestThreadID()

			if _, err := runtime.ThreadGoals().InsertThreadGoal(ctx, threadID, "limit me", GoalStatusActive, nil); err != nil {
				t.Fatalf("insert goal: %v", err)
			}
			seed := tc.seedStatus
			if seed != GoalStatusActive {
				if _, err := runtime.ThreadGoals().UpdateThreadGoal(ctx, threadID, GoalUpdate{Status: &seed}); err != nil {
					t.Fatalf("seed status: %v", err)
				}
			}

			limited, err := runtime.ThreadGoals().UsageLimitActiveThreadGoal(ctx, threadID)
			if err != nil {
				t.Fatalf("usage-limit goal: %v", err)
			}
			if tc.wantHit {
				if limited == nil || limited.Status != GoalStatusUsageLimited {
					t.Fatalf("limited = %+v, want usage_limited", limited)
				}
			} else if limited != nil {
				t.Fatalf("limited = %+v, want nil", limited)
			}
		})
	}
}

// TestDeleteThreadGoal mirrors the delete path: deleting an existing goal
// reports true, a second delete reports false.
func TestDeleteThreadGoal(t *testing.T) {
	runtime := newGoalTestRuntime(t)
	ctx := context.Background()
	threadID := goalTestThreadID()

	if _, err := runtime.ThreadGoals().InsertThreadGoal(ctx, threadID, "delete me", GoalStatusActive, nil); err != nil {
		t.Fatalf("insert goal: %v", err)
	}
	deleted, err := runtime.ThreadGoals().DeleteThreadGoal(ctx, threadID)
	if err != nil {
		t.Fatalf("delete goal: %v", err)
	}
	if !deleted {
		t.Error("delete should report true for an existing goal")
	}
	again, err := runtime.ThreadGoals().DeleteThreadGoal(ctx, threadID)
	if err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if again {
		t.Error("second delete should report false")
	}
}
