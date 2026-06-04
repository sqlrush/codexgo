package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// dispatchToolInvocation routes a tool invocation through the session's
// [ToolRouter], preferring the session-aware path ([sessionAwareRouter]) so
// built-in executors can emit their visible lifecycle events (exec_command,
// file_change). It falls back to the session-less [ToolRouter.Dispatch] for
// routers that do not implement the session-aware interface.
func dispatchToolInvocation(ctx context.Context, sess *Session, tc *TurnContext, inv ToolInvocation, payload tools.ToolPayload) (ToolResult, error) {
	if aware, ok := sess.services.ToolRouter.(sessionAwareRouter); ok {
		return aware.DispatchWithSession(ctx, sess, tc, inv, payload)
	}
	return sess.services.ToolRouter.Dispatch(ctx, tc, inv)
}

// handleOutputItemDone processes a completed output item from the model stream.
// Assistant messages and reasoning are recorded into history and surfaced via
// ItemCompleted/AgentMessage/AgentReasoning events; tool calls are dispatched
// through the [ToolRouter] and their outputs are accumulated for the next model
// request. Mirrors the Rust `handle_output_item_done` (reduced surface).
func handleOutputItemDone(ctx context.Context, sess *Session, tc *TurnContext, item protocol.ResponseItem, st *streamState) error {
	switch item.Type {
	case protocol.ResponseItemKindMessage:
		if item.Role == "assistant" {
			sess.RecordItems([]protocol.ResponseItem{item})
			text := assistantMessageText(item)
			emitAgentMessage(sess, tc, text)
			emitItemCompletedAgentMessage(sess, tc, item, text)
			if text != "" {
				t := text
				*st.lastAgentMessage = &t
			}
		} else {
			// Non-assistant message items are recorded verbatim.
			sess.RecordItems([]protocol.ResponseItem{item})
		}
		return nil

	case protocol.ResponseItemKindReasoning:
		sess.RecordItems([]protocol.ResponseItem{item})
		emitReasoning(sess, tc, item)
		return nil

	case protocol.ResponseItemKindFunctionCall:
		return dispatchFunctionCall(ctx, sess, tc, item, st)

	case protocol.ResponseItemKindCustomToolCall:
		return dispatchCustomToolCall(ctx, sess, tc, item, st)

	default:
		// LocalShellCall, WebSearchCall, ImageGenerationCall, ToolSearchCall and
		// other tool variants are recorded verbatim; their dedicated handlers are
		// STUBbed (owned by the tools area agent).
		sess.RecordItems([]protocol.ResponseItem{item})
		return nil
	}
}

// dispatchFunctionCall runs a function-call tool invocation through the
// [ToolRouter], records the resulting function_call_output item, and marks the
// turn as needing a follow-up request so the model observes the output. Mirrors
// the Rust function-call dispatch path (sequential subset).
//
// STUB: parallel tool execution (FuturesOrdered), MCP fan-out, approval
// orchestration, and the turn-diff tracker are deferred. Tool calls run
// sequentially here, preserving submission/output ordering.
func dispatchFunctionCall(ctx context.Context, sess *Session, tc *TurnContext, item protocol.ResponseItem, st *streamState) error {
	// Record the call item itself so history reflects what the model asked for.
	sess.RecordItems([]protocol.ResponseItem{item})

	inv := ToolInvocation{
		CallID:    item.CallID,
		Name:      toolNameFor(item),
		Arguments: []byte(item.Arguments),
	}
	if at := sess.ActiveTurn(); at != nil {
		at.State.IncToolCalls()
	}

	result, err := dispatchToolInvocation(ctx, sess, tc, inv, tools.FunctionPayload(item.Arguments))
	if err != nil {
		// A dispatch error becomes a failed tool output so the model can recover,
		// matching the Rust behavior of surfacing tool errors back to the model.
		result = ToolResult{Output: fmt.Sprintf("tool dispatch error: %v", err), Success: false}
	}

	output := functionCallOutputItem(item.CallID, result)
	*st.toolOutputs = append(*st.toolOutputs, output)
	*st.needsFollowUp = true
	return nil
}

// dispatchCustomToolCall runs a custom-tool-call invocation and records the
// custom_tool_call_output item. Mirrors the Rust custom-tool dispatch path.
func dispatchCustomToolCall(ctx context.Context, sess *Session, tc *TurnContext, item protocol.ResponseItem, st *streamState) error {
	sess.RecordItems([]protocol.ResponseItem{item})

	inv := ToolInvocation{
		CallID:    item.CallID,
		Name:      protocol.PlainToolName(item.Name),
		Arguments: []byte(item.Input),
	}
	if at := sess.ActiveTurn(); at != nil {
		at.State.IncToolCalls()
	}

	result, err := dispatchToolInvocation(ctx, sess, tc, inv, tools.CustomPayload(item.Input))
	if err != nil {
		result = ToolResult{Output: fmt.Sprintf("tool dispatch error: %v", err), Success: false}
	}

	output := customToolCallOutputItem(item.CallID, result)
	*st.toolOutputs = append(*st.toolOutputs, output)
	*st.needsFollowUp = true
	return nil
}

// toolNameFor derives a [protocol.ToolName] from a function-call item, honoring
// the optional namespace.
func toolNameFor(item protocol.ResponseItem) protocol.ToolName {
	if item.Namespace != nil {
		return protocol.NamespacedToolName(*item.Namespace, item.Name)
	}
	return protocol.PlainToolName(item.Name)
}

// functionCallOutputItem builds a function_call_output response item from a tool
// result.
func functionCallOutputItem(callID string, result ToolResult) protocol.ResponseItem {
	text := result.Output
	success := result.Success
	return protocol.ResponseItem{
		Type:   protocol.ResponseItemKindFunctionCallOutput,
		CallID: callID,
		Output: &protocol.FunctionCallOutputPayload{
			Text:    &text,
			Success: &success,
		},
	}
}

// customToolCallOutputItem builds a custom_tool_call_output response item from a
// tool result.
func customToolCallOutputItem(callID string, result ToolResult) protocol.ResponseItem {
	text := result.Output
	success := result.Success
	return protocol.ResponseItem{
		Type:   protocol.ResponseItemKindCustomToolCallOutput,
		CallID: callID,
		Output: &protocol.FunctionCallOutputPayload{
			Text:    &text,
			Success: &success,
		},
	}
}

// assistantMessageText concatenates the OutputText content of an assistant
// message response item.
func assistantMessageText(item protocol.ResponseItem) string {
	var b strings.Builder
	for _, c := range item.Content {
		if c.Type == protocol.ContentItemKindOutputText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// reasoningSummaryText concatenates the summary text of a reasoning item.
func reasoningSummaryText(item protocol.ResponseItem) string {
	var b strings.Builder
	for _, s := range item.Summary {
		b.WriteString(s.Text)
	}
	return b.String()
}

// reasoningRawText concatenates the raw reasoning-text content of a reasoning
// item, if present.
func reasoningRawText(item protocol.ResponseItem) string {
	if item.ReasoningContent == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range *item.ReasoningContent {
		if c.Type == protocol.ReasoningItemContentKindReasoningText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
