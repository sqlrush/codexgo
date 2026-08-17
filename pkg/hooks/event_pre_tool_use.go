package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// PreToolUseRequest is the input for a PreToolUse dispatch. Mirrors
// PreToolUseRequest.
type PreToolUseRequest struct {
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
}

// PreToolUseOutcome is the merged result of a PreToolUse dispatch. Mirrors
// PreToolUseOutcome.
type PreToolUseOutcome struct {
	HookEvents         []protocol.HookCompletedEvent
	ShouldBlock        bool
	BlockReason        *string
	AdditionalContexts []string
	UpdatedInput       json.RawMessage
}

type preToolUseHandlerData struct {
	shouldBlock             bool
	blockReason             *string
	additionalContextsModel []string
	updatedInput            json.RawMessage
}

func previewPreToolUse(handlers []ConfiguredHandler, request *PreToolUseRequest) []protocol.HookRunSummary {
	matcherInputs := MatcherInputs(request.ToolName, request.MatcherAliases)
	matched := selectHandlersForMatcherInputs(handlers, protocol.HookEventNamePreToolUse, matcherInputs)
	out := make([]protocol.HookRunSummary, 0, len(matched))
	for i := range matched {
		out = append(out, hookRunForToolUse(runningSummary(matched[i]), request.ToolUseID))
	}
	return out
}

func runPreToolUse(ctx context.Context, handlers []ConfiguredHandler, shell CommandShell, request PreToolUseRequest) PreToolUseOutcome {
	matcherInputs := MatcherInputs(request.ToolName, request.MatcherAliases)
	matched := selectHandlersForMatcherInputs(handlers, protocol.HookEventNamePreToolUse, matcherInputs)
	if len(matched) == 0 {
		return PreToolUseOutcome{}
	}

	inputJSON, err := preToolUseCommandInputJSON(&request)
	if err != nil {
		turnID := request.TurnID
		events := serializationFailureHookEventsForToolUse(
			matched, &turnID,
			fmt.Sprintf("failed to serialize pre tool use hook input: %v", err),
			request.ToolUseID,
		)
		return PreToolUseOutcome{HookEvents: events}
	}

	turnID := request.TurnID
	results := executeHandlers(ctx, shell, matched, inputJSON, request.Cwd.String(), &turnID, parsePreToolUseCompleted)

	shouldBlock := false
	var blockReason *string
	contexts := make([][]string, 0, len(results))
	for i := range results {
		if results[i].data.shouldBlock {
			shouldBlock = true
		}
		if blockReason == nil && results[i].data.blockReason != nil {
			blockReason = results[i].data.blockReason
		}
		contexts = append(contexts, results[i].data.additionalContextsModel)
	}
	additionalContexts := flattenAdditionalContexts(contexts)

	var updatedInput json.RawMessage
	if !shouldBlock {
		updatedInput = latestUpdatedInput(results)
	}

	events := make([]protocol.HookCompletedEvent, 0, len(results))
	for i := range results {
		events = append(events, hookCompletedForToolUse(results[i].completed, request.ToolUseID))
	}

	return PreToolUseOutcome{
		HookEvents:         events,
		ShouldBlock:        shouldBlock,
		BlockReason:        blockReason,
		AdditionalContexts: additionalContexts,
		UpdatedInput:       updatedInput,
	}
}

// latestUpdatedInput chooses the rewrite from the hook that finished last.
// Mirrors latest_updated_input.
func latestUpdatedInput(results []parsedHandler[preToolUseHandlerData]) json.RawMessage {
	var chosen json.RawMessage
	best := -1
	for i := range results {
		if results[i].data.updatedInput == nil {
			continue
		}
		if results[i].completionOrder >= best {
			best = results[i].completionOrder
			chosen = results[i].data.updatedInput
		}
	}
	return chosen
}

func preToolUseCommandInputJSON(request *PreToolUseRequest) (string, error) {
	subagent := subagentFieldsFrom(request.Subagent)
	in := preToolUseCommandInput{
		SessionID:      request.SessionID.String(),
		TurnID:         request.TurnID,
		AgentID:        subagent.agentID,
		AgentType:      subagent.agentType,
		TranscriptPath: nullableFromString(request.TranscriptPath),
		Cwd:            request.Cwd.String(),
		HookEventName:  "PreToolUse",
		Model:          request.Model,
		PermissionMode: request.PermissionMode,
		ToolName:       request.ToolName,
		ToolInput:      request.ToolInput,
		ToolUseID:      request.ToolUseID,
	}
	data, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parsePreToolUseCompleted(handler ConfiguredHandler, run commandRunResult, turnID *string) parsedHandler[preToolUseHandlerData] {
	entries := make([]protocol.HookOutputEntry, 0)
	status := protocol.HookRunStatusCompleted
	shouldBlock := false
	var blockReason *string
	contextsForModel := make([]string, 0)
	var updatedInput json.RawMessage

	switch {
	case run.error != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(*run.error))
	case run.exitCode != nil && *run.exitCode == 0:
		if trimmedNonEmpty(run.stdout) != nil {
			if parsed, ok := parsePreToolUse(run.stdout); ok {
				if parsed.universal.SystemMessage != nil {
					entries = append(entries, warningEntry(*parsed.universal.SystemMessage))
				}
				if parsed.invalidReason != nil {
					status = protocol.HookRunStatusFailed
					entries = append(entries, errorEntry(*parsed.invalidReason))
				} else {
					if parsed.additionalContext != nil {
						appendAdditionalContext(&entries, &contextsForModel, *parsed.additionalContext)
					}
					if parsed.blockReason != nil {
						status = protocol.HookRunStatusBlocked
						shouldBlock = true
						blockReason = parsed.blockReason
						entries = append(entries, feedbackEntry(*parsed.blockReason))
					}
					if !shouldBlock {
						updatedInput = parsed.updatedInput
					}
				}
			} else if looksLikeJSON(run.stdout) {
				status = protocol.HookRunStatusFailed
				entries = append(entries, errorEntry("hook returned invalid pre-tool-use JSON output"))
			}
		}
	case run.exitCode != nil && *run.exitCode == 2:
		if reason := trimmedNonEmpty(run.stderr); reason != nil {
			status = protocol.HookRunStatusBlocked
			shouldBlock = true
			blockReason = reason
			entries = append(entries, feedbackEntry(*reason))
		} else {
			status = protocol.HookRunStatusFailed
			entries = append(entries, errorEntry("PreToolUse hook exited with code 2 but did not write a blocking reason to stderr"))
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
	return parsedHandler[preToolUseHandlerData]{
		completed: completed,
		data: preToolUseHandlerData{
			shouldBlock:             shouldBlock,
			blockReason:             blockReason,
			additionalContextsModel: contextsForModel,
			updatedInput:            updatedInput,
		},
	}
}
