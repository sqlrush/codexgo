package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// PreCompactRequest is the input for a PreCompact dispatch. Mirrors
// PreCompactRequest.
type PreCompactRequest struct {
	SessionID      protocol.ThreadID
	TurnID         string
	Subagent       *SubagentHookContext
	Cwd            abspath.AbsolutePathBuf
	TranscriptPath *string
	Model          string
	Trigger        string
}

// PostCompactRequest is the input for a PostCompact dispatch. Mirrors
// PostCompactRequest.
type PostCompactRequest struct {
	SessionID      protocol.ThreadID
	TurnID         string
	Subagent       *SubagentHookContext
	Cwd            abspath.AbsolutePathBuf
	TranscriptPath *string
	Model          string
	Trigger        string
}

// PreCompactOutcome is the merged result of a PreCompact dispatch. Mirrors
// PreCompactOutcome.
type PreCompactOutcome struct {
	HookEvents []protocol.HookCompletedEvent
	ShouldStop bool
	StopReason *string
}

// StatelessHookOutcome is the merged result of a PostCompact dispatch. Mirrors
// StatelessHookOutcome.
type StatelessHookOutcome struct {
	HookEvents []protocol.HookCompletedEvent
	ShouldStop bool
	StopReason *string
}

type compactHandlerData struct {
	shouldStop bool
	stopReason *string
}

func previewPreCompact(handlers []ConfiguredHandler, request *PreCompactRequest) []protocol.HookRunSummary {
	matched := selectHandlers(handlers, protocol.HookEventNamePreCompact, &request.Trigger)
	out := make([]protocol.HookRunSummary, 0, len(matched))
	for i := range matched {
		out = append(out, runningSummary(matched[i]))
	}
	return out
}

func runPreCompact(ctx context.Context, handlers []ConfiguredHandler, shell CommandShell, request PreCompactRequest) PreCompactOutcome {
	matched := selectHandlers(handlers, protocol.HookEventNamePreCompact, &request.Trigger)
	if len(matched) == 0 {
		return PreCompactOutcome{}
	}

	inputJSON, err := preCompactCommandInputJSON(&request)
	if err != nil {
		turnID := request.TurnID
		events := serializationFailureHookEvents(matched, &turnID, fmt.Sprintf("failed to serialize pre compact hook input: %v", err))
		return PreCompactOutcome{HookEvents: events}
	}

	turnID := request.TurnID
	results := executeHandlers(ctx, shell, matched, inputJSON, request.Cwd.String(), &turnID, parsePreCompactCompleted)
	shouldStop, stopReason := foldCompactResults(results)
	events := make([]protocol.HookCompletedEvent, 0, len(results))
	for i := range results {
		events = append(events, results[i].completed)
	}
	return PreCompactOutcome{HookEvents: events, ShouldStop: shouldStop, StopReason: stopReason}
}

func runPostCompact(ctx context.Context, handlers []ConfiguredHandler, shell CommandShell, request PostCompactRequest) StatelessHookOutcome {
	matched := selectHandlers(handlers, protocol.HookEventNamePostCompact, &request.Trigger)
	if len(matched) == 0 {
		return StatelessHookOutcome{}
	}

	inputJSON, err := postCompactCommandInputJSON(&request)
	if err != nil {
		turnID := request.TurnID
		events := serializationFailureHookEvents(matched, &turnID, fmt.Sprintf("failed to serialize post compact hook input: %v", err))
		return StatelessHookOutcome{HookEvents: events}
	}

	turnID := request.TurnID
	results := executeHandlers(ctx, shell, matched, inputJSON, request.Cwd.String(), &turnID, parsePostCompactCompleted)
	shouldStop, stopReason := foldCompactResults(results)
	events := make([]protocol.HookCompletedEvent, 0, len(results))
	for i := range results {
		events = append(events, results[i].completed)
	}
	return StatelessHookOutcome{HookEvents: events, ShouldStop: shouldStop, StopReason: stopReason}
}

func foldCompactResults(results []parsedHandler[compactHandlerData]) (bool, *string) {
	shouldStop := false
	var stopReason *string
	for i := range results {
		if results[i].data.shouldStop {
			shouldStop = true
		}
		if stopReason == nil && results[i].data.stopReason != nil {
			stopReason = results[i].data.stopReason
		}
	}
	return shouldStop, stopReason
}

func previewPostCompact(handlers []ConfiguredHandler, request *PostCompactRequest) []protocol.HookRunSummary {
	matched := selectHandlers(handlers, protocol.HookEventNamePostCompact, &request.Trigger)
	out := make([]protocol.HookRunSummary, 0, len(matched))
	for i := range matched {
		out = append(out, runningSummary(matched[i]))
	}
	return out
}

func preCompactCommandInputJSON(request *PreCompactRequest) (string, error) {
	subagent := subagentFieldsFrom(request.Subagent)
	in := preCompactCommandInput{
		SessionID:      request.SessionID.String(),
		TurnID:         request.TurnID,
		AgentID:        subagent.agentID,
		AgentType:      subagent.agentType,
		TranscriptPath: nullableFromString(request.TranscriptPath),
		Cwd:            request.Cwd.String(),
		HookEventName:  "PreCompact",
		Model:          request.Model,
		Trigger:        request.Trigger,
	}
	data, err := json.Marshal(in)
	return string(data), err
}

func postCompactCommandInputJSON(request *PostCompactRequest) (string, error) {
	subagent := subagentFieldsFrom(request.Subagent)
	in := postCompactCommandInput{
		SessionID:      request.SessionID.String(),
		TurnID:         request.TurnID,
		AgentID:        subagent.agentID,
		AgentType:      subagent.agentType,
		TranscriptPath: nullableFromString(request.TranscriptPath),
		Cwd:            request.Cwd.String(),
		HookEventName:  "PostCompact",
		Model:          request.Model,
		Trigger:        request.Trigger,
	}
	data, err := json.Marshal(in)
	return string(data), err
}

func parsePreCompactCompleted(handler ConfiguredHandler, run commandRunResult, turnID *string) parsedHandler[compactHandlerData] {
	entries := make([]protocol.HookOutputEntry, 0)
	status := protocol.HookRunStatusCompleted
	shouldStop := false
	var stopReason *string

	switch {
	case run.error != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(*run.error))
	case run.exitCode != nil && *run.exitCode == 0:
		if trimmedNonEmpty(run.stdout) != nil {
			if parsed, ok := parsePreCompact(run.stdout); ok {
				if parsed.universal.SystemMessage != nil {
					entries = append(entries, warningEntry(*parsed.universal.SystemMessage))
				}
				if !parsed.universal.ContinueProcessing {
					status = protocol.HookRunStatusStopped
					shouldStop = true
					stopReason = parsed.universal.StopReason
					text := "PreCompact hook stopped execution"
					if parsed.universal.StopReason != nil {
						text = *parsed.universal.StopReason
					}
					entries = append(entries, stopEntry(text))
				} else if parsed.invalidReason != nil {
					status = protocol.HookRunStatusFailed
					entries = append(entries, errorEntry(*parsed.invalidReason))
				}
			} else if looksLikeJSON(run.stdout) {
				status = protocol.HookRunStatusFailed
				entries = append(entries, errorEntry("hook returned invalid PreCompact hook JSON output"))
			}
		}
	case run.exitCode != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(compactExitText(run, *run.exitCode)))
	default:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry("hook process terminated without an exit code"))
	}

	return parsedHandler[compactHandlerData]{
		completed: protocol.HookCompletedEvent{
			TurnID: turnID,
			Run:    completedSummary(handler, run, status, entries),
		},
		data: compactHandlerData{shouldStop: shouldStop, stopReason: stopReason},
	}
}

func parsePostCompactCompleted(handler ConfiguredHandler, run commandRunResult, turnID *string) parsedHandler[compactHandlerData] {
	return parseStatelessCompactCompleted(handler, run, turnID, "PostCompact", parsePostCompact)
}

func parseStatelessCompactCompleted(handler ConfiguredHandler, run commandRunResult, turnID *string, eventLabel string, parseOutput func(string) (*statelessHookOutput, bool)) parsedHandler[compactHandlerData] {
	entries := make([]protocol.HookOutputEntry, 0)
	status := protocol.HookRunStatusCompleted
	shouldStop := false
	var stopReason *string

	switch {
	case run.error != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(*run.error))
	case run.exitCode != nil && *run.exitCode == 0:
		if trimmedNonEmpty(run.stdout) != nil {
			if parsed, ok := parseOutput(run.stdout); ok {
				if parsed.universal.SystemMessage != nil {
					entries = append(entries, warningEntry(*parsed.universal.SystemMessage))
				}
				if !parsed.universal.ContinueProcessing {
					status = protocol.HookRunStatusStopped
					shouldStop = true
					stopReason = parsed.universal.StopReason
					text := fmt.Sprintf("%s hook stopped execution", eventLabel)
					if parsed.universal.StopReason != nil {
						text = *parsed.universal.StopReason
					}
					entries = append(entries, stopEntry(text))
				} else if parsed.invalidReason != nil {
					status = protocol.HookRunStatusFailed
					entries = append(entries, errorEntry(*parsed.invalidReason))
				}
			} else if looksLikeJSON(run.stdout) {
				status = protocol.HookRunStatusFailed
				entries = append(entries, errorEntry(fmt.Sprintf("hook returned invalid %s hook JSON output", eventLabel)))
			}
		}
	case run.exitCode != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(compactExitText(run, *run.exitCode)))
	default:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry("hook process terminated without an exit code"))
	}

	return parsedHandler[compactHandlerData]{
		completed: protocol.HookCompletedEvent{
			TurnID: turnID,
			Run:    completedSummary(handler, run, status, entries),
		},
		data: compactHandlerData{shouldStop: shouldStop, stopReason: stopReason},
	}
}

func compactExitText(run commandRunResult, code int) string {
	if reason := trimmedNonEmpty(run.stderr); reason != nil {
		return *reason
	}
	return fmt.Sprintf("hook exited with code %d", code)
}
