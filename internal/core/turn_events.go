package core

import (
	"errors"
	"time"

	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// buildPrompt constructs the per-request [Prompt] from the current conversation
// history and the turn context's resolved tools/instructions. A fresh Prompt is
// built for every request (immutability). Mirrors the Rust `build_prompt`
// (reduced surface: parallel-tool/personality/output-schema-strict fields are
// owned by the model client).
func buildPrompt(sess *Session, tc *TurnContext) Prompt {
	prompt := Prompt{
		Input:        sess.HistoryItems(),
		Tools:        tc.Tools,
		OutputSchema: tc.FinalOutputJSONSchema,
	}
	if tc.BaseInstructions != "" {
		bi := tc.BaseInstructions
		prompt.BaseInstructionsOverride = &bi
	}
	return prompt
}

// nowMillis returns the current unix time in milliseconds, used to stamp
// item-lifecycle events.
func nowMillis() int64 { return time.Now().UnixMilli() }

// nowSeconds returns the current unix time in seconds, used to stamp turn
// lifecycle events.
func nowSeconds() int64 { return time.Now().Unix() }

// emitItemStarted emits an ItemStarted event for a streaming response item, when
// it maps to a client-visible turn item. Items that do not map to a turn item
// (tool calls, outputs) are skipped here; their begin/end events are owned by the
// tools area agent. Mirrors the Rust `emit_turn_item_started` for the
// non-tool streamed-item subset.
func emitItemStarted(sess *Session, tc *TurnContext, item *protocol.ResponseItem) {
	ti, ok := turnItemForStreamed(item)
	if !ok {
		return
	}
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindItemStarted,
		ItemStarted: &protocol.ItemStartedEvent{
			ThreadID:  sess.threadID,
			TurnID:    tc.SubID,
			Item:      ti,
			StartedAt: nowMillis(),
		},
	})
}

// emitItemCompletedAgentMessage emits an ItemCompleted event for a finished
// assistant message item.
func emitItemCompletedAgentMessage(sess *Session, tc *TurnContext, item protocol.ResponseItem, text string) {
	id := ""
	if item.MessageID != nil {
		id = *item.MessageID
	}
	ti := protocol.TurnItem{
		Type: protocol.TurnItemKindAgentMessage,
		AgentMessage: &protocol.AgentMessageItem{
			ID:      id,
			Content: []protocol.AgentMessageContent{protocol.NewAgentMessageText(text)},
			Phase:   item.Phase,
		},
	}
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindItemCompleted,
		ItemCompleted: &protocol.ItemCompletedEvent{
			ThreadID:    sess.threadID,
			TurnID:      tc.SubID,
			Item:        ti,
			CompletedAt: nowMillis(),
		},
	})
}

// turnItemForStreamed maps a streamed response item to its client-visible turn
// item for ItemStarted events. It returns (item, false) for items that have no
// streamed turn-item representation. Mirrors the Rust `handle_non_tool_response_item`
// (message + reasoning subset).
func turnItemForStreamed(item *protocol.ResponseItem) (protocol.TurnItem, bool) {
	switch item.Type {
	case protocol.ResponseItemKindMessage:
		if item.Role != "assistant" {
			return protocol.TurnItem{}, false
		}
		id := ""
		if item.MessageID != nil {
			id = *item.MessageID
		}
		return protocol.TurnItem{
			Type: protocol.TurnItemKindAgentMessage,
			AgentMessage: &protocol.AgentMessageItem{
				ID:      id,
				Content: []protocol.AgentMessageContent{},
				Phase:   item.Phase,
			},
		}, true
	case protocol.ResponseItemKindReasoning:
		return protocol.TurnItem{
			Type: protocol.TurnItemKindReasoning,
			Reasoning: &protocol.ReasoningTurnItem{
				ID: item.ReasoningID,
			},
		}, true
	default:
		return protocol.TurnItem{}, false
	}
}

// emitAgentMessageDelta emits an incremental agent-message content delta during
// streaming. Empty deltas and deltas without an active assistant item are
// ignored. Mirrors the Rust OutputTextDelta arm.
func emitAgentMessageDelta(sess *Session, tc *TurnContext, active *protocol.ResponseItem, delta string) {
	if delta == "" || active == nil || active.Type != protocol.ResponseItemKindMessage {
		return
	}
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindAgentMessageContentDelta,
		AgentMessageContentDelta: &protocol.AgentMessageContentDeltaEvent{
			ThreadID: sess.threadID.String(),
			TurnID:   tc.SubID,
			ItemID:   streamedItemID(active),
			Delta:    delta,
		},
	})
}

// emitReasoningSummaryDelta emits an incremental reasoning-summary delta.
func emitReasoningSummaryDelta(sess *Session, tc *TurnContext, active *protocol.ResponseItem, delta string, summaryIndex int64) {
	if delta == "" || active == nil {
		return
	}
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindReasoningContentDelta,
		ReasoningContentDelta: &protocol.ReasoningContentDeltaEvent{
			ThreadID:     sess.threadID.String(),
			TurnID:       tc.SubID,
			ItemID:       streamedItemID(active),
			Delta:        delta,
			SummaryIndex: summaryIndex,
		},
	})
}

// emitReasoningRawDelta emits an incremental raw-reasoning (chain-of-thought)
// delta.
func emitReasoningRawDelta(sess *Session, tc *TurnContext, active *protocol.ResponseItem, delta string, contentIndex int64) {
	if delta == "" || active == nil {
		return
	}
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindReasoningRawContentDelta,
		ReasoningRawContentDelta: &protocol.ReasoningRawContentDeltaEvent{
			ThreadID:     sess.threadID.String(),
			TurnID:       tc.SubID,
			ItemID:       streamedItemID(active),
			Delta:        delta,
			ContentIndex: contentIndex,
		},
	})
}

// emitReasoningSectionBreak emits a reasoning section-break event marking a new
// summary section.
func emitReasoningSectionBreak(sess *Session, tc *TurnContext, active *protocol.ResponseItem, summaryIndex int64) {
	if active == nil {
		return
	}
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindAgentReasoningSectionBreak,
		AgentReasoningSectionBreak: &protocol.AgentReasoningSectionBreakEvent{
			ItemID:       streamedItemID(active),
			SummaryIndex: summaryIndex,
		},
	})
}

// emitAgentMessage emits the terminal AgentMessage event for a completed
// assistant message. Empty messages are skipped.
func emitAgentMessage(sess *Session, tc *TurnContext, text string) {
	if text == "" {
		return
	}
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindAgentMessage,
		AgentMessage: &protocol.AgentMessageEvent{
			Message: text,
		},
	})
}

// emitReasoning emits AgentReasoning (summary) and, when present,
// AgentReasoningRawContent (raw chain-of-thought) events for a completed
// reasoning item. Mirrors the Rust reasoning finalization.
func emitReasoning(sess *Session, tc *TurnContext, item protocol.ResponseItem) {
	if summary := reasoningSummaryText(item); summary != "" {
		sess.SendEvent(tc.SubID, protocol.EventMsg{
			Type:           protocol.EventMsgKindAgentReasoning,
			AgentReasoning: &protocol.AgentReasoningEvent{Text: summary},
		})
	}
	if raw := reasoningRawText(item); raw != "" {
		sess.SendEvent(tc.SubID, protocol.EventMsg{
			Type:                     protocol.EventMsgKindAgentReasoningRawContent,
			AgentReasoningRawContent: &protocol.AgentReasoningRawContentEvent{Text: raw},
		})
	}
}

// streamedItemID returns the stable id to use for delta events keyed by the
// active streamed item.
func streamedItemID(item *protocol.ResponseItem) string {
	switch item.Type {
	case protocol.ResponseItemKindMessage:
		if item.MessageID != nil {
			return *item.MessageID
		}
	case protocol.ResponseItemKindReasoning:
		return item.ReasoningID
	}
	return ""
}

// emitTurnError surfaces a turn-level error to the client via an Error event.
// Mirrors the Rust `emit_turn_error_lifecycle` + Error event (reduced: no goal
// runtime or error-classification mapping).
func emitTurnError(sess *Session, tc *TurnContext, err error) {
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindError,
		Error: &protocol.ErrorEvent{
			Message: modelFacingErrorMessage(err),
		},
	})
}

// modelFacingErrorMessage renders a turn error the way codex surfaces it to the
// client: an HTTP transport failure reports the UPSTREAM RESPONSE BODY verbatim
// (the Rust UnexpectedStatus display), not the internal Go wrapping chain.
// Other errors keep their full message.
func modelFacingErrorMessage(err error) string {
	var transport *client.TransportError
	if errors.As(err, &transport) && transport.Kind == client.TransportErrorHTTP && transport.Body != "" {
		return transport.Body
	}
	return err.Error()
}

// accountAndEmitTokenCount folds the completed request's token usage into the
// running session total (when present) and emits a TokenCount event carrying the
// latest token-usage info and rate-limit snapshot. Mirrors the Rust
// `record_token_usage_info` + `send_token_count_event`.
func accountAndEmitTokenCount(sess *Session, tc *TurnContext, usage *protocol.TokenUsage) {
	var (
		info       *protocol.TokenUsageInfo
		rateLimits *protocol.RateLimitSnapshot
	)
	sess.WithState(func(st *SessionState) {
		if usage != nil {
			st.UpdateTokenInfoFromUsage(*usage, tc.ModelContextWindow)
		}
		info, rateLimits = st.TokenInfoAndRateLimits()
	})
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindTokenCount,
		TokenCount: &protocol.TokenCountEvent{
			Info:       info,
			RateLimits: rateLimits,
		},
	})
}
