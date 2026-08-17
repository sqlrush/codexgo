package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// UserPromptSubmitRequest is the input for a UserPromptSubmit dispatch. Mirrors
// UserPromptSubmitRequest.
type UserPromptSubmitRequest struct {
	SessionID      protocol.ThreadID
	TurnID         string
	Subagent       *SubagentHookContext
	Cwd            abspath.AbsolutePathBuf
	TranscriptPath *string
	Model          string
	PermissionMode string
	Prompt         string
}

// UserPromptSubmitOutcome is the merged result of a UserPromptSubmit dispatch.
// Mirrors UserPromptSubmitOutcome.
type UserPromptSubmitOutcome struct {
	HookEvents         []protocol.HookCompletedEvent
	ShouldStop         bool
	StopReason         *string
	AdditionalContexts []string
}

type userPromptSubmitHandlerData struct {
	shouldStop              bool
	stopReason              *string
	additionalContextsModel []string
}

func previewUserPromptSubmit(handlers []ConfiguredHandler, _ *UserPromptSubmitRequest) []protocol.HookRunSummary {
	matched := selectHandlers(handlers, protocol.HookEventNameUserPromptSubmit, nil)
	out := make([]protocol.HookRunSummary, 0, len(matched))
	for i := range matched {
		out = append(out, runningSummary(matched[i]))
	}
	return out
}

func runUserPromptSubmit(ctx context.Context, handlers []ConfiguredHandler, shell CommandShell, request UserPromptSubmitRequest) UserPromptSubmitOutcome {
	matched := selectHandlers(handlers, protocol.HookEventNameUserPromptSubmit, nil)
	if len(matched) == 0 {
		return UserPromptSubmitOutcome{}
	}

	subagent := subagentFieldsFrom(request.Subagent)
	in := userPromptSubmitCommandInput{
		SessionID:      request.SessionID.String(),
		TurnID:         request.TurnID,
		AgentID:        subagent.agentID,
		AgentType:      subagent.agentType,
		TranscriptPath: nullableFromString(request.TranscriptPath),
		Cwd:            request.Cwd.String(),
		HookEventName:  "UserPromptSubmit",
		Model:          request.Model,
		PermissionMode: request.PermissionMode,
		Prompt:         request.Prompt,
	}
	data, err := json.Marshal(in)
	if err != nil {
		turnID := request.TurnID
		events := serializationFailureHookEvents(matched, &turnID, fmt.Sprintf("failed to serialize user prompt submit hook input: %v", err))
		return UserPromptSubmitOutcome{HookEvents: events}
	}

	turnID := request.TurnID
	results := executeHandlers(ctx, shell, matched, string(data), request.Cwd.String(), &turnID, parseUserPromptSubmitCompleted)

	shouldStop := false
	var stopReason *string
	contexts := make([][]string, 0, len(results))
	for i := range results {
		if results[i].data.shouldStop {
			shouldStop = true
		}
		if stopReason == nil && results[i].data.stopReason != nil {
			stopReason = results[i].data.stopReason
		}
		contexts = append(contexts, results[i].data.additionalContextsModel)
	}

	events := make([]protocol.HookCompletedEvent, 0, len(results))
	for i := range results {
		events = append(events, results[i].completed)
	}

	return UserPromptSubmitOutcome{
		HookEvents:         events,
		ShouldStop:         shouldStop,
		StopReason:         stopReason,
		AdditionalContexts: flattenAdditionalContexts(contexts),
	}
}

func parseUserPromptSubmitCompleted(handler ConfiguredHandler, run commandRunResult, turnID *string) parsedHandler[userPromptSubmitHandlerData] {
	entries := make([]protocol.HookOutputEntry, 0)
	status := protocol.HookRunStatusCompleted
	shouldStop := false
	var stopReason *string
	contextsForModel := make([]string, 0)

	switch {
	case run.error != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(*run.error))
	case run.exitCode != nil && *run.exitCode == 0:
		trimmed := trimmedNonEmpty(run.stdout)
		if trimmed != nil {
			if parsed, ok := parseUserPromptSubmit(run.stdout); ok {
				if parsed.universal.SystemMessage != nil {
					entries = append(entries, warningEntry(*parsed.universal.SystemMessage))
				}
				if parsed.invalidBlockReason == nil && parsed.additionalContext != nil {
					appendAdditionalContext(&entries, &contextsForModel, *parsed.additionalContext)
				}
				switch {
				case !parsed.universal.ContinueProcessing:
					status = protocol.HookRunStatusStopped
					shouldStop = true
					stopReason = parsed.universal.StopReason
					if parsed.universal.StopReason != nil {
						entries = append(entries, stopEntry(*parsed.universal.StopReason))
					}
				case parsed.invalidBlockReason != nil:
					status = protocol.HookRunStatusFailed
					entries = append(entries, errorEntry(*parsed.invalidBlockReason))
				case parsed.shouldBlock:
					status = protocol.HookRunStatusBlocked
					shouldStop = true
					stopReason = parsed.reason
					if parsed.reason != nil {
						entries = append(entries, feedbackEntry(*parsed.reason))
					}
				}
			} else if looksLikeJSON(run.stdout) {
				status = protocol.HookRunStatusFailed
				entries = append(entries, errorEntry("hook returned invalid user prompt submit JSON output"))
			} else {
				appendAdditionalContext(&entries, &contextsForModel, *trimmed)
			}
		}
	case run.exitCode != nil && *run.exitCode == 2:
		if reason := trimmedNonEmpty(run.stderr); reason != nil {
			status = protocol.HookRunStatusBlocked
			shouldStop = true
			stopReason = reason
			entries = append(entries, feedbackEntry(*reason))
		} else {
			status = protocol.HookRunStatusFailed
			entries = append(entries, errorEntry("UserPromptSubmit hook exited with code 2 but did not write a blocking reason to stderr"))
		}
	case run.exitCode != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(fmt.Sprintf("hook exited with code %d", *run.exitCode)))
	default:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry("hook exited without a status code"))
	}

	completed := protocol.HookCompletedEvent{
		TurnID: turnID,
		Run:    completedSummary(handler, run, status, entries),
	}
	return parsedHandler[userPromptSubmitHandlerData]{
		completed: completed,
		data: userPromptSubmitHandlerData{
			shouldStop:              shouldStop,
			stopReason:              stopReason,
			additionalContextsModel: contextsForModel,
		},
	}
}
