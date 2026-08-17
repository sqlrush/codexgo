package core

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/sqlrush/codexgo/internal/utils/truncation"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// This file holds the pure history-rebuild helpers and the small Session/event
// glue used by event_compact.go. They are split out to keep event_compact.go
// focused on the compaction control flow.

// newContextCompactionTurnItem builds a fresh ContextCompaction turn item with a
// generated id, mirroring the Rust `ContextCompactionItem::new()`.
func newContextCompactionTurnItem() protocol.TurnItem {
	return protocol.TurnItem{
		Type:              protocol.TurnItemKindContextCompaction,
		ContextCompaction: &protocol.ContextCompactionItem{ID: uuid.NewString()},
	}
}

// EmitTurnItemStarted emits an ItemStarted event for an already-built turn item.
// Unlike the streamed-item helper in turn_events.go, this emits whatever turn
// item it is given (used for the synthetic compaction item). Mirrors the Rust
// `Session::emit_turn_item_started`.
func EmitTurnItemStarted(sess *Session, tc *TurnContext, item protocol.TurnItem) {
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindItemStarted,
		ItemStarted: &protocol.ItemStartedEvent{
			ThreadID:  sess.threadID,
			TurnID:    tc.SubID,
			Item:      item,
			StartedAt: nowMillis(),
		},
	})
}

// EmitTurnItemCompleted emits an ItemCompleted event for an already-built turn
// item. Mirrors the Rust `Session::emit_turn_item_completed`.
func EmitTurnItemCompleted(sess *Session, tc *TurnContext, item protocol.TurnItem) {
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindItemCompleted,
		ItemCompleted: &protocol.ItemCompletedEvent{
			ThreadID:    sess.threadID,
			TurnID:      tc.SubID,
			Item:        item,
			CompletedAt: nowMillis(),
		},
	})
}

// getLastAssistantMessageFromTurn returns the text of the most recent assistant
// message in items, scanning from the end. It is the Go analogue of the Rust
// `get_last_assistant_message_from_turn`.
//
// STUB: the Rust implementation strips hidden assistant markup (citations, plan
// blocks) via strip_hidden_assistant_markup before returning. That stripping is
// owned by the stream-events area; here we return the raw assistant output text.
func getLastAssistantMessageFromTurn(items []protocol.ResponseItem) string {
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.Type != protocol.ResponseItemKindMessage || item.Role != "assistant" {
			continue
		}
		text := assistantMessageText(item)
		if strings.TrimSpace(text) == "" {
			continue
		}
		return text
	}
	return ""
}

// collectUserMessages projects the history into the ordered list of real user
// message texts (excluding compaction summaries). It is the Go analogue of the
// Rust `collect_user_messages`, reusing parseTurnItem from event_mapping.go so
// the same user-message recognition (contextual/hook filtering) applies.
func collectUserMessages(items []protocol.ResponseItem) []string {
	var out []string
	for _, item := range items {
		ti, ok := parseTurnItem(item)
		if !ok || ti.Type != protocol.TurnItemKindUserMessage || ti.UserMessage == nil {
			continue
		}
		msg := userMessageText(*ti.UserMessage)
		if isSummaryMessage(msg) {
			continue
		}
		out = append(out, msg)
	}
	return out
}

// isSummaryMessage reports whether a user-message text is a compaction summary
// (it begins with the summary prefix followed by a newline). Mirrors the Rust
// `is_summary_message`.
func isSummaryMessage(message string) bool {
	return strings.HasPrefix(message, SummaryPrefix+"\n")
}

// buildCompactedHistory rebuilds a compact replacement history from an optional
// initial context, the recent user messages (token-budgeted, oldest-first in the
// output), and the summary text. It is the Go analogue of the Rust
// `build_compacted_history`.
func buildCompactedHistory(initialContext []protocol.ResponseItem, userMessages []string, summaryText string) []protocol.ResponseItem {
	return buildCompactedHistoryWithLimit(initialContext, userMessages, summaryText, compactUserMessageMaxTokens)
}

// buildCompactedHistoryWithLimit is buildCompactedHistory with an explicit token
// budget, exposed for testing. It is the Go analogue of the Rust
// `build_compacted_history_with_limit`.
//
// Selection walks the user messages newest-first, keeping whole messages while
// the remaining budget allows and truncating the first message that overflows;
// the selection is then reversed back to chronological order before being
// appended, followed by the summary as a final user message.
func buildCompactedHistoryWithLimit(history []protocol.ResponseItem, userMessages []string, summaryText string, maxTokens int) []protocol.ResponseItem {
	out := append([]protocol.ResponseItem(nil), history...)

	var selected []string
	if maxTokens > 0 {
		remaining := maxTokens
		for i := len(userMessages) - 1; i >= 0; i-- {
			if remaining == 0 {
				break
			}
			message := userMessages[i]
			tokens := truncation.ApproxTokenCount(message)
			if tokens <= remaining {
				selected = append(selected, message)
				remaining = saturatingSub(remaining, tokens)
			} else {
				truncated := truncation.TruncateText(message, truncation.TokensPolicy(remaining))
				selected = append(selected, truncated)
				break
			}
		}
		reverseStrings(selected)
	}

	for _, message := range selected {
		out = append(out, userTextMessage(message))
	}

	summary := summaryText
	if summary == "" {
		summary = noSummaryAvailablePlaceholder
	}
	out = append(out, userTextMessage(summary))

	return out
}

// insertInitialContextBeforeLastRealUserOrSummary splices initialContext into the
// compacted history at the model-expected boundary. It is the Go analogue of the
// Rust `insert_initial_context_before_last_real_user_or_summary`.
//
// Placement rules (in order of preference):
//   - immediately before the last real (non-summary) user message;
//   - else before the last summary user message (so the summary stays last);
//   - else before the last compaction item (remote compaction may return only
//     compaction items);
//   - else append.
func insertInitialContextBeforeLastRealUserOrSummary(compacted []protocol.ResponseItem, initialContext []protocol.ResponseItem) []protocol.ResponseItem {
	lastUserOrSummary := -1
	lastRealUser := -1
	for i := len(compacted) - 1; i >= 0; i-- {
		ti, ok := parseTurnItem(compacted[i])
		if !ok || ti.Type != protocol.TurnItemKindUserMessage || ti.UserMessage == nil {
			continue
		}
		if lastUserOrSummary == -1 {
			lastUserOrSummary = i
		}
		if !isSummaryMessage(userMessageText(*ti.UserMessage)) {
			lastRealUser = i
			break
		}
	}

	lastCompaction := -1
	for i := len(compacted) - 1; i >= 0; i-- {
		switch compacted[i].Type {
		case protocol.ResponseItemKindCompaction, protocol.ResponseItemKindContextCompaction:
			lastCompaction = i
		}
		if lastCompaction != -1 {
			break
		}
	}

	insertIdx := -1
	switch {
	case lastRealUser != -1:
		insertIdx = lastRealUser
	case lastUserOrSummary != -1:
		insertIdx = lastUserOrSummary
	case lastCompaction != -1:
		insertIdx = lastCompaction
	}

	if insertIdx == -1 {
		return append(append([]protocol.ResponseItem(nil), compacted...), initialContext...)
	}

	out := make([]protocol.ResponseItem, 0, len(compacted)+len(initialContext))
	out = append(out, compacted[:insertIdx]...)
	out = append(out, initialContext...)
	out = append(out, compacted[insertIdx:]...)
	return out
}

// userTextMessage builds a user-role message response item carrying a single
// input-text content item.
func userTextMessage(text string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type: protocol.ResponseItemKindMessage,
		Role: "user",
		Content: []protocol.ContentItem{{
			Type: protocol.ContentItemKindInputText,
			Text: text,
		}},
	}
}

// reverseStrings reverses s in place.
func reverseStrings(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// saturatingSub returns a-b clamped at 0.
func saturatingSub(a, b int) int {
	if b >= a {
		return 0
	}
	return a - b
}

// ----------------------------------------------------------------------------
// Session compaction glue
// ----------------------------------------------------------------------------

// replaceCompactedHistory swaps shared history for the compacted one, clears the
// auto-compaction prefill baseline, persists a `compacted` rollout line, and
// advances the compaction window. It is the Go analogue of the Rust
// `Session::replace_compacted_history`.
//
// STUB: the reference-context-item baseline is accepted but not stored (the
// reference-context tracking lives in the context-manager area); it is only used
// to drive whether a turn-context rollout line is persisted alongside the
// compacted line.
func (s *Session) replaceCompactedHistory(
	ctx context.Context,
	newHistory []protocol.ResponseItem,
	referenceContextItem *rollout.TurnContextItem,
	compacted rollout.CompactedItem,
) {
	s.WithState(func(st *SessionState) {
		st.ReplaceHistory(newHistory)
		st.StartNextAutoCompactWindow()
	})
	s.recordRollout(ctx, rollout.NewCompactedItem(compacted))
	if referenceContextItem != nil {
		s.recordRollout(ctx, rollout.NewTurnContextItem(*referenceContextItem))
	}
}

// recomputeTokenUsage recomputes the session token-usage baseline after a history
// replacement so the next token-count event reflects the compacted history. It is
// the Go analogue of the Rust `Session::recompute_token_usage`.
//
// STUB: the Rust implementation re-derives input tokens from the new history and
// updates the auto-compaction window's estimated prefill. Here we estimate the
// prefill from the compacted history's approximate token count and feed it to the
// window; the full per-category token recomputation is owned by the
// context-manager area.
func (s *Session) recomputeTokenUsage(tc *TurnContext) {
	items := s.HistoryItems()
	estimated := approxHistoryTokens(items)
	s.WithState(func(st *SessionState) {
		st.SetAutoCompactWindowEstimatedPrefill(estimated)
	})
}

// buildInitialContext returns the session's canonical initial-context items to be
// re-injected into a mid-turn compacted history. It is the Go analogue of the
// Rust `Session::build_initial_context`.
//
// STUB: the real initial context bundles user instructions, environment context,
// skills, and personality fragments assembled by the context/skills area agents.
// The turn-running subset reconstructs the developer/user-instruction text
// fragments available on the turn context; richer fragments are deferred.
func (s *Session) buildInitialContext(tc *TurnContext) []protocol.ResponseItem {
	var out []protocol.ResponseItem
	if tc.DeveloperInstructions != nil && *tc.DeveloperInstructions != "" {
		out = append(out, developerTextMessage(*tc.DeveloperInstructions))
	}
	if tc.UserInstructions != nil && *tc.UserInstructions != "" {
		out = append(out, userTextMessage(*tc.UserInstructions))
	}
	return out
}

// recordRollout persists rollout items best-effort. A nil recorder or a recorder
// error is swallowed: compaction must not fail because persistence is
// unavailable (mirroring the Rust recorder, which logs and continues).
func (s *Session) recordRollout(ctx context.Context, items ...rollout.RolloutItem) {
	if s.services.RolloutRecorder == nil || len(items) == 0 {
		return
	}
	_ = s.services.RolloutRecorder.Record(ctx, items)
}

// developerTextMessage builds a developer-role message response item carrying a
// single input-text content item.
func developerTextMessage(text string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type: protocol.ResponseItemKindMessage,
		Role: "developer",
		Content: []protocol.ContentItem{{
			Type: protocol.ContentItemKindInputText,
			Text: text,
		}},
	}
}

// approxHistoryTokens estimates the total token count of a history by summing the
// approximate token count of each item's text content. It is a coarse stand-in
// for the Rust per-category recomputation (see recomputeTokenUsage STUB).
func approxHistoryTokens(items []protocol.ResponseItem) int64 {
	var total int64
	for _, item := range items {
		for _, c := range item.Content {
			if c.Type == protocol.ContentItemKindInputText || c.Type == protocol.ContentItemKindOutputText {
				total += int64(truncation.ApproxTokenCount(c.Text))
			}
		}
	}
	return total
}
