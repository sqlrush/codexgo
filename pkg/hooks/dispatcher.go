package hooks

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// parsedHandler is one handler's parsed output plus its completion order.
// Mirrors ParsedHandler<T>.
type parsedHandler[T any] struct {
	completed       protocol.HookCompletedEvent
	data            T
	completionOrder int
}

// parseFn converts a finished command run into a parsedHandler. Mirrors the
// `parse` function pointer passed to execute_handlers.
type parseFn[T any] func(handler ConfiguredHandler, result commandRunResult, turnID *string) parsedHandler[T]

// selectHandlers selects handlers for a single matcher input. Mirrors
// select_handlers.
func selectHandlers(handlers []ConfiguredHandler, eventName protocol.HookEventName, matcherInput *string) []ConfiguredHandler {
	var inputs []string
	if matcherInput != nil {
		inputs = []string{*matcherInput}
	}
	return selectHandlersForMatcherInputs(handlers, eventName, inputs)
}

// selectHandlersForMatcherInputs selects handlers that match the event and any
// of the matcher inputs, checking each configured handler at most once. Mirrors
// select_handlers_for_matcher_inputs.
func selectHandlersForMatcherInputs(handlers []ConfiguredHandler, eventName protocol.HookEventName, matcherInputs []string) []ConfiguredHandler {
	out := make([]ConfiguredHandler, 0)
	for _, handler := range handlers {
		if handler.EventName != eventName {
			continue
		}
		if !handlerMatchesEvent(handler, eventName, matcherInputs) {
			continue
		}
		out = append(out, handler)
	}
	return out
}

func handlerMatchesEvent(handler ConfiguredHandler, eventName protocol.HookEventName, matcherInputs []string) bool {
	switch eventName {
	case protocol.HookEventNameUserPromptSubmit, protocol.HookEventNameStop:
		return true
	case protocol.HookEventNamePreToolUse,
		protocol.HookEventNamePermissionRequest,
		protocol.HookEventNamePostToolUse,
		protocol.HookEventNameSessionStart,
		protocol.HookEventNameSubagentStart,
		protocol.HookEventNameSubagentStop,
		protocol.HookEventNamePreCompact,
		protocol.HookEventNamePostCompact:
		if len(matcherInputs) == 0 {
			return MatchesMatcher(handler.Matcher, nil)
		}
		for i := range matcherInputs {
			input := matcherInputs[i]
			if MatchesMatcher(handler.Matcher, &input) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// runningSummary builds the "running" summary for a handler. Mirrors
// running_summary.
func runningSummary(handler ConfiguredHandler) protocol.HookRunSummary {
	return protocol.HookRunSummary{
		ID:            handler.RunID(),
		EventName:     handler.EventName,
		HandlerType:   protocol.HookHandlerTypeCommand,
		ExecutionMode: protocol.HookExecutionModeSync,
		Scope:         scopeForEvent(handler.EventName),
		SourcePath:    protocol.AbsolutePath(handler.SourcePath.String()),
		Source:        handler.Source,
		DisplayOrder:  handler.DisplayOrder,
		Status:        protocol.HookRunStatusRunning,
		StatusMessage: handler.StatusMessage,
		StartedAt:     time.Now().Unix(),
		CompletedAt:   nil,
		DurationMs:    nil,
		Entries:       []protocol.HookOutputEntry{},
	}
}

// completedSummary builds the completed summary for a handler. Mirrors
// completed_summary.
func completedSummary(handler ConfiguredHandler, run commandRunResult, status protocol.HookRunStatus, entries []protocol.HookOutputEntry) protocol.HookRunSummary {
	completedAt := run.completedAt
	durationMs := run.durationMs
	return protocol.HookRunSummary{
		ID:            handler.RunID(),
		EventName:     handler.EventName,
		HandlerType:   protocol.HookHandlerTypeCommand,
		ExecutionMode: protocol.HookExecutionModeSync,
		Scope:         scopeForEvent(handler.EventName),
		SourcePath:    protocol.AbsolutePath(handler.SourcePath.String()),
		Source:        handler.Source,
		DisplayOrder:  handler.DisplayOrder,
		Status:        status,
		StatusMessage: handler.StatusMessage,
		StartedAt:     run.startedAt,
		CompletedAt:   &completedAt,
		DurationMs:    &durationMs,
		Entries:       entries,
	}
}

// scopeForEvent maps an event name to its hook scope. Mirrors scope_for_event.
func scopeForEvent(eventName protocol.HookEventName) protocol.HookScope {
	switch eventName {
	case protocol.HookEventNameSessionStart, protocol.HookEventNameSubagentStart:
		return protocol.HookScopeThread
	default:
		return protocol.HookScopeTurn
	}
}

// executeHandlers runs each matched handler concurrently, writing inputJSON to
// stdin, then parses results. Results are returned in configured order, but each
// parsedHandler carries the completion order in which its subprocess finished.
// Mirrors execute_handlers.
func executeHandlers[T any](ctx context.Context, shell CommandShell, handlers []ConfiguredHandler, inputJSON, cwd string, turnID *string, parse parseFn[T]) []parsedHandler[T] {
	type indexed struct {
		configuredOrder int
		parsed          parsedHandler[T]
	}

	results := make([]indexed, len(handlers))
	var completionCounter int64 = -1
	var wg sync.WaitGroup
	for i := range handlers {
		wg.Add(1)
		go func(configuredOrder int, handler ConfiguredHandler) {
			defer wg.Done()
			result := runCommand(ctx, shell, handler, inputJSON, cwd)
			parsed := parse(handler, result, turnID)
			order := int(atomic.AddInt64(&completionCounter, 1))
			parsed.completionOrder = order
			results[configuredOrder] = indexed{configuredOrder: configuredOrder, parsed: parsed}
		}(i, handlers[i])
	}
	wg.Wait()

	// results is already in configured order by index; sort defensively to match
	// the Rust sort_by_key(configured_order).
	sort.SliceStable(results, func(a, b int) bool {
		return results[a].configuredOrder < results[b].configuredOrder
	})
	out := make([]parsedHandler[T], len(results))
	for i := range results {
		out[i] = results[i].parsed
	}
	return out
}
