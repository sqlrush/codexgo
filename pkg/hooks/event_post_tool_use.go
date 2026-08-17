package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// PostToolUseRequest is the input for a PostToolUse dispatch. Mirrors
// PostToolUseRequest.
type PostToolUseRequest struct {
	SessionID      protocol.ThreadID
	TurnID         string
	Subagent       *SubagentHookContext
	Cwd            abspath.AbsolutePathBuf
	TranscriptPath *string
	Model          string
	PermissionMode string
	ToolName       string
	MatcherAliases []string
	ToolUseID      string
	ToolInput      json.RawMessage
	ToolResponse   json.RawMessage
}

// PostToolUseOutcome is the merged result of a PostToolUse dispatch. Mirrors
// PostToolUseOutcome.
type PostToolUseOutcome struct {
	HookEvents         []protocol.HookCompletedEvent
	ShouldStop         bool
	StopReason         *string
	AdditionalContexts []string
	FeedbackMessage    *string
}

type postToolUseHandlerData struct {
	shouldStop              bool
	stopReason              *string
	additionalContextsModel []string
	feedbackMessagesModel   []string
}

func previewPostToolUse(handlers []ConfiguredHandler, request *PostToolUseRequest) []protocol.HookRunSummary {
	matcherInputs := MatcherInputs(request.ToolName, request.MatcherAliases)
	matched := selectHandlersForMatcherInputs(handlers, protocol.HookEventNamePostToolUse, matcherInputs)
	out := make([]protocol.HookRunSummary, 0, len(matched))
	for i := range matched {
		out = append(out, hookRunForToolUse(runningSummary(matched[i]), request.ToolUseID))
	}
	return out
}

func runPostToolUse(ctx context.Context, handlers []ConfiguredHandler, shell CommandShell, request PostToolUseRequest) PostToolUseOutcome {
	matcherInputs := MatcherInputs(request.ToolName, request.MatcherAliases)
	matched := selectHandlersForMatcherInputs(handlers, protocol.HookEventNamePostToolUse, matcherInputs)
	if len(matched) == 0 {
		return PostToolUseOutcome{}
	}

	inputJSON, err := postToolUseCommandInputJSON(&request)
	if err != nil {
		turnID := request.TurnID
		events := serializationFailureHookEventsForToolUse(
			matched, &turnID,
			fmt.Sprintf("failed to serialize post tool use hook input: %v", err),
			request.ToolUseID,
		)
		return PostToolUseOutcome{HookEvents: events}
	}

	turnID := request.TurnID
	results := executeHandlers(ctx, shell, matched, inputJSON, request.Cwd.String(), &turnID, parsePostToolUseCompleted)

	contexts := make([][]string, 0, len(results))
	shouldStop := false
	var stopReason *string
	feedbackChunks := make([]string, 0)
	for i := range results {
		contexts = append(contexts, results[i].data.additionalContextsModel)
		if results[i].data.shouldStop {
			shouldStop = true
		}
		if stopReason == nil && results[i].data.stopReason != nil {
			stopReason = results[i].data.stopReason
		}
		feedbackChunks = append(feedbackChunks, results[i].data.feedbackMessagesModel...)
	}
	additionalContexts := flattenAdditionalContexts(contexts)
	feedbackMessage := joinTextChunks(feedbackChunks)

	events := make([]protocol.HookCompletedEvent, 0, len(results))
	for i := range results {
		events = append(events, hookCompletedForToolUse(results[i].completed, request.ToolUseID))
	}

	return PostToolUseOutcome{
		HookEvents:         events,
		ShouldStop:         shouldStop,
		StopReason:         stopReason,
		AdditionalContexts: additionalContexts,
		FeedbackMessage:    feedbackMessage,
	}
}

func postToolUseCommandInputJSON(request *PostToolUseRequest) (string, error) {
	subagent := subagentFieldsFrom(request.Subagent)
	in := postToolUseCommandInput{
		SessionID:      request.SessionID.String(),
		TurnID:         request.TurnID,
		AgentID:        subagent.agentID,
		AgentType:      subagent.agentType,
		TranscriptPath: nullableFromString(request.TranscriptPath),
		Cwd:            request.Cwd.String(),
		HookEventName:  "PostToolUse",
		Model:          request.Model,
		PermissionMode: request.PermissionMode,
		ToolName:       request.ToolName,
		ToolInput:      request.ToolInput,
		ToolResponse:   request.ToolResponse,
		ToolUseID:      request.ToolUseID,
	}
	data, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parsePostToolUseCompleted(handler ConfiguredHandler, run commandRunResult, turnID *string) parsedHandler[postToolUseHandlerData] {
	entries := make([]protocol.HookOutputEntry, 0)
	status := protocol.HookRunStatusCompleted
	shouldStop := false
	var stopReason *string
	contextsForModel := make([]string, 0)
	feedbackForModel := make([]string, 0)

	switch {
	case run.error != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(*run.error))
	case run.exitCode != nil && *run.exitCode == 0:
		if trimmedNonEmpty(run.stdout) != nil {
			if parsed, ok := parsePostToolUse(run.stdout); ok {
				if parsed.universal.SystemMessage != nil {
					entries = append(entries, warningEntry(*parsed.universal.SystemMessage))
				}
				if parsed.invalidReason == nil && parsed.invalidBlockReason == nil && parsed.additionalContext != nil {
					appendAdditionalContext(&entries, &contextsForModel, *parsed.additionalContext)
				}
				switch {
				case !parsed.universal.ContinueProcessing:
					status = protocol.HookRunStatusStopped
					shouldStop = true
					stopReason = parsed.universal.StopReason
					stopText := "PostToolUse hook stopped execution"
					if parsed.universal.StopReason != nil {
						stopText = *parsed.universal.StopReason
					}
					entries = append(entries, stopEntry(stopText))
					modelFeedback := stopText
					if mf := trimmedNonEmptyPtr(parsed.reason); mf != nil {
						modelFeedback = *mf
					}
					feedbackForModel = append(feedbackForModel, modelFeedback)
				case parsed.invalidReason != nil:
					status = protocol.HookRunStatusFailed
					entries = append(entries, errorEntry(*parsed.invalidReason))
				case parsed.invalidBlockReason != nil:
					status = protocol.HookRunStatusFailed
					entries = append(entries, errorEntry(*parsed.invalidBlockReason))
				case parsed.shouldBlock:
					status = protocol.HookRunStatusBlocked
					if parsed.reason != nil {
						entries = append(entries, feedbackEntry(*parsed.reason))
						feedbackForModel = append(feedbackForModel, *parsed.reason)
					}
				}
			} else if looksLikeJSON(run.stdout) {
				status = protocol.HookRunStatusFailed
				entries = append(entries, errorEntry("hook returned invalid post-tool-use JSON output"))
			}
		}
	case run.exitCode != nil && *run.exitCode == 2:
		if reason := trimmedNonEmpty(run.stderr); reason != nil {
			entries = append(entries, feedbackEntry(*reason))
			feedbackForModel = append(feedbackForModel, *reason)
		} else {
			status = protocol.HookRunStatusFailed
			entries = append(entries, errorEntry("PostToolUse hook exited with code 2 but did not write feedback to stderr"))
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
	return parsedHandler[postToolUseHandlerData]{
		completed: completed,
		data: postToolUseHandlerData{
			shouldStop:              shouldStop,
			stopReason:              stopReason,
			additionalContextsModel: contextsForModel,
			feedbackMessagesModel:   feedbackForModel,
		},
	}
}

// trimmedNonEmptyPtr trims a *string, returning nil when nil or blank.
func trimmedNonEmptyPtr(s *string) *string {
	if s == nil {
		return nil
	}
	return trimmedNonEmpty(*s)
}
