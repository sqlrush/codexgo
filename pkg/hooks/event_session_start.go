package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// SessionStartSource enumerates the cause of a SessionStart hook. Mirrors
// SessionStartSource.
type SessionStartSource string

const (
	// SessionStartStartup is a fresh process launch.
	SessionStartStartup SessionStartSource = "startup"
	// SessionStartResume resumes an existing session.
	SessionStartResume SessionStartSource = "resume"
	// SessionStartClear clears the conversation.
	SessionStartClear SessionStartSource = "clear"
	// SessionStartCompact follows a compaction.
	SessionStartCompact SessionStartSource = "compact"
)

// StartHookTargetKind discriminates the start-hook target.
type StartHookTargetKind int

const (
	// StartHookTargetSessionStart targets the SessionStart event.
	StartHookTargetSessionStart StartHookTargetKind = iota
	// StartHookTargetSubagentStart targets the SubagentStart event.
	StartHookTargetSubagentStart
)

// StartHookTarget selects between a session-level and a subagent-level start
// hook. Mirrors StartHookTarget.
type StartHookTarget struct {
	Kind StartHookTargetKind

	// SessionStart fields.
	Source SessionStartSource

	// SubagentStart fields.
	TurnID    string
	AgentID   string
	AgentType string
}

func (t StartHookTarget) eventName() protocol.HookEventName {
	if t.Kind == StartHookTargetSubagentStart {
		return protocol.HookEventNameSubagentStart
	}
	return protocol.HookEventNameSessionStart
}

func (t StartHookTarget) matcherInput() string {
	if t.Kind == StartHookTargetSubagentStart {
		return t.AgentType
	}
	return string(t.Source)
}

// SessionStartRequest is the input for a SessionStart/SubagentStart dispatch.
// Mirrors SessionStartRequest.
type SessionStartRequest struct {
	SessionID      protocol.ThreadID
	Cwd            abspath.AbsolutePathBuf
	TranscriptPath *string
	Model          string
	PermissionMode string
	Target         StartHookTarget
}

// SessionStartOutcome is the merged result of a start dispatch. Mirrors
// SessionStartOutcome.
type SessionStartOutcome struct {
	HookEvents         []protocol.HookCompletedEvent
	ShouldStop         bool
	StopReason         *string
	AdditionalContexts []string
}

type sessionStartHandlerData struct {
	shouldStop              bool
	stopReason              *string
	additionalContextsModel []string
}

func previewSessionStart(handlers []ConfiguredHandler, request *SessionStartRequest) []protocol.HookRunSummary {
	matcherInput := request.Target.matcherInput()
	matched := selectHandlers(handlers, request.Target.eventName(), &matcherInput)
	out := make([]protocol.HookRunSummary, 0, len(matched))
	for i := range matched {
		out = append(out, runningSummary(matched[i]))
	}
	return out
}

func runSessionStart(ctx context.Context, handlers []ConfiguredHandler, shell CommandShell, request SessionStartRequest, turnID *string) SessionStartOutcome {
	matcherInput := request.Target.matcherInput()
	matched := selectHandlers(handlers, request.Target.eventName(), &matcherInput)
	if len(matched) == 0 {
		return SessionStartOutcome{}
	}

	inputJSON, effectiveTurnID, err, errLabel := buildStartInput(&request, turnID)
	if err != nil {
		events := serializationFailureHookEvents(matched, effectiveTurnID, fmt.Sprintf("%s: %v", errLabel, err))
		return SessionStartOutcome{HookEvents: events}
	}

	results := executeHandlers(ctx, shell, matched, inputJSON, request.Cwd.String(), effectiveTurnID, parseSessionStartCompleted)

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

	return SessionStartOutcome{
		HookEvents:         events,
		ShouldStop:         shouldStop,
		StopReason:         stopReason,
		AdditionalContexts: flattenAdditionalContexts(contexts),
	}
}

// buildStartInput serializes the per-target command stdin. It returns the JSON,
// the turn id to thread through, any serialization error, and the error label
// to prefix that error with. Mirrors the match in session_start::run.
func buildStartInput(request *SessionStartRequest, turnID *string) (string, *string, error, string) {
	if request.Target.Kind == StartHookTargetSubagentStart {
		subagentTurnID := request.Target.TurnID
		in := subagentStartCommandInput{
			SessionID:      request.SessionID.String(),
			TurnID:         subagentTurnID,
			TranscriptPath: nullableFromString(request.TranscriptPath),
			Cwd:            request.Cwd.String(),
			HookEventName:  "SubagentStart",
			Model:          request.Model,
			PermissionMode: request.PermissionMode,
			AgentID:        request.Target.AgentID,
			AgentType:      request.Target.AgentType,
		}
		data, err := json.Marshal(in)
		return string(data), &subagentTurnID, err, "failed to serialize subagent start hook input"
	}

	in := sessionStartCommandInput{
		SessionID:      request.SessionID.String(),
		TranscriptPath: nullableFromString(request.TranscriptPath),
		Cwd:            request.Cwd.String(),
		HookEventName:  "SessionStart",
		Model:          request.Model,
		PermissionMode: request.PermissionMode,
		Source:         string(request.Target.Source),
	}
	data, err := json.Marshal(in)
	return string(data), turnID, err, "failed to serialize session start hook input"
}

func parseSessionStartCompleted(handler ConfiguredHandler, run commandRunResult, turnID *string) parsedHandler[sessionStartHandlerData] {
	entries := make([]protocol.HookOutputEntry, 0)
	status := protocol.HookRunStatusCompleted
	shouldStop := false
	var stopReason *string
	contextsForModel := make([]string, 0)

	isSessionStart := handler.EventName == protocol.HookEventNameSessionStart

	switch {
	case run.error != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(*run.error))
	case run.exitCode != nil && *run.exitCode == 0:
		trimmed := trimmedNonEmpty(run.stdout)
		if trimmed != nil {
			var parsed *sessionStartOutput
			var ok bool
			if isSessionStart {
				parsed, ok = parseSessionStart(run.stdout)
			} else {
				parsed, ok = parseSubagentStart(run.stdout)
			}
			if ok {
				if parsed.universal.SystemMessage != nil {
					entries = append(entries, warningEntry(*parsed.universal.SystemMessage))
				}
				if parsed.additionalContext != nil {
					appendAdditionalContext(&entries, &contextsForModel, *parsed.additionalContext)
				}
				if isSessionStart && !parsed.universal.ContinueProcessing {
					status = protocol.HookRunStatusStopped
					shouldStop = true
					stopReason = parsed.universal.StopReason
					if parsed.universal.StopReason != nil {
						entries = append(entries, stopEntry(*parsed.universal.StopReason))
					}
				}
			} else if looksLikeJSON(run.stdout) {
				status = protocol.HookRunStatusFailed
				if isSessionStart {
					entries = append(entries, errorEntry("hook returned invalid session start JSON output"))
				} else {
					entries = append(entries, errorEntry("hook returned invalid subagent start JSON output"))
				}
			} else {
				appendAdditionalContext(&entries, &contextsForModel, *trimmed)
			}
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
	return parsedHandler[sessionStartHandlerData]{
		completed: completed,
		data: sessionStartHandlerData{
			shouldStop:              shouldStop,
			stopReason:              stopReason,
			additionalContextsModel: contextsForModel,
		},
	}
}
