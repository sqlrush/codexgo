package core

// Tests for the goal-tool bridge executors: per-turn advertisement gating
// (Feature::Goals, review sub-agent exclusion, wiring presence) and dispatch
// through the SQLite-backed goal store. Mirrors spec_plan's
// `goal_tools_enabled` selection.

import (
	"context"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/ext/goal"
	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/state"
)

// newTestGoalExecutors builds the goal trio over a temp-home state runtime.
func newTestGoalExecutors(t *testing.T) []goalToolBridge {
	t.Helper()
	runtime, err := state.InitRuntime(context.Background(), t.TempDir(), "test-provider")
	if err != nil {
		t.Fatalf("init state runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	threadID := protocol.NewThreadID("00000000-0000-0000-0000-000000000123")
	return goal.NewToolExecutors(threadID, goal.NewStateRuntimeBridge(runtime), nil, nil)
}

// specNames resolves the advertised tool names for a turn.
func specNames(t *testing.T, router *DefaultToolRouter, tc *TurnContext) []string {
	t.Helper()
	specs, err := router.SpecsForTurn(context.Background(), tc)
	if err != nil {
		t.Fatalf("SpecsForTurn: %v", err)
	}
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name())
	}
	return names
}

// TestGoalToolsAdvertisedInPlanOrder asserts the goal trio advertises right
// after update_plan in spec_plan registration order under default features.
func TestGoalToolsAdvertisedInPlanOrder(t *testing.T) {
	router, err := BuiltinToolRouter(BuiltinToolDeps{
		Exec:      &mockExecService{},
		GoalTools: newTestGoalExecutors(t),
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}

	names := strings.Join(specNames(t, router, newTestTurn(t.TempDir())), ",")
	want := "update_plan,get_goal,create_goal,update_goal"
	if !strings.Contains(names, want) {
		t.Errorf("advertised order %q does not contain %q", names, want)
	}
}

// TestGoalToolsGating asserts the goal trio is hidden when the Goals feature is
// disabled, when the session is the review sub-agent, or when the host did not
// wire a goal store.
func TestGoalToolsGating(t *testing.T) {
	disabledFeatures := features.NewFeaturesWithDefaults()
	disabledFeatures.SetEnabled(features.FeatureGoals, false)

	reviewSource := rollout.SessionSource{
		Kind:     rollout.SessionSourceKindSubAgent,
		SubAgent: &rollout.SubAgentSource{Kind: rollout.SubAgentSourceKindReview},
	}

	tests := []struct {
		name      string
		goalTools []goalToolBridge
		mutate    func(tc *TurnContext)
		want      bool
	}{
		{
			name:      "advertised by default",
			goalTools: newTestGoalExecutors(t),
			mutate:    func(tc *TurnContext) {},
			want:      true,
		},
		{
			name:      "hidden when Goals feature disabled",
			goalTools: newTestGoalExecutors(t),
			mutate:    func(tc *TurnContext) { tc.Features = &disabledFeatures },
			want:      false,
		},
		{
			name:      "hidden for review sub-agent",
			goalTools: newTestGoalExecutors(t),
			mutate:    func(tc *TurnContext) { tc.SessionSource = reviewSource },
			want:      false,
		},
		{
			name:      "hidden when no goal store wired",
			goalTools: nil,
			mutate:    func(tc *TurnContext) {},
			want:      false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, err := BuiltinToolRouter(BuiltinToolDeps{
				Exec:      &mockExecService{},
				GoalTools: tc.goalTools,
			})
			if err != nil {
				t.Fatalf("build router: %v", err)
			}
			turn := newTestTurn(t.TempDir())
			tc.mutate(turn)
			names := strings.Join(specNames(t, router, turn), ",")
			got := strings.Contains(names, "get_goal")
			if got != tc.want {
				t.Errorf("get_goal advertised = %v, want %v (names %s)", got, tc.want, names)
			}
		})
	}
}

// TestGoalToolDispatch drives create_goal then get_goal through the router.
func TestGoalToolDispatch(t *testing.T) {
	router, err := BuiltinToolRouter(BuiltinToolDeps{
		Exec:      &mockExecService{},
		GoalTools: newTestGoalExecutors(t),
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	turn := newTestTurn(t.TempDir())
	ctx := context.Background()

	created, err := router.DispatchAny(ctx, turn, ToolInvocation{
		Name:      protocol.PlainToolName("create_goal"),
		CallID:    "call-1",
		Arguments: []byte(`{"objective":"finish the port"}`),
	})
	if err != nil {
		t.Fatalf("dispatch create_goal: %v", err)
	}
	if text := toolOutputText(created.Output, created.CallID, created.Payload); !strings.Contains(text, "finish the port") {
		t.Errorf("create_goal output = %q", text)
	}

	got, err := router.DispatchAny(ctx, turn, ToolInvocation{
		Name:      protocol.PlainToolName("get_goal"),
		CallID:    "call-2",
		Arguments: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("dispatch get_goal: %v", err)
	}
	if text := toolOutputText(got.Output, got.CallID, got.Payload); !strings.Contains(text, "finish the port") {
		t.Errorf("get_goal output = %q", text)
	}
}
