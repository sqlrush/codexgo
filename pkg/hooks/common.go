package hooks

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// joinTextChunks joins chunks with a blank line, or returns nil for an empty
// slice. Mirrors join_text_chunks.
func joinTextChunks(chunks []string) *string {
	if len(chunks) == 0 {
		return nil
	}
	joined := strings.Join(chunks, "\n\n")
	return &joined
}

// trimmedNonEmpty returns the trimmed text or nil when empty. Mirrors
// trimmed_non_empty.
func trimmedNonEmpty(text string) *string {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	return &t
}

// appendAdditionalContext records additionalContext as a Context entry and adds
// it to the model-visible slice. Mirrors append_additional_context.
func appendAdditionalContext(entries *[]protocol.HookOutputEntry, contextsForModel *[]string, additionalContext string) {
	*entries = append(*entries, protocol.HookOutputEntry{
		Kind: protocol.HookOutputEntryKindContext,
		Text: additionalContext,
	})
	*contextsForModel = append(*contextsForModel, additionalContext)
}

// flattenAdditionalContexts concatenates per-handler context slices in order.
// Mirrors flatten_additional_contexts.
func flattenAdditionalContexts(chunks [][]string) []string {
	out := make([]string, 0)
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}

// serializationFailureHookEvents builds failed HookCompletedEvents for every
// handler when stdin serialization fails. Mirrors
// serialization_failure_hook_events.
func serializationFailureHookEvents(handlers []ConfiguredHandler, turnID *string, errorMessage string) []protocol.HookCompletedEvent {
	out := make([]protocol.HookCompletedEvent, 0, len(handlers))
	for i := range handlers {
		run := runningSummary(handlers[i])
		run.Status = protocol.HookRunStatusFailed
		started := run.StartedAt
		run.CompletedAt = &started
		zero := int64(0)
		run.DurationMs = &zero
		run.Entries = []protocol.HookOutputEntry{{
			Kind: protocol.HookOutputEntryKindError,
			Text: errorMessage,
		}}
		out = append(out, protocol.HookCompletedEvent{TurnID: turnID, Run: run})
	}
	return out
}

// serializationFailureHookEventsForToolUse mirrors
// serialization_failure_hook_events_for_tool_use.
func serializationFailureHookEventsForToolUse(handlers []ConfiguredHandler, turnID *string, errorMessage, toolUseID string) []protocol.HookCompletedEvent {
	events := serializationFailureHookEvents(handlers, turnID, errorMessage)
	for i := range events {
		events[i] = hookCompletedForToolUse(events[i], toolUseID)
	}
	return events
}

// hookCompletedForToolUse appends the tool-use id to a completed event's run id.
// Mirrors hook_completed_for_tool_use.
func hookCompletedForToolUse(event protocol.HookCompletedEvent, toolUseID string) protocol.HookCompletedEvent {
	event.Run = hookRunForToolUse(event.Run, toolUseID)
	return event
}

// hookRunForToolUse appends the tool-use id to a run summary's id. Mirrors
// hook_run_for_tool_use.
func hookRunForToolUse(run protocol.HookRunSummary, toolUseID string) protocol.HookRunSummary {
	run.ID = fmt.Sprintf("%s:%s", run.ID, toolUseID)
	return run
}
