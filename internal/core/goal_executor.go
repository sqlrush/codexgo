package core

// goal_executor bridges the ext/goal tool-executor trio (get_goal /
// create_goal / update_goal) into the core tool router, porting spec_plan's
// goal registration:
//
//	if goal_tools_enabled(turn_context) {
//	    planned_tools.add(GetGoalHandler);
//	    planned_tools.add(CreateGoalHandler);
//	    planned_tools.add(UpdateGoalHandler);
//	}
//
// where goal_tools_enabled = turn_context.goal_tools_supported (state DB
// present and the thread is not ephemeral — represented here by the host
// wiring BuiltinToolDeps.GoalTools) && Feature::Goals && the session is not
// the review sub-agent.

import (
	"context"

	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/tools"
)

// goalToolBridge is the generic goal-tool runtime contract the core router
// hosts: the shared [tools.ToolExecutor] surface instantiated for core tool
// calls (an alias, so ext/goal executor slices wire in directly). The
// concrete implementations live in internal/ext/goal.
type goalToolBridge = tools.ToolExecutor[tools.ToolCall]

// turnGoalToolsEnabled reports whether the goal tools are advertised this turn.
// Mirrors `TurnContext::goal_tools_enabled` (Feature::Goals) combined with
// spec_plan's review-sub-agent exclusion. The Rust goal_tools_supported half
// (persistent state DB present, non-ephemeral thread) is decided at wiring
// time: hosts without a goal store leave BuiltinToolDeps.GoalTools nil.
func turnGoalToolsEnabled(tc *TurnContext) bool {
	if !turnFeatures(tc).Enabled(features.FeatureGoals) {
		return false
	}
	return !turnIsReviewSubAgent(tc)
}

// turnIsReviewSubAgent reports whether the turn runs inside the review
// sub-agent. Mirrors the Rust `SessionSource::SubAgent(SubAgentSource::Review)`
// match in spec_plan's goal_tools_enabled.
func turnIsReviewSubAgent(tc *TurnContext) bool {
	if tc == nil {
		return false
	}
	var source rollout.SessionSource
	switch v := tc.SessionSource.(type) {
	case rollout.SessionSource:
		source = v
	case *rollout.SessionSource:
		if v == nil {
			return false
		}
		source = *v
	default:
		return false
	}
	return source.Kind == rollout.SessionSourceKindSubAgent &&
		source.SubAgent != nil &&
		source.SubAgent.Kind == rollout.SubAgentSourceKindReview
}

// goalExecutor adapts one ext/goal tool executor onto the core [ToolExecutor]
// contract, applying the per-turn advertisement gate.
type goalExecutor struct {
	inner goalToolBridge
}

func (e goalExecutor) Name() protocol.ToolName { return e.inner.ToolName() }

// Spec advertises the goal tool spec when goal tools are enabled this turn.
func (e goalExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	if !turnGoalToolsEnabled(tc) {
		return tools.ToolSpec{}, false
	}
	return e.inner.Spec(), true
}

func (e goalExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

// Handle forwards the invocation to the ext/goal executor.
func (e goalExecutor) Handle(ctx context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	turnID := ""
	if h.Turn != nil {
		turnID = h.Turn.SubID
	}
	return e.inner.Handle(ctx, tools.ToolCall{
		TurnID:   turnID,
		CallID:   h.CallID,
		ToolName: h.ToolName,
		Payload:  h.Payload,
	})
}
