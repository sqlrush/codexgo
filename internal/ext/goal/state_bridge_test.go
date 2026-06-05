package goal

// Tests for the state-runtime bridge: the goal tools running over the real
// SQLite-backed state.StateRuntime goal store (instead of the in-memory fake).

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/state"
)

// newBridgeExecutors builds the goal tool executor trio over a temp-home state
// runtime, as the headless host wires them.
func newBridgeExecutors(t *testing.T) (get, create, update *goalToolExecutor) {
	t.Helper()
	runtime, err := state.InitRuntime(context.Background(), t.TempDir(), "test-provider")
	if err != nil {
		t.Fatalf("init state runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })

	threadID := protocol.NewThreadID("00000000-0000-0000-0000-000000000123")
	execs := NewToolExecutors(threadID, NewStateRuntimeBridge(runtime), nil, nil)
	if len(execs) != 3 {
		t.Fatalf("executor count = %d, want 3", len(execs))
	}
	var ok bool
	if get, ok = execs[0].(*goalToolExecutor); !ok || get.kind != goalToolKindGet {
		t.Fatalf("executor 0 = %T/%v, want get_goal", execs[0], get.kind)
	}
	if create, ok = execs[1].(*goalToolExecutor); !ok || create.kind != goalToolKindCreate {
		t.Fatalf("executor 1 = %T/%v, want create_goal", execs[1], create.kind)
	}
	if update, ok = execs[2].(*goalToolExecutor); !ok || update.kind != goalToolKindUpdate {
		t.Fatalf("executor 2 = %T/%v, want update_goal", execs[2], update.kind)
	}
	return get, create, update
}

// TestStateBridgeGoalRoundTrip drives create_goal → get_goal → update_goal
// through the SQLite-backed bridge.
func TestStateBridgeGoalRoundTrip(t *testing.T) {
	get, create, update := newBridgeExecutors(t)
	ctx := context.Background()

	// get_goal with no goal returns a null goal.
	out, err := get.Handle(ctx, makeToolCall("call-0", `{}`))
	if err != nil {
		t.Fatalf("get_goal (empty): %v", err)
	}
	if resp := decodeGoalResponse(t, out); resp.Goal != nil {
		t.Errorf("initial goal = %+v, want nil", resp.Goal)
	}

	// create_goal persists a new active goal.
	out, err = create.Handle(ctx, makeToolCall("call-1", `{"objective":"ship parity","token_budget":1000}`))
	if err != nil {
		t.Fatalf("create_goal: %v", err)
	}
	created := decodeGoalResponse(t, out)
	if created.Goal == nil || created.Goal.Objective != "ship parity" {
		t.Fatalf("created goal = %+v", created.Goal)
	}
	if created.Goal.Status != protocol.ThreadGoalStatusActive {
		t.Errorf("created status = %v, want active", created.Goal.Status)
	}
	if created.RemainingTokens == nil || *created.RemainingTokens != 1000 {
		t.Errorf("remaining tokens = %v, want 1000", created.RemainingTokens)
	}

	// A second create fails with the model-facing duplicate error.
	if _, err := create.Handle(ctx, makeToolCall("call-2", `{"objective":"another"}`)); err == nil {
		t.Error("second create_goal should fail")
	}

	// get_goal round-trips the persisted goal.
	out, err = get.Handle(ctx, makeToolCall("call-3", `{}`))
	if err != nil {
		t.Fatalf("get_goal: %v", err)
	}
	got := decodeGoalResponse(t, out)
	if got.Goal == nil || got.Goal.Objective != "ship parity" {
		t.Fatalf("got goal = %+v", got.Goal)
	}

	// update_goal marks it complete and includes the budget report.
	out, err = update.Handle(ctx, makeToolCall("call-4", `{"status":"complete"}`))
	if err != nil {
		t.Fatalf("update_goal: %v", err)
	}
	updated := decodeGoalResponse(t, out)
	if updated.Goal == nil || updated.Goal.Status != protocol.ThreadGoalStatusComplete {
		t.Fatalf("updated goal = %+v, want complete", updated.Goal)
	}
	if updated.CompletionBudgetReport == nil {
		t.Error("completion budget report should be present for a budgeted goal")
	}
}
