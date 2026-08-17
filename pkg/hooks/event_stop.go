package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// StopHookTargetKind discriminates the stop-hook target.
type StopHookTargetKind int

const (
	// StopHookTargetStop targets the Stop event.
	StopHookTargetStop StopHookTargetKind = iota
	// StopHookTargetSubagentStop targets the SubagentStop event.
	StopHookTargetSubagentStop
)

// StopHookTarget selects between a turn-level Stop and a subagent-level
// SubagentStop. Mirrors StopHookTarget.
type StopHookTarget struct {
	Kind StopHookTargetKind

	// SubagentStop fields.
	AgentID             string
	AgentType           string
	AgentTranscriptPath *string
}

func (t StopHookTarget) eventName() protocol.HookEventName {
	if t.Kind == StopHookTargetSubagentStop {
		return protocol.HookEventNameSubagentStop
	}
	return protocol.HookEventNameStop
}

func (t StopHookTarget) matcherInput() *string {
	if t.Kind == StopHookTargetSubagentStop {
		return &t.AgentType
	}
	return nil
}

// StopRequest is the input for a Stop/SubagentStop dispatch. Mirrors StopRequest.
type StopRequest struct {
	SessionID            protocol.ThreadID
	TurnID               string
	Cwd                  abspath.AbsolutePathBuf
	TranscriptPath       *string
	Model                string
	PermissionMode       string
	StopHookActive       bool
	LastAssistantMessage *string
	Target               StopHookTarget
}

// StopOutcome is the merged result of a Stop/SubagentStop dispatch. Mirrors
// StopOutcome.
type StopOutcome struct {
	HookEvents            []protocol.HookCompletedEvent
	ShouldStop            bool
	StopReason            *string
	ShouldBlock           bool
	BlockReason           *string
	ContinuationFragments []protocol.HookPromptFragment
}

type stopHandlerData struct {
	shouldStop            bool
	stopReason            *string
	shouldBlock           bool
	blockReason           *string
	continuationFragments []protocol.HookPromptFragment
}

func previewStop(handlers []ConfiguredHandler, request *StopRequest) []protocol.HookRunSummary {
	matched := selectHandlers(handlers, request.Target.eventName(), request.Target.matcherInput())
	out := make([]protocol.HookRunSummary, 0, len(matched))
	for i := range matched {
		out = append(out, runningSummary(matched[i]))
	}
	return out
}

func runStop(ctx context.Context, handlers []ConfiguredHandler, shell CommandShell, request StopRequest) StopOutcome {
	matched := selectHandlers(handlers, request.Target.eventName(), request.Target.matcherInput())
	if len(matched) == 0 {
		return StopOutcome{}
	}

	inputJSON, err := stopCommandInputJSON(&request)
	if err != nil {
		turnID := request.TurnID
		label := "failed to serialize stop hook input"
		if request.Target.Kind == StopHookTargetSubagentStop {
			label = "failed to serialize subagent stop hook input"
		}
		events := serializationFailureHookEvents(matched, &turnID, fmt.Sprintf("%s: %v", label, err))
		return StopOutcome{HookEvents: events}
	}

	turnID := request.TurnID
	results := executeHandlers(ctx, shell, matched, inputJSON, request.Cwd.String(), &turnID, parseStopCompleted)

	datas := make([]stopHandlerData, 0, len(results))
	for i := range results {
		datas = append(datas, results[i].data)
	}
	aggregate := aggregateStopResults(datas)

	events := make([]protocol.HookCompletedEvent, 0, len(results))
	for i := range results {
		events = append(events, results[i].completed)
	}

	return StopOutcome{
		HookEvents:            events,
		ShouldStop:            aggregate.shouldStop,
		StopReason:            aggregate.stopReason,
		ShouldBlock:           aggregate.shouldBlock,
		BlockReason:           aggregate.blockReason,
		ContinuationFragments: aggregate.continuationFragments,
	}
}

func stopCommandInputJSON(request *StopRequest) (string, error) {
	if request.Target.Kind == StopHookTargetSubagentStop {
		in := subagentStopCommandInput{
			SessionID:            request.SessionID.String(),
			TurnID:               request.TurnID,
			TranscriptPath:       nullableFromString(request.TranscriptPath),
			AgentTranscriptPath:  nullableFromString(request.Target.AgentTranscriptPath),
			Cwd:                  request.Cwd.String(),
			HookEventName:        "SubagentStop",
			Model:                request.Model,
			PermissionMode:       request.PermissionMode,
			StopHookActive:       request.StopHookActive,
			AgentID:              request.Target.AgentID,
			AgentType:            request.Target.AgentType,
			LastAssistantMessage: nullableFromString(request.LastAssistantMessage),
		}
		data, err := json.Marshal(in)
		return string(data), err
	}

	in := stopCommandInput{
		SessionID:            request.SessionID.String(),
		TurnID:               request.TurnID,
		TranscriptPath:       nullableFromString(request.TranscriptPath),
		Cwd:                  request.Cwd.String(),
		HookEventName:        "Stop",
		Model:                request.Model,
		PermissionMode:       request.PermissionMode,
		StopHookActive:       request.StopHookActive,
		LastAssistantMessage: nullableFromString(request.LastAssistantMessage),
	}
	data, err := json.Marshal(in)
	return string(data), err
}

func parseStopCompleted(handler ConfiguredHandler, run commandRunResult, turnID *string) parsedHandler[stopHandlerData] {
	entries := make([]protocol.HookOutputEntry, 0)
	status := protocol.HookRunStatusCompleted
	shouldStop := false
	var stopReason *string
	shouldBlock := false
	var blockReason *string
	var continuationPrompt *string

	isStop := handler.EventName == protocol.HookEventNameStop

	switch {
	case run.error != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(*run.error))
	case run.exitCode != nil && *run.exitCode == 0:
		if trimmedNonEmpty(run.stdout) != nil {
			var parsed *stopOutput
			var ok bool
			if isStop {
				parsed, ok = parseStop(run.stdout)
			} else {
				parsed, ok = parseSubagentStop(run.stdout)
			}
			if ok {
				if parsed.universal.SystemMessage != nil {
					entries = append(entries, warningEntry(*parsed.universal.SystemMessage))
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
					if reason := trimmedNonEmptyPtr(parsed.reason); reason != nil {
						status = protocol.HookRunStatusBlocked
						shouldBlock = true
						blockReason = reason
						continuationPrompt = reason
						entries = append(entries, feedbackEntry(*reason))
					} else {
						status = protocol.HookRunStatusFailed
						entries = append(entries, errorEntry(stopBlockReasonMissing(isStop)))
					}
				}
			} else {
				status = protocol.HookRunStatusFailed
				if isStop {
					entries = append(entries, errorEntry("hook returned invalid stop hook JSON output"))
				} else {
					entries = append(entries, errorEntry("hook returned invalid subagent stop hook JSON output"))
				}
			}
		}
	case run.exitCode != nil && *run.exitCode == 2:
		if reason := trimmedNonEmpty(run.stderr); reason != nil {
			status = protocol.HookRunStatusBlocked
			shouldBlock = true
			blockReason = reason
			continuationPrompt = reason
			entries = append(entries, feedbackEntry(*reason))
		} else {
			status = protocol.HookRunStatusFailed
			entries = append(entries, errorEntry(stopExitTwoMissing(isStop)))
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

	var fragments []protocol.HookPromptFragment
	if continuationPrompt != nil {
		fragments = []protocol.HookPromptFragment{{
			Text:      *continuationPrompt,
			HookRunID: completed.Run.ID,
		}}
	}

	return parsedHandler[stopHandlerData]{
		completed: completed,
		data: stopHandlerData{
			shouldStop:            shouldStop,
			stopReason:            stopReason,
			shouldBlock:           shouldBlock,
			blockReason:           blockReason,
			continuationFragments: fragments,
		},
	}
}

func stopBlockReasonMissing(isStop bool) string {
	if isStop {
		return "Stop hook returned decision:block without a non-empty reason"
	}
	return "SubagentStop hook returned decision:block without a non-empty reason"
}

func stopExitTwoMissing(isStop bool) string {
	if isStop {
		return "Stop hook exited with code 2 but did not write a continuation prompt to stderr"
	}
	return "SubagentStop hook exited with code 2 but did not write a continuation prompt to stderr"
}

// aggregateStopResults folds per-handler stop data. Mirrors aggregate_results.
func aggregateStopResults(results []stopHandlerData) stopHandlerData {
	shouldStop := false
	var stopReason *string
	anyBlock := false
	for i := range results {
		if results[i].shouldStop {
			shouldStop = true
		}
		if stopReason == nil && results[i].stopReason != nil {
			stopReason = results[i].stopReason
		}
		if results[i].shouldBlock {
			anyBlock = true
		}
	}
	shouldBlock := !shouldStop && anyBlock

	var blockReason *string
	var continuationFragments []protocol.HookPromptFragment
	if shouldBlock {
		blockChunks := make([]string, 0)
		continuationFragments = make([]protocol.HookPromptFragment, 0)
		for i := range results {
			if results[i].blockReason != nil {
				blockChunks = append(blockChunks, *results[i].blockReason)
			}
			if results[i].shouldBlock {
				continuationFragments = append(continuationFragments, results[i].continuationFragments...)
			}
		}
		blockReason = joinTextChunks(blockChunks)
	}

	return stopHandlerData{
		shouldStop:            shouldStop,
		stopReason:            stopReason,
		shouldBlock:           shouldBlock,
		blockReason:           blockReason,
		continuationFragments: continuationFragments,
	}
}
