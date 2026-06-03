package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// PermissionRequestDecisionKind discriminates a public permission-request
// decision. Mirrors the PermissionRequestDecision enum variants.
type PermissionRequestDecisionKind int

const (
	// PermissionRequestAllow allows the tool call.
	PermissionRequestAllow PermissionRequestDecisionKind = iota
	// PermissionRequestDeny denies the tool call with a message.
	PermissionRequestDeny
)

// PermissionRequestDecision is a hook-supplied allow/deny verdict. Mirrors the
// public PermissionRequestDecision enum.
type PermissionRequestDecision struct {
	Kind    PermissionRequestDecisionKind
	Message string
}

// PermissionRequestRequest is the input for a PermissionRequest dispatch.
// Mirrors PermissionRequestRequest. Cwd is a free-form path string (Rust uses
// PathBuf here, not AbsolutePathBuf).
type PermissionRequestRequest struct {
	SessionID      protocol.ThreadID
	TurnID         string
	Subagent       *SubagentHookContext
	Cwd            string
	TranscriptPath *string
	Model          string
	PermissionMode string
	ToolName       string
	MatcherAliases []string
	RunIDSuffix    string
	ToolInput      json.RawMessage
}

// PermissionRequestOutcome is the merged result of a PermissionRequest dispatch.
// Mirrors PermissionRequestOutcome.
type PermissionRequestOutcome struct {
	HookEvents []protocol.HookCompletedEvent
	Decision   *PermissionRequestDecision
}

type permissionRequestHandlerData struct {
	decision *PermissionRequestDecision
}

func previewPermissionRequest(handlers []ConfiguredHandler, request *PermissionRequestRequest) []protocol.HookRunSummary {
	matcherInputs := MatcherInputs(request.ToolName, request.MatcherAliases)
	matched := selectHandlersForMatcherInputs(handlers, protocol.HookEventNamePermissionRequest, matcherInputs)
	out := make([]protocol.HookRunSummary, 0, len(matched))
	for i := range matched {
		out = append(out, hookRunForToolUse(runningSummary(matched[i]), request.RunIDSuffix))
	}
	return out
}

func runPermissionRequest(ctx context.Context, handlers []ConfiguredHandler, shell CommandShell, request PermissionRequestRequest) PermissionRequestOutcome {
	matcherInputs := MatcherInputs(request.ToolName, request.MatcherAliases)
	matched := selectHandlersForMatcherInputs(handlers, protocol.HookEventNamePermissionRequest, matcherInputs)
	if len(matched) == 0 {
		return PermissionRequestOutcome{}
	}

	inputJSON, err := permissionRequestCommandInputJSON(&request)
	if err != nil {
		turnID := request.TurnID
		events := serializationFailureHookEventsForToolUse(
			matched, &turnID,
			fmt.Sprintf("failed to serialize permission request hook input: %v", err),
			request.RunIDSuffix,
		)
		return PermissionRequestOutcome{HookEvents: events}
	}

	turnID := request.TurnID
	results := executeHandlers(ctx, shell, matched, inputJSON, request.Cwd, &turnID, parsePermissionRequestCompleted)

	decisions := make([]*PermissionRequestDecision, 0, len(results))
	for i := range results {
		decisions = append(decisions, results[i].data.decision)
	}
	decision := resolvePermissionRequestDecision(decisions)

	events := make([]protocol.HookCompletedEvent, 0, len(results))
	for i := range results {
		events = append(events, hookCompletedForToolUse(results[i].completed, request.RunIDSuffix))
	}

	return PermissionRequestOutcome{HookEvents: events, Decision: decision}
}

// resolvePermissionRequestDecision folds decisions conservatively: any deny wins
// immediately; otherwise the highest-precedence allow wins. Mirrors
// resolve_permission_request_decision.
func resolvePermissionRequestDecision(decisions []*PermissionRequestDecision) *PermissionRequestDecision {
	var resolvedAllow *PermissionRequestDecision
	for _, d := range decisions {
		if d == nil {
			continue
		}
		switch d.Kind {
		case PermissionRequestAllow:
			allow := PermissionRequestDecision{Kind: PermissionRequestAllow}
			resolvedAllow = &allow
		case PermissionRequestDeny:
			deny := PermissionRequestDecision{Kind: PermissionRequestDeny, Message: d.Message}
			return &deny
		}
	}
	return resolvedAllow
}

func permissionRequestCommandInputJSON(request *PermissionRequestRequest) (string, error) {
	subagent := subagentFieldsFrom(request.Subagent)
	in := permissionRequestCommandInput{
		SessionID:      request.SessionID.String(),
		TurnID:         request.TurnID,
		AgentID:        subagent.agentID,
		AgentType:      subagent.agentType,
		TranscriptPath: nullableFromString(request.TranscriptPath),
		Cwd:            request.Cwd,
		HookEventName:  "PermissionRequest",
		Model:          request.Model,
		PermissionMode: request.PermissionMode,
		ToolName:       request.ToolName,
		ToolInput:      request.ToolInput,
	}
	data, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parsePermissionRequestCompleted(handler ConfiguredHandler, run commandRunResult, turnID *string) parsedHandler[permissionRequestHandlerData] {
	entries := make([]protocol.HookOutputEntry, 0)
	status := protocol.HookRunStatusCompleted
	var decision *PermissionRequestDecision

	switch {
	case run.error != nil:
		status = protocol.HookRunStatusFailed
		entries = append(entries, errorEntry(*run.error))
	case run.exitCode != nil && *run.exitCode == 0:
		if trimmedNonEmpty(run.stdout) != nil {
			if parsed, ok := parsePermissionRequest(run.stdout); ok {
				if parsed.universal.SystemMessage != nil {
					entries = append(entries, warningEntry(*parsed.universal.SystemMessage))
				}
				if parsed.invalidReason != nil {
					status = protocol.HookRunStatusFailed
					entries = append(entries, errorEntry(*parsed.invalidReason))
				} else if parsed.decision != nil {
					switch parsed.decision.kind {
					case permissionRequestDecisionAllow:
						decision = &PermissionRequestDecision{Kind: PermissionRequestAllow}
					case permissionRequestDecisionDeny:
						status = protocol.HookRunStatusBlocked
						entries = append(entries, feedbackEntry(parsed.decision.message))
						decision = &PermissionRequestDecision{Kind: PermissionRequestDeny, Message: parsed.decision.message}
					}
				}
			} else if looksLikeJSON(run.stdout) {
				status = protocol.HookRunStatusFailed
				entries = append(entries, errorEntry("hook returned invalid permission-request JSON output"))
			}
		}
	case run.exitCode != nil && *run.exitCode == 2:
		if message := trimmedNonEmpty(run.stderr); message != nil {
			status = protocol.HookRunStatusBlocked
			entries = append(entries, feedbackEntry(*message))
			decision = &PermissionRequestDecision{Kind: PermissionRequestDeny, Message: *message}
		} else {
			status = protocol.HookRunStatusFailed
			entries = append(entries, errorEntry("PermissionRequest hook exited with code 2 but did not write a denial reason to stderr"))
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
	return parsedHandler[permissionRequestHandlerData]{
		completed: completed,
		data:      permissionRequestHandlerData{decision: decision},
	}
}
