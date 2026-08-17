package core

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// The token-budget tools of upstream 0.147 (`get_context_remaining` and
// `new_context`; spec 50 D0.3). Both are registered unconditionally and gated
// per turn on TurnContext.TokenBudget (spec_plan `features.enabled(TokenBudget)`),
// so a session that never enables the token budget never advertises them.

const (
	getContextRemainingToolName = "get_context_remaining"
	newContextWindowToolName    = "new_context"
	// newContextWindowMessage mirrors NEW_CONTEXT_WINDOW_MESSAGE.
	newContextWindowMessage = "A new context window will start without summarizing conversation history."
)

// getContextRemainingExecutor reports the remaining base window tokens.
type getContextRemainingExecutor struct{}

func (getContextRemainingExecutor) Name() protocol.ToolName {
	return protocol.PlainToolName(getContextRemainingToolName)
}

func (getContextRemainingExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	if tc == nil || tc.TokenBudget == nil {
		return tools.ToolSpec{}, false
	}
	return functionSpecStub(getContextRemainingToolName, "Get the remaining tokens in the current context window."), true
}

func (getContextRemainingExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (getContextRemainingExecutor) Handle(_ context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	status := contextWindowTokenStatus(h.Session, h.Turn)
	return NewTextToolOutput(renderTokensLeft(status.BaseWindowTokensRemaining), boolPtr(true)), nil
}

// renderTokensLeft mirrors `TokenBudgetRemainingContext::body`.
func renderTokensLeft(tokensLeft *int64) string {
	if tokensLeft == nil {
		return "You have unknown tokens left in this context window."
	}
	return fmt.Sprintf("You have %d tokens left in this context window.", *tokensLeft)
}

// newContextWindowExecutor requests a fresh context window at the next sampling
// boundary (the turn loop honors it via TakeNewContextWindowRequest).
type newContextWindowExecutor struct{}

func (newContextWindowExecutor) Name() protocol.ToolName {
	return protocol.PlainToolName(newContextWindowToolName)
}

func (newContextWindowExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	if tc == nil || tc.TokenBudget == nil {
		return tools.ToolSpec{}, false
	}
	return functionSpecStub(newContextWindowToolName, "Start a new context window. Does not clear, reset, or otherwise affect environment state."), true
}

func (newContextWindowExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (newContextWindowExecutor) Handle(_ context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	h.Session.RequestNewContextWindow()
	return NewTextToolOutput(newContextWindowMessage, boolPtr(true)), nil
}
