package protocol

import (
	"encoding/json"
	"fmt"
)

// This file ports the engine->client event protocol from `protocol.rs`:
//   - the Event envelope (`{ "id": ..., "msg": ... }`),
//   - the EventMsg internally-tagged enum (`#[serde(tag = "type")]`), and
//   - every EventMsg payload struct.
//
// Fidelity notes:
//   - EventMsg uses serde `rename_all = "snake_case"`, so the discriminator
//     strings are the snake_case of each variant name. Two variants carry
//     v1/v2 wire aliases: TurnStarted emits "task_started" (accepts
//     "turn_started") and TurnComplete emits "task_complete" (accepts
//     "turn_complete"). The custom (Un)MarshalJSON below preserves this.
//   - Several fields reference types owned by sibling protocol modules that may
//     not be ported yet (FileChange and ReviewDecision from op.go;
//     ReasoningEffortConfig / ReasoningSummaryConfig / PermissionProfile /
//     ActivePermissionProfile from config; ParsedCommand from parse_command;
//     RealtimeEvent / RealtimeVoicesList from realtime; the approval-request
//     events from approvals.rs). To keep this file self-contained and to
//     round-trip losslessly, those fields are modeled as json.RawMessage,
//     following the precedent set by items.go (FileChangeItem.Changes,
//     McpToolCallItem.Result, etc.). When the owning types land, callers may
//     decode these raw payloads with them.
//   - std::time::Duration serializes with serde's default representation
//     (`{"secs":N,"nanos":N}`), NOT a string (the `#[ts(type="string")]` only
//     affects TypeScript codegen). Duration fields are kept as json.RawMessage.

// Event is the engine->client event envelope. Mirrors Rust `Event`.
type Event struct {
	// ID is the submission id this event is correlated with.
	ID string `json:"id"`
	// Msg is the event payload.
	Msg EventMsg `json:"msg"`
}

// EventMsgKind is the discriminator (the `type` field) for EventMsg. Values are
// the serde snake_case of the Rust variant names. The two task_*/turn_*
// variants store their emitted (default) dialect here.
type EventMsgKind string

const (
	EventMsgKindError                                  EventMsgKind = "error"
	EventMsgKindWarning                                EventMsgKind = "warning"
	EventMsgKindGuardianWarning                        EventMsgKind = "guardian_warning"
	EventMsgKindRealtimeConversationStarted            EventMsgKind = "realtime_conversation_started"
	EventMsgKindRealtimeConversationRealtime           EventMsgKind = "realtime_conversation_realtime"
	EventMsgKindRealtimeConversationClosed             EventMsgKind = "realtime_conversation_closed"
	EventMsgKindRealtimeConversationSdp                EventMsgKind = "realtime_conversation_sdp"
	EventMsgKindModelReroute                           EventMsgKind = "model_reroute"
	EventMsgKindModelVerification                      EventMsgKind = "model_verification"
	EventMsgKindContextCompacted                       EventMsgKind = "context_compacted"
	EventMsgKindThreadRolledBack                       EventMsgKind = "thread_rolled_back"
	EventMsgKindTurnStarted                            EventMsgKind = "task_started"
	EventMsgKindThreadSettingsApplied                  EventMsgKind = "thread_settings_applied"
	EventMsgKindTurnComplete                           EventMsgKind = "task_complete"
	EventMsgKindTokenCount                             EventMsgKind = "token_count"
	EventMsgKindAgentMessage                           EventMsgKind = "agent_message"
	EventMsgKindUserMessage                            EventMsgKind = "user_message"
	EventMsgKindAgentReasoning                         EventMsgKind = "agent_reasoning"
	EventMsgKindAgentReasoningRawContent               EventMsgKind = "agent_reasoning_raw_content"
	EventMsgKindAgentReasoningSectionBreak             EventMsgKind = "agent_reasoning_section_break"
	EventMsgKindSessionConfigured                      EventMsgKind = "session_configured"
	EventMsgKindThreadGoalUpdated                      EventMsgKind = "thread_goal_updated"
	EventMsgKindMcpStartupUpdate                       EventMsgKind = "mcp_startup_update"
	EventMsgKindMcpStartupComplete                     EventMsgKind = "mcp_startup_complete"
	EventMsgKindMcpToolCallBegin                       EventMsgKind = "mcp_tool_call_begin"
	EventMsgKindMcpToolCallEnd                         EventMsgKind = "mcp_tool_call_end"
	EventMsgKindWebSearchBegin                         EventMsgKind = "web_search_begin"
	EventMsgKindWebSearchEnd                           EventMsgKind = "web_search_end"
	EventMsgKindImageGenerationBegin                   EventMsgKind = "image_generation_begin"
	EventMsgKindImageGenerationEnd                     EventMsgKind = "image_generation_end"
	EventMsgKindExecCommandBegin                       EventMsgKind = "exec_command_begin"
	EventMsgKindExecCommandOutputDelta                 EventMsgKind = "exec_command_output_delta"
	EventMsgKindTerminalInteraction                    EventMsgKind = "terminal_interaction"
	EventMsgKindExecCommandEnd                         EventMsgKind = "exec_command_end"
	EventMsgKindViewImageToolCall                      EventMsgKind = "view_image_tool_call"
	EventMsgKindExecApprovalRequest                    EventMsgKind = "exec_approval_request"
	EventMsgKindRequestPermissions                     EventMsgKind = "request_permissions"
	EventMsgKindRequestUserInput                       EventMsgKind = "request_user_input"
	EventMsgKindDynamicToolCallRequest                 EventMsgKind = "dynamic_tool_call_request"
	EventMsgKindDynamicToolCallResponse                EventMsgKind = "dynamic_tool_call_response"
	EventMsgKindElicitationRequest                     EventMsgKind = "elicitation_request"
	EventMsgKindApplyPatchApprovalRequest              EventMsgKind = "apply_patch_approval_request"
	EventMsgKindGuardianAssessment                     EventMsgKind = "guardian_assessment"
	EventMsgKindDeprecationNotice                      EventMsgKind = "deprecation_notice"
	EventMsgKindStreamError                            EventMsgKind = "stream_error"
	EventMsgKindPatchApplyBegin                        EventMsgKind = "patch_apply_begin"
	EventMsgKindPatchApplyUpdated                      EventMsgKind = "patch_apply_updated"
	EventMsgKindPatchApplyEnd                          EventMsgKind = "patch_apply_end"
	EventMsgKindTurnDiff                               EventMsgKind = "turn_diff"
	EventMsgKindRealtimeConversationListVoicesResponse EventMsgKind = "realtime_conversation_list_voices_response"
	EventMsgKindPlanUpdate                             EventMsgKind = "plan_update"
	EventMsgKindTurnAborted                            EventMsgKind = "turn_aborted"
	EventMsgKindShutdownComplete                       EventMsgKind = "shutdown_complete"
	EventMsgKindEnteredReviewMode                      EventMsgKind = "entered_review_mode"
	EventMsgKindExitedReviewMode                       EventMsgKind = "exited_review_mode"
	EventMsgKindRawResponseItem                        EventMsgKind = "raw_response_item"
	EventMsgKindItemStarted                            EventMsgKind = "item_started"
	EventMsgKindItemCompleted                          EventMsgKind = "item_completed"
	EventMsgKindHookStarted                            EventMsgKind = "hook_started"
	EventMsgKindHookCompleted                          EventMsgKind = "hook_completed"
	EventMsgKindAgentMessageContentDelta               EventMsgKind = "agent_message_content_delta"
	EventMsgKindPlanDelta                              EventMsgKind = "plan_delta"
	EventMsgKindReasoningContentDelta                  EventMsgKind = "reasoning_content_delta"
	EventMsgKindReasoningRawContentDelta               EventMsgKind = "reasoning_raw_content_delta"
	EventMsgKindCollabAgentSpawnBegin                  EventMsgKind = "collab_agent_spawn_begin"
	EventMsgKindCollabAgentSpawnEnd                    EventMsgKind = "collab_agent_spawn_end"
	EventMsgKindCollabAgentInteractionBegin            EventMsgKind = "collab_agent_interaction_begin"
	EventMsgKindCollabAgentInteractionEnd              EventMsgKind = "collab_agent_interaction_end"
	EventMsgKindCollabWaitingBegin                     EventMsgKind = "collab_waiting_begin"
	EventMsgKindCollabWaitingEnd                       EventMsgKind = "collab_waiting_end"
	EventMsgKindCollabCloseBegin                       EventMsgKind = "collab_close_begin"
	EventMsgKindCollabCloseEnd                         EventMsgKind = "collab_close_end"
	EventMsgKindCollabResumeBegin                      EventMsgKind = "collab_resume_begin"
	EventMsgKindCollabResumeEnd                        EventMsgKind = "collab_resume_end"
)

// v1/v2 wire aliases accepted on input for the task_*/turn_* variants.
const (
	eventMsgAliasTurnStarted  EventMsgKind = "turn_started"
	eventMsgAliasTurnComplete EventMsgKind = "turn_complete"
)

// EventMsg is the engine->client event payload: an internally-tagged enum on
// the `type` field. Exactly one of the variant pointers is non-nil for a
// payload-carrying variant; payload-less variants (ShutdownComplete) carry only
// the Type discriminator.
//
// Unknown discriminators are tolerated for forward compatibility: Type is set
// to the observed string and Raw retains the full object so it can be
// re-emitted byte-for-byte.
type EventMsg struct {
	Type EventMsgKind

	// Raw retains the entire original object for variants this build does not
	// recognize, enabling lossless re-serialization (forward-compat).
	Raw json.RawMessage

	Error                                  *ErrorEvent
	Warning                                *WarningEvent
	GuardianWarning                        *WarningEvent
	RealtimeConversationStarted            *RealtimeConversationStartedEvent
	RealtimeConversationRealtime           *RealtimeConversationRealtimeEvent
	RealtimeConversationClosed             *RealtimeConversationClosedEvent
	RealtimeConversationSdp                *RealtimeConversationSdpEvent
	ModelReroute                           *ModelRerouteEvent
	ModelVerification                      *ModelVerificationEvent
	ContextCompacted                       *ContextCompactedEvent
	ThreadRolledBack                       *ThreadRolledBackEvent
	TurnStarted                            *TurnStartedEvent
	ThreadSettingsApplied                  *ThreadSettingsAppliedEvent
	TurnComplete                           *TurnCompleteEvent
	TokenCount                             *TokenCountEvent
	AgentMessage                           *AgentMessageEvent
	UserMessage                            *UserMessageEvent
	AgentReasoning                         *AgentReasoningEvent
	AgentReasoningRawContent               *AgentReasoningRawContentEvent
	AgentReasoningSectionBreak             *AgentReasoningSectionBreakEvent
	SessionConfigured                      *SessionConfiguredEvent
	ThreadGoalUpdated                      *ThreadGoalUpdatedEvent
	McpStartupUpdate                       *McpStartupUpdateEvent
	McpStartupComplete                     *McpStartupCompleteEvent
	McpToolCallBegin                       *McpToolCallBeginEvent
	McpToolCallEnd                         *McpToolCallEndEvent
	WebSearchBegin                         *WebSearchBeginEvent
	WebSearchEnd                           *WebSearchEndEvent
	ImageGenerationBegin                   *ImageGenerationBeginEvent
	ImageGenerationEnd                     *ImageGenerationEndEvent
	ExecCommandBegin                       *ExecCommandBeginEvent
	ExecCommandOutputDelta                 *ExecCommandOutputDeltaEvent
	TerminalInteraction                    *TerminalInteractionEvent
	ExecCommandEnd                         *ExecCommandEndEvent
	ViewImageToolCall                      *ViewImageToolCallEvent
	ExecApprovalRequest                    *ExecApprovalRequestEvent
	RequestPermissions                     *RequestPermissionsEvent
	RequestUserInput                       *RequestUserInputEvent
	DynamicToolCallRequest                 *DynamicToolCallRequest
	DynamicToolCallResponse                *DynamicToolCallResponseEvent
	ElicitationRequest                     *ElicitationRequestEvent
	ApplyPatchApprovalRequest              *ApplyPatchApprovalRequestEvent
	GuardianAssessment                     *GuardianAssessmentEvent
	DeprecationNotice                      *DeprecationNoticeEvent
	StreamError                            *StreamErrorEvent
	PatchApplyBegin                        *PatchApplyBeginEvent
	PatchApplyUpdated                      *PatchApplyUpdatedEvent
	PatchApplyEnd                          *PatchApplyEndEvent
	TurnDiff                               *TurnDiffEvent
	RealtimeConversationListVoicesResponse *RealtimeConversationListVoicesResponseEvent
	PlanUpdate                             *UpdatePlanArgs
	TurnAborted                            *TurnAbortedEvent
	EnteredReviewMode                      *ReviewRequest
	ExitedReviewMode                       *ExitedReviewModeEvent
	RawResponseItem                        *RawResponseItemEvent
	ItemStarted                            *ItemStartedEvent
	ItemCompleted                          *ItemCompletedEvent
	HookStarted                            *HookStartedEvent
	HookCompleted                          *HookCompletedEvent
	AgentMessageContentDelta               *AgentMessageContentDeltaEvent
	PlanDelta                              *PlanDeltaEvent
	ReasoningContentDelta                  *ReasoningContentDeltaEvent
	ReasoningRawContentDelta               *ReasoningRawContentDeltaEvent
	CollabAgentSpawnBegin                  *CollabAgentSpawnBeginEvent
	CollabAgentSpawnEnd                    *CollabAgentSpawnEndEvent
	CollabAgentInteractionBegin            *CollabAgentInteractionBeginEvent
	CollabAgentInteractionEnd              *CollabAgentInteractionEndEvent
	CollabWaitingBegin                     *CollabWaitingBeginEvent
	CollabWaitingEnd                       *CollabWaitingEndEvent
	CollabCloseBegin                       *CollabCloseBeginEvent
	CollabCloseEnd                         *CollabCloseEndEvent
	CollabResumeBegin                      *CollabResumeBeginEvent
	CollabResumeEnd                        *CollabResumeEndEvent
}

// MarshalJSON emits the active variant's payload merged with the "type" tag,
// using codex's default (v1) dialect for the task_*/turn_* aliases.
func (e EventMsg) MarshalJSON() ([]byte, error) {
	// Payload-less variant: just the tag.
	if e.Type == EventMsgKindShutdownComplete {
		return json.Marshal(map[string]EventMsgKind{"type": EventMsgKindShutdownComplete})
	}

	var payload any
	switch e.Type {
	case EventMsgKindError:
		payload = e.Error
	case EventMsgKindWarning:
		payload = e.Warning
	case EventMsgKindGuardianWarning:
		payload = e.GuardianWarning
	case EventMsgKindRealtimeConversationStarted:
		payload = e.RealtimeConversationStarted
	case EventMsgKindRealtimeConversationRealtime:
		payload = e.RealtimeConversationRealtime
	case EventMsgKindRealtimeConversationClosed:
		payload = e.RealtimeConversationClosed
	case EventMsgKindRealtimeConversationSdp:
		payload = e.RealtimeConversationSdp
	case EventMsgKindModelReroute:
		payload = e.ModelReroute
	case EventMsgKindModelVerification:
		payload = e.ModelVerification
	case EventMsgKindContextCompacted:
		payload = e.ContextCompacted
	case EventMsgKindThreadRolledBack:
		payload = e.ThreadRolledBack
	case EventMsgKindTurnStarted:
		payload = e.TurnStarted
	case EventMsgKindThreadSettingsApplied:
		payload = e.ThreadSettingsApplied
	case EventMsgKindTurnComplete:
		payload = e.TurnComplete
	case EventMsgKindTokenCount:
		payload = e.TokenCount
	case EventMsgKindAgentMessage:
		payload = e.AgentMessage
	case EventMsgKindUserMessage:
		payload = e.UserMessage
	case EventMsgKindAgentReasoning:
		payload = e.AgentReasoning
	case EventMsgKindAgentReasoningRawContent:
		payload = e.AgentReasoningRawContent
	case EventMsgKindAgentReasoningSectionBreak:
		payload = e.AgentReasoningSectionBreak
	case EventMsgKindSessionConfigured:
		payload = e.SessionConfigured
	case EventMsgKindThreadGoalUpdated:
		payload = e.ThreadGoalUpdated
	case EventMsgKindMcpStartupUpdate:
		payload = e.McpStartupUpdate
	case EventMsgKindMcpStartupComplete:
		payload = e.McpStartupComplete
	case EventMsgKindMcpToolCallBegin:
		payload = e.McpToolCallBegin
	case EventMsgKindMcpToolCallEnd:
		payload = e.McpToolCallEnd
	case EventMsgKindWebSearchBegin:
		payload = e.WebSearchBegin
	case EventMsgKindWebSearchEnd:
		payload = e.WebSearchEnd
	case EventMsgKindImageGenerationBegin:
		payload = e.ImageGenerationBegin
	case EventMsgKindImageGenerationEnd:
		payload = e.ImageGenerationEnd
	case EventMsgKindExecCommandBegin:
		payload = e.ExecCommandBegin
	case EventMsgKindExecCommandOutputDelta:
		payload = e.ExecCommandOutputDelta
	case EventMsgKindTerminalInteraction:
		payload = e.TerminalInteraction
	case EventMsgKindExecCommandEnd:
		payload = e.ExecCommandEnd
	case EventMsgKindViewImageToolCall:
		payload = e.ViewImageToolCall
	case EventMsgKindExecApprovalRequest:
		payload = e.ExecApprovalRequest
	case EventMsgKindRequestPermissions:
		payload = e.RequestPermissions
	case EventMsgKindRequestUserInput:
		payload = e.RequestUserInput
	case EventMsgKindDynamicToolCallRequest:
		payload = e.DynamicToolCallRequest
	case EventMsgKindDynamicToolCallResponse:
		payload = e.DynamicToolCallResponse
	case EventMsgKindElicitationRequest:
		payload = e.ElicitationRequest
	case EventMsgKindApplyPatchApprovalRequest:
		payload = e.ApplyPatchApprovalRequest
	case EventMsgKindGuardianAssessment:
		payload = e.GuardianAssessment
	case EventMsgKindDeprecationNotice:
		payload = e.DeprecationNotice
	case EventMsgKindStreamError:
		payload = e.StreamError
	case EventMsgKindPatchApplyBegin:
		payload = e.PatchApplyBegin
	case EventMsgKindPatchApplyUpdated:
		payload = e.PatchApplyUpdated
	case EventMsgKindPatchApplyEnd:
		payload = e.PatchApplyEnd
	case EventMsgKindTurnDiff:
		payload = e.TurnDiff
	case EventMsgKindRealtimeConversationListVoicesResponse:
		payload = e.RealtimeConversationListVoicesResponse
	case EventMsgKindPlanUpdate:
		payload = e.PlanUpdate
	case EventMsgKindTurnAborted:
		payload = e.TurnAborted
	case EventMsgKindEnteredReviewMode:
		payload = e.EnteredReviewMode
	case EventMsgKindExitedReviewMode:
		payload = e.ExitedReviewMode
	case EventMsgKindRawResponseItem:
		payload = e.RawResponseItem
	case EventMsgKindItemStarted:
		payload = e.ItemStarted
	case EventMsgKindItemCompleted:
		payload = e.ItemCompleted
	case EventMsgKindHookStarted:
		payload = e.HookStarted
	case EventMsgKindHookCompleted:
		payload = e.HookCompleted
	case EventMsgKindAgentMessageContentDelta:
		payload = e.AgentMessageContentDelta
	case EventMsgKindPlanDelta:
		payload = e.PlanDelta
	case EventMsgKindReasoningContentDelta:
		payload = e.ReasoningContentDelta
	case EventMsgKindReasoningRawContentDelta:
		payload = e.ReasoningRawContentDelta
	case EventMsgKindCollabAgentSpawnBegin:
		payload = e.CollabAgentSpawnBegin
	case EventMsgKindCollabAgentSpawnEnd:
		payload = e.CollabAgentSpawnEnd
	case EventMsgKindCollabAgentInteractionBegin:
		payload = e.CollabAgentInteractionBegin
	case EventMsgKindCollabAgentInteractionEnd:
		payload = e.CollabAgentInteractionEnd
	case EventMsgKindCollabWaitingBegin:
		payload = e.CollabWaitingBegin
	case EventMsgKindCollabWaitingEnd:
		payload = e.CollabWaitingEnd
	case EventMsgKindCollabCloseBegin:
		payload = e.CollabCloseBegin
	case EventMsgKindCollabCloseEnd:
		payload = e.CollabCloseEnd
	case EventMsgKindCollabResumeBegin:
		payload = e.CollabResumeBegin
	case EventMsgKindCollabResumeEnd:
		payload = e.CollabResumeEnd
	default:
		// Unknown variant: re-emit the retained raw object verbatim.
		if len(e.Raw) > 0 {
			return e.Raw, nil
		}
		return nil, fmt.Errorf("unknown EventMsg type: %q", e.Type)
	}
	return mergeTaggedObject(string(e.Type), payload)
}

// UnmarshalJSON decodes the internally-tagged EventMsg, accepting both the v1
// (task_*) and v2 (turn_*) wire aliases for the started/complete variants and
// tolerating unknown discriminators.
func (e *EventMsg) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type EventMsgKind `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}

	// Normalize v2 aliases to the canonical (emitted) discriminator.
	kind := probe.Type
	switch kind {
	case eventMsgAliasTurnStarted:
		kind = EventMsgKindTurnStarted
	case eventMsgAliasTurnComplete:
		kind = EventMsgKindTurnComplete
	}

	out := EventMsg{Type: kind}
	var err error
	switch kind {
	case EventMsgKindShutdownComplete:
		// payload-less; nothing to decode.
	case EventMsgKindError:
		out.Error = new(ErrorEvent)
		err = json.Unmarshal(data, out.Error)
	case EventMsgKindWarning:
		out.Warning = new(WarningEvent)
		err = json.Unmarshal(data, out.Warning)
	case EventMsgKindGuardianWarning:
		out.GuardianWarning = new(WarningEvent)
		err = json.Unmarshal(data, out.GuardianWarning)
	case EventMsgKindRealtimeConversationStarted:
		out.RealtimeConversationStarted = new(RealtimeConversationStartedEvent)
		err = json.Unmarshal(data, out.RealtimeConversationStarted)
	case EventMsgKindRealtimeConversationRealtime:
		out.RealtimeConversationRealtime = new(RealtimeConversationRealtimeEvent)
		err = json.Unmarshal(data, out.RealtimeConversationRealtime)
	case EventMsgKindRealtimeConversationClosed:
		out.RealtimeConversationClosed = new(RealtimeConversationClosedEvent)
		err = json.Unmarshal(data, out.RealtimeConversationClosed)
	case EventMsgKindRealtimeConversationSdp:
		out.RealtimeConversationSdp = new(RealtimeConversationSdpEvent)
		err = json.Unmarshal(data, out.RealtimeConversationSdp)
	case EventMsgKindModelReroute:
		out.ModelReroute = new(ModelRerouteEvent)
		err = json.Unmarshal(data, out.ModelReroute)
	case EventMsgKindModelVerification:
		out.ModelVerification = new(ModelVerificationEvent)
		err = json.Unmarshal(data, out.ModelVerification)
	case EventMsgKindContextCompacted:
		out.ContextCompacted = new(ContextCompactedEvent)
		err = json.Unmarshal(data, out.ContextCompacted)
	case EventMsgKindThreadRolledBack:
		out.ThreadRolledBack = new(ThreadRolledBackEvent)
		err = json.Unmarshal(data, out.ThreadRolledBack)
	case EventMsgKindTurnStarted:
		out.TurnStarted = new(TurnStartedEvent)
		err = json.Unmarshal(data, out.TurnStarted)
	case EventMsgKindThreadSettingsApplied:
		out.ThreadSettingsApplied = new(ThreadSettingsAppliedEvent)
		err = json.Unmarshal(data, out.ThreadSettingsApplied)
	case EventMsgKindTurnComplete:
		out.TurnComplete = new(TurnCompleteEvent)
		err = json.Unmarshal(data, out.TurnComplete)
	case EventMsgKindTokenCount:
		out.TokenCount = new(TokenCountEvent)
		err = json.Unmarshal(data, out.TokenCount)
	case EventMsgKindAgentMessage:
		out.AgentMessage = new(AgentMessageEvent)
		err = json.Unmarshal(data, out.AgentMessage)
	case EventMsgKindUserMessage:
		out.UserMessage = new(UserMessageEvent)
		err = json.Unmarshal(data, out.UserMessage)
	case EventMsgKindAgentReasoning:
		out.AgentReasoning = new(AgentReasoningEvent)
		err = json.Unmarshal(data, out.AgentReasoning)
	case EventMsgKindAgentReasoningRawContent:
		out.AgentReasoningRawContent = new(AgentReasoningRawContentEvent)
		err = json.Unmarshal(data, out.AgentReasoningRawContent)
	case EventMsgKindAgentReasoningSectionBreak:
		out.AgentReasoningSectionBreak = new(AgentReasoningSectionBreakEvent)
		err = json.Unmarshal(data, out.AgentReasoningSectionBreak)
	case EventMsgKindSessionConfigured:
		out.SessionConfigured = new(SessionConfiguredEvent)
		err = json.Unmarshal(data, out.SessionConfigured)
	case EventMsgKindThreadGoalUpdated:
		out.ThreadGoalUpdated = new(ThreadGoalUpdatedEvent)
		err = json.Unmarshal(data, out.ThreadGoalUpdated)
	case EventMsgKindMcpStartupUpdate:
		out.McpStartupUpdate = new(McpStartupUpdateEvent)
		err = json.Unmarshal(data, out.McpStartupUpdate)
	case EventMsgKindMcpStartupComplete:
		out.McpStartupComplete = new(McpStartupCompleteEvent)
		err = json.Unmarshal(data, out.McpStartupComplete)
	case EventMsgKindMcpToolCallBegin:
		out.McpToolCallBegin = new(McpToolCallBeginEvent)
		err = json.Unmarshal(data, out.McpToolCallBegin)
	case EventMsgKindMcpToolCallEnd:
		out.McpToolCallEnd = new(McpToolCallEndEvent)
		err = json.Unmarshal(data, out.McpToolCallEnd)
	case EventMsgKindWebSearchBegin:
		out.WebSearchBegin = new(WebSearchBeginEvent)
		err = json.Unmarshal(data, out.WebSearchBegin)
	case EventMsgKindWebSearchEnd:
		out.WebSearchEnd = new(WebSearchEndEvent)
		err = json.Unmarshal(data, out.WebSearchEnd)
	case EventMsgKindImageGenerationBegin:
		out.ImageGenerationBegin = new(ImageGenerationBeginEvent)
		err = json.Unmarshal(data, out.ImageGenerationBegin)
	case EventMsgKindImageGenerationEnd:
		out.ImageGenerationEnd = new(ImageGenerationEndEvent)
		err = json.Unmarshal(data, out.ImageGenerationEnd)
	case EventMsgKindExecCommandBegin:
		out.ExecCommandBegin = new(ExecCommandBeginEvent)
		err = json.Unmarshal(data, out.ExecCommandBegin)
	case EventMsgKindExecCommandOutputDelta:
		out.ExecCommandOutputDelta = new(ExecCommandOutputDeltaEvent)
		err = json.Unmarshal(data, out.ExecCommandOutputDelta)
	case EventMsgKindTerminalInteraction:
		out.TerminalInteraction = new(TerminalInteractionEvent)
		err = json.Unmarshal(data, out.TerminalInteraction)
	case EventMsgKindExecCommandEnd:
		out.ExecCommandEnd = new(ExecCommandEndEvent)
		err = json.Unmarshal(data, out.ExecCommandEnd)
	case EventMsgKindViewImageToolCall:
		out.ViewImageToolCall = new(ViewImageToolCallEvent)
		err = json.Unmarshal(data, out.ViewImageToolCall)
	case EventMsgKindExecApprovalRequest:
		out.ExecApprovalRequest = new(ExecApprovalRequestEvent)
		err = json.Unmarshal(data, out.ExecApprovalRequest)
	case EventMsgKindRequestPermissions:
		out.RequestPermissions = new(RequestPermissionsEvent)
		err = json.Unmarshal(data, out.RequestPermissions)
	case EventMsgKindRequestUserInput:
		out.RequestUserInput = new(RequestUserInputEvent)
		err = json.Unmarshal(data, out.RequestUserInput)
	case EventMsgKindDynamicToolCallRequest:
		out.DynamicToolCallRequest = new(DynamicToolCallRequest)
		err = json.Unmarshal(data, out.DynamicToolCallRequest)
	case EventMsgKindDynamicToolCallResponse:
		out.DynamicToolCallResponse = new(DynamicToolCallResponseEvent)
		err = json.Unmarshal(data, out.DynamicToolCallResponse)
	case EventMsgKindElicitationRequest:
		out.ElicitationRequest = new(ElicitationRequestEvent)
		err = json.Unmarshal(data, out.ElicitationRequest)
	case EventMsgKindApplyPatchApprovalRequest:
		out.ApplyPatchApprovalRequest = new(ApplyPatchApprovalRequestEvent)
		err = json.Unmarshal(data, out.ApplyPatchApprovalRequest)
	case EventMsgKindGuardianAssessment:
		out.GuardianAssessment = new(GuardianAssessmentEvent)
		err = json.Unmarshal(data, out.GuardianAssessment)
	case EventMsgKindDeprecationNotice:
		out.DeprecationNotice = new(DeprecationNoticeEvent)
		err = json.Unmarshal(data, out.DeprecationNotice)
	case EventMsgKindStreamError:
		out.StreamError = new(StreamErrorEvent)
		err = json.Unmarshal(data, out.StreamError)
	case EventMsgKindPatchApplyBegin:
		out.PatchApplyBegin = new(PatchApplyBeginEvent)
		err = json.Unmarshal(data, out.PatchApplyBegin)
	case EventMsgKindPatchApplyUpdated:
		out.PatchApplyUpdated = new(PatchApplyUpdatedEvent)
		err = json.Unmarshal(data, out.PatchApplyUpdated)
	case EventMsgKindPatchApplyEnd:
		out.PatchApplyEnd = new(PatchApplyEndEvent)
		err = json.Unmarshal(data, out.PatchApplyEnd)
	case EventMsgKindTurnDiff:
		out.TurnDiff = new(TurnDiffEvent)
		err = json.Unmarshal(data, out.TurnDiff)
	case EventMsgKindRealtimeConversationListVoicesResponse:
		out.RealtimeConversationListVoicesResponse = new(RealtimeConversationListVoicesResponseEvent)
		err = json.Unmarshal(data, out.RealtimeConversationListVoicesResponse)
	case EventMsgKindPlanUpdate:
		out.PlanUpdate = new(UpdatePlanArgs)
		err = json.Unmarshal(data, out.PlanUpdate)
	case EventMsgKindTurnAborted:
		out.TurnAborted = new(TurnAbortedEvent)
		err = json.Unmarshal(data, out.TurnAborted)
	case EventMsgKindEnteredReviewMode:
		out.EnteredReviewMode = new(ReviewRequest)
		err = json.Unmarshal(data, out.EnteredReviewMode)
	case EventMsgKindExitedReviewMode:
		out.ExitedReviewMode = new(ExitedReviewModeEvent)
		err = json.Unmarshal(data, out.ExitedReviewMode)
	case EventMsgKindRawResponseItem:
		out.RawResponseItem = new(RawResponseItemEvent)
		err = json.Unmarshal(data, out.RawResponseItem)
	case EventMsgKindItemStarted:
		out.ItemStarted = new(ItemStartedEvent)
		err = json.Unmarshal(data, out.ItemStarted)
	case EventMsgKindItemCompleted:
		out.ItemCompleted = new(ItemCompletedEvent)
		err = json.Unmarshal(data, out.ItemCompleted)
	case EventMsgKindHookStarted:
		out.HookStarted = new(HookStartedEvent)
		err = json.Unmarshal(data, out.HookStarted)
	case EventMsgKindHookCompleted:
		out.HookCompleted = new(HookCompletedEvent)
		err = json.Unmarshal(data, out.HookCompleted)
	case EventMsgKindAgentMessageContentDelta:
		out.AgentMessageContentDelta = new(AgentMessageContentDeltaEvent)
		err = json.Unmarshal(data, out.AgentMessageContentDelta)
	case EventMsgKindPlanDelta:
		out.PlanDelta = new(PlanDeltaEvent)
		err = json.Unmarshal(data, out.PlanDelta)
	case EventMsgKindReasoningContentDelta:
		out.ReasoningContentDelta = new(ReasoningContentDeltaEvent)
		err = json.Unmarshal(data, out.ReasoningContentDelta)
	case EventMsgKindReasoningRawContentDelta:
		out.ReasoningRawContentDelta = new(ReasoningRawContentDeltaEvent)
		err = json.Unmarshal(data, out.ReasoningRawContentDelta)
	case EventMsgKindCollabAgentSpawnBegin:
		out.CollabAgentSpawnBegin = new(CollabAgentSpawnBeginEvent)
		err = json.Unmarshal(data, out.CollabAgentSpawnBegin)
	case EventMsgKindCollabAgentSpawnEnd:
		out.CollabAgentSpawnEnd = new(CollabAgentSpawnEndEvent)
		err = json.Unmarshal(data, out.CollabAgentSpawnEnd)
	case EventMsgKindCollabAgentInteractionBegin:
		out.CollabAgentInteractionBegin = new(CollabAgentInteractionBeginEvent)
		err = json.Unmarshal(data, out.CollabAgentInteractionBegin)
	case EventMsgKindCollabAgentInteractionEnd:
		out.CollabAgentInteractionEnd = new(CollabAgentInteractionEndEvent)
		err = json.Unmarshal(data, out.CollabAgentInteractionEnd)
	case EventMsgKindCollabWaitingBegin:
		out.CollabWaitingBegin = new(CollabWaitingBeginEvent)
		err = json.Unmarshal(data, out.CollabWaitingBegin)
	case EventMsgKindCollabWaitingEnd:
		out.CollabWaitingEnd = new(CollabWaitingEndEvent)
		err = json.Unmarshal(data, out.CollabWaitingEnd)
	case EventMsgKindCollabCloseBegin:
		out.CollabCloseBegin = new(CollabCloseBeginEvent)
		err = json.Unmarshal(data, out.CollabCloseBegin)
	case EventMsgKindCollabCloseEnd:
		out.CollabCloseEnd = new(CollabCloseEndEvent)
		err = json.Unmarshal(data, out.CollabCloseEnd)
	case EventMsgKindCollabResumeBegin:
		out.CollabResumeBegin = new(CollabResumeBeginEvent)
		err = json.Unmarshal(data, out.CollabResumeBegin)
	case EventMsgKindCollabResumeEnd:
		out.CollabResumeEnd = new(CollabResumeEndEvent)
		err = json.Unmarshal(data, out.CollabResumeEnd)
	default:
		// Forward-compat: retain the raw object so it can be re-emitted.
		out.Raw = append(json.RawMessage(nil), data...)
	}
	if err != nil {
		return err
	}
	*e = out
	return nil
}

// ---------------------------------------------------------------------------
// EventMsg payload structs.
// ---------------------------------------------------------------------------

// CodexErrorInfoKind is the discriminator for CodexErrorInfo. Rust:
// snake_case (an internally-tagged-by-default enum: serde's externally-tagged
// representation since there is no `tag` attribute). Variants with payloads
// are externally tagged: `{"http_connection_failed": {...}}`.
type CodexErrorInfoKind string

const (
	CodexErrorInfoContextWindowExceeded         CodexErrorInfoKind = "context_window_exceeded"
	CodexErrorInfoUsageLimitExceeded            CodexErrorInfoKind = "usage_limit_exceeded"
	CodexErrorInfoServerOverloaded              CodexErrorInfoKind = "server_overloaded"
	CodexErrorInfoCyberPolicy                   CodexErrorInfoKind = "cyber_policy"
	CodexErrorInfoHTTPConnectionFailed          CodexErrorInfoKind = "http_connection_failed"
	CodexErrorInfoResponseStreamConnectionFail  CodexErrorInfoKind = "response_stream_connection_failed"
	CodexErrorInfoInternalServerError           CodexErrorInfoKind = "internal_server_error"
	CodexErrorInfoUnauthorized                  CodexErrorInfoKind = "unauthorized"
	CodexErrorInfoBadRequest                    CodexErrorInfoKind = "bad_request"
	CodexErrorInfoSandboxError                  CodexErrorInfoKind = "sandbox_error"
	CodexErrorInfoResponseStreamDisconnected    CodexErrorInfoKind = "response_stream_disconnected"
	CodexErrorInfoResponseTooManyFailedAttempts CodexErrorInfoKind = "response_too_many_failed_attempts"
	CodexErrorInfoActiveTurnNotSteerable        CodexErrorInfoKind = "active_turn_not_steerable"
	CodexErrorInfoThreadRollbackFailed          CodexErrorInfoKind = "thread_rollback_failed"
	CodexErrorInfoOther                         CodexErrorInfoKind = "other"
)

// CodexErrorInfo is a client-facing error classification. Rust serializes it
// as an externally-tagged enum. The unit variants serialize as bare strings
// (e.g. "context_window_exceeded"); the struct variants serialize as
// single-key objects (e.g. {"http_connection_failed":{"http_status_code":...}}).
// We retain the raw encoding to round-trip losslessly and tolerate unknown
// variants for forward-compatibility.
type CodexErrorInfo struct {
	Kind CodexErrorInfoKind
	// Raw retains the original JSON encoding (string or single-key object).
	Raw json.RawMessage
}

// MarshalJSON re-emits the retained raw encoding.
func (c CodexErrorInfo) MarshalJSON() ([]byte, error) {
	if len(c.Raw) > 0 {
		return c.Raw, nil
	}
	// No raw retained: emit as a bare string from Kind.
	return json.Marshal(string(c.Kind))
}

// UnmarshalJSON decodes either a bare string (unit variant) or a single-key
// object (struct variant), recording the discriminator and retaining the raw
// bytes.
func (c *CodexErrorInfo) UnmarshalJSON(data []byte) error {
	c.Raw = append(json.RawMessage(nil), data...)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.Kind = CodexErrorInfoKind(s)
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	for k := range m {
		c.Kind = CodexErrorInfoKind(k)
		return nil
	}
	return nil
}

// ErrorEvent reports an error while executing a submission.
type ErrorEvent struct {
	Message        string          `json:"message"`
	CodexErrorInfo *CodexErrorInfo `json:"codex_error_info"`
}

// WarningEvent reports a non-fatal warning. Also used by GuardianWarning.
type WarningEvent struct {
	Message string `json:"message"`
}

// ModelRerouteReason enumerates why a turn was rerouted to a different model.
type ModelRerouteReason string

const (
	ModelRerouteReasonHighRiskCyberActivity ModelRerouteReason = "high_risk_cyber_activity"
)

// ModelRerouteEvent reports that the requested model was rerouted.
type ModelRerouteEvent struct {
	FromModel string             `json:"from_model"`
	ToModel   string             `json:"to_model"`
	Reason    ModelRerouteReason `json:"reason"`
}

// ModelVerification enumerates additional verification requirements.
type ModelVerification string

const (
	ModelVerificationTrustedAccessForCyber ModelVerification = "trusted_access_for_cyber"
)

// ModelVerificationEvent recommends additional account verification.
type ModelVerificationEvent struct {
	Verifications []ModelVerification `json:"verifications"`
}

// ContextCompactedEvent marks that history was compacted. Rust: a unit struct,
// which serde serializes as `null`.
type ContextCompactedEvent struct{}

// MarshalJSON emits `null` to match the Rust unit-struct encoding.
func (ContextCompactedEvent) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}

// UnmarshalJSON accepts `null` (or any value) for the unit struct.
func (*ContextCompactedEvent) UnmarshalJSON([]byte) error { return nil }

// TurnCompleteEvent signals the agent completed all actions for a turn.
// Wire discriminator: "task_complete" (alias "turn_complete").
type TurnCompleteEvent struct {
	TurnID           string  `json:"turn_id"`
	LastAgentMessage *string `json:"last_agent_message"`
	// CompletedAt is the unix timestamp (seconds) when the turn completed.
	CompletedAt *int64 `json:"completed_at,omitempty"`
	// DurationMs is the turn duration in milliseconds, if known.
	DurationMs *int64 `json:"duration_ms,omitempty"`
	// TimeToFirstTokenMs is the time-to-first-token in milliseconds, if known.
	TimeToFirstTokenMs *int64 `json:"time_to_first_token_ms,omitempty"`
}

// TurnStartedEvent signals the agent started a turn.
// Wire discriminator: "task_started" (alias "turn_started").
//
// CollaborationModeKind uses ModeKind with serde `#[serde(default)]`, so it is
// always present on the wire (no omitempty).
type TurnStartedEvent struct {
	TurnID string `json:"turn_id"`
	// TraceID correlates the turn with telemetry traces.
	TraceID *string `json:"trace_id,omitempty"`
	// StartedAt is the unix timestamp (seconds) when the turn started.
	StartedAt *int64 `json:"started_at,omitempty"`
	// ModelContextWindow is the model's context window size, if known.
	ModelContextWindow *int64   `json:"model_context_window"`
	CollaborationMode  ModeKind `json:"collaboration_mode_kind"`
}

// ThreadSettingsAppliedEvent reports applied persistent thread-settings.
type ThreadSettingsAppliedEvent struct {
	ThreadSettings ThreadSettingsSnapshot `json:"thread_settings"`
}

// ThreadSettingsSnapshot captures the effective per-thread configuration.
//
// PermissionProfile / ActivePermissionProfile / ReasoningEffortConfig /
// ReasoningSummaryConfig are owned by sibling config modules; they are carried
// as json.RawMessage to round-trip losslessly until those types are ported.
type ThreadSettingsSnapshot struct {
	Model                   string            `json:"model"`
	ModelProviderID         string            `json:"model_provider_id"`
	ServiceTier             *string           `json:"service_tier,omitempty"`
	ApprovalPolicy          AskForApproval    `json:"approval_policy"`
	ApprovalsReviewer       ApprovalsReviewer `json:"approvals_reviewer"`
	PermissionProfile       json.RawMessage   `json:"permission_profile"`
	ActivePermissionProfile json.RawMessage   `json:"active_permission_profile,omitempty"`
	Cwd                     AbsolutePath      `json:"cwd"`
	ReasoningEffort         json.RawMessage   `json:"reasoning_effort,omitempty"`
	ReasoningSummary        json.RawMessage   `json:"reasoning_summary,omitempty"`
	Personality             *Personality      `json:"personality,omitempty"`
	CollaborationMode       CollaborationMode `json:"collaboration_mode"`
}

// TokenUsage is an element-wise token count. Mirrors Rust `TokenUsage`.
type TokenUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
}

// TokenUsageInfo aggregates total and last-turn token usage for a session.
type TokenUsageInfo struct {
	TotalTokenUsage    TokenUsage `json:"total_token_usage"`
	LastTokenUsage     TokenUsage `json:"last_token_usage"`
	ModelContextWindow *int64     `json:"model_context_window"`
}

// TokenCountEvent reports the latest usage and rate-limit snapshot. Both fields
// are optional; UIs should not display them when null.
type TokenCountEvent struct {
	Info       *TokenUsageInfo    `json:"info"`
	RateLimits *RateLimitSnapshot `json:"rate_limits"`
}

// RateLimitReachedType enumerates the reason a rate limit was reached.
type RateLimitReachedType string

const (
	RateLimitReachedTypeRateLimitReached                 RateLimitReachedType = "rate_limit_reached"
	RateLimitReachedTypeWorkspaceOwnerCreditsDepleted    RateLimitReachedType = "workspace_owner_credits_depleted"
	RateLimitReachedTypeWorkspaceMemberCreditsDepleted   RateLimitReachedType = "workspace_member_credits_depleted"
	RateLimitReachedTypeWorkspaceOwnerUsageLimitReached  RateLimitReachedType = "workspace_owner_usage_limit_reached"
	RateLimitReachedTypeWorkspaceMemberUsageLimitReached RateLimitReachedType = "workspace_member_usage_limit_reached"
)

// RateLimitSnapshot is a point-in-time view of account rate limits.
//
// PlanType is owned by account.go and reused here by bare identifier.
type RateLimitSnapshot struct {
	LimitID              *string               `json:"limit_id"`
	LimitName            *string               `json:"limit_name"`
	Primary              *RateLimitWindow      `json:"primary"`
	Secondary            *RateLimitWindow      `json:"secondary"`
	Credits              *CreditsSnapshot      `json:"credits"`
	PlanType             *PlanType             `json:"plan_type"`
	RateLimitReachedType *RateLimitReachedType `json:"rate_limit_reached_type"`
}

// RateLimitWindow describes one rolling rate-limit window.
type RateLimitWindow struct {
	// UsedPercent is the percentage (0-100) of the window consumed.
	UsedPercent float64 `json:"used_percent"`
	// WindowMinutes is the rolling-window duration in minutes.
	WindowMinutes *int64 `json:"window_minutes"`
	// ResetsAt is the unix timestamp (seconds) when the window resets.
	ResetsAt *int64 `json:"resets_at"`
}

// CreditsSnapshot describes available credits.
type CreditsSnapshot struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

// AgentMessageEvent carries agent text output.
//
// Phase / MemoryCitation use serde `#[serde(default)]`, so they are always
// present on the wire (no omitempty); a missing/null value decodes to nil.
type AgentMessageEvent struct {
	Message        string          `json:"message"`
	Phase          *MessagePhase   `json:"phase"`
	MemoryCitation *MemoryCitation `json:"memory_citation"`
}

// UserMessageEvent carries the user/system input sent to the model.
//
// LocalImages are filesystem paths (Rust PathBuf), modeled as strings.
type UserMessageEvent struct {
	ClientID          *string        `json:"client_id,omitempty"`
	Message           string         `json:"message"`
	Images            *[]string      `json:"images,omitempty"`
	ImageDetails      []*ImageDetail `json:"image_details,omitempty"`
	LocalImages       []string       `json:"local_images"`
	LocalImageDetails []*ImageDetail `json:"local_image_details,omitempty"`
	TextElements      []TextElement  `json:"text_elements"`
}

// AgentReasoningEvent carries a reasoning summary from the agent.
type AgentReasoningEvent struct {
	Text string `json:"text"`
}

// AgentReasoningRawContentEvent carries raw chain-of-thought from the agent.
type AgentReasoningRawContentEvent struct {
	Text string `json:"text"`
}

// AgentReasoningSectionBreakEvent signals a new reasoning summary section.
type AgentReasoningSectionBreakEvent struct {
	ItemID       string `json:"item_id"`
	SummaryIndex int64  `json:"summary_index"`
}

// McpInvocation describes an MCP tool invocation.
type McpInvocation struct {
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

// McpToolCallBeginEvent signals the start of an MCP tool call.
type McpToolCallBeginEvent struct {
	CallID            string        `json:"call_id"`
	Invocation        McpInvocation `json:"invocation"`
	McpAppResourceURI *string       `json:"mcp_app_resource_uri,omitempty"`
	PluginID          *string       `json:"plugin_id,omitempty"`
}

// McpToolCallEndEvent signals the completion of an MCP tool call.
//
// Duration is a Rust std::time::Duration serialized as `{"secs":N,"nanos":N}`;
// kept raw to preserve exact bytes. Result is `Result<CallToolResult, String>`,
// serialized externally tagged as `{"Ok":{...}}` or `{"Err":"..."}`; kept raw.
type McpToolCallEndEvent struct {
	CallID            string          `json:"call_id"`
	Invocation        McpInvocation   `json:"invocation"`
	McpAppResourceURI *string         `json:"mcp_app_resource_uri,omitempty"`
	PluginID          *string         `json:"plugin_id,omitempty"`
	Duration          json.RawMessage `json:"duration"`
	Result            json.RawMessage `json:"result"`
}

// DynamicToolCallResponseEvent reports the result of a dynamic tool call.
//
// Duration is kept raw (std::time::Duration encoding).
type DynamicToolCallResponseEvent struct {
	CallID       string                             `json:"call_id"`
	TurnID       string                             `json:"turn_id"`
	CompletedAt  int64                              `json:"completed_at_ms"`
	Namespace    *string                            `json:"namespace"`
	Tool         string                             `json:"tool"`
	Arguments    json.RawMessage                    `json:"arguments"`
	ContentItems []DynamicToolCallOutputContentItem `json:"content_items"`
	Success      bool                               `json:"success"`
	Error        *string                            `json:"error"`
	Duration     json.RawMessage                    `json:"duration"`
}

// WebSearchBeginEvent signals the start of a web search.
type WebSearchBeginEvent struct {
	CallID string `json:"call_id"`
}

// WebSearchEndEvent signals the completion of a web search.
type WebSearchEndEvent struct {
	CallID string          `json:"call_id"`
	Query  string          `json:"query"`
	Action WebSearchAction `json:"action"`
}

// ImageGenerationBeginEvent signals the start of an image generation.
type ImageGenerationBeginEvent struct {
	CallID string `json:"call_id"`
}

// ImageGenerationEndEvent signals the completion of an image generation.
type ImageGenerationEndEvent struct {
	CallID        string        `json:"call_id"`
	Status        string        `json:"status"`
	RevisedPrompt *string       `json:"revised_prompt,omitempty"`
	Result        string        `json:"result"`
	SavedPath     *AbsolutePath `json:"saved_path,omitempty"`
}

// ExecCommandSource enumerates where an exec command originated.
type ExecCommandSource string

const (
	ExecCommandSourceAgent                  ExecCommandSource = "agent"
	ExecCommandSourceUserShell              ExecCommandSource = "user_shell"
	ExecCommandSourceUnifiedExecStartup     ExecCommandSource = "unified_exec_startup"
	ExecCommandSourceUnifiedExecInteraction ExecCommandSource = "unified_exec_interaction"
)

// ExecCommandStatus enumerates the completion status of an exec command.
type ExecCommandStatus string

const (
	ExecCommandStatusCompleted ExecCommandStatus = "completed"
	ExecCommandStatusFailed    ExecCommandStatus = "failed"
	ExecCommandStatusDeclined  ExecCommandStatus = "declined"
)

// ExecCommandBeginEvent signals the server is about to execute a command.
//
// ParsedCmd elements are ParsedCommand (owned by parse_command.rs); kept raw.
type ExecCommandBeginEvent struct {
	CallID           string            `json:"call_id"`
	ProcessID        *string           `json:"process_id,omitempty"`
	TurnID           string            `json:"turn_id"`
	StartedAt        int64             `json:"started_at_ms"`
	Command          []string          `json:"command"`
	Cwd              AbsolutePath      `json:"cwd"`
	ParsedCmd        []json.RawMessage `json:"parsed_cmd"`
	Source           ExecCommandSource `json:"source"`
	InteractionInput *string           `json:"interaction_input,omitempty"`
}

// ExecCommandEndEvent signals an exec command finished.
//
// Duration is kept raw (std::time::Duration encoding); ParsedCmd elements are
// ParsedCommand kept raw.
type ExecCommandEndEvent struct {
	CallID           string            `json:"call_id"`
	ProcessID        *string           `json:"process_id,omitempty"`
	TurnID           string            `json:"turn_id"`
	CompletedAt      int64             `json:"completed_at_ms"`
	Command          []string          `json:"command"`
	Cwd              AbsolutePath      `json:"cwd"`
	ParsedCmd        []json.RawMessage `json:"parsed_cmd"`
	Source           ExecCommandSource `json:"source"`
	InteractionInput *string           `json:"interaction_input,omitempty"`
	Stdout           string            `json:"stdout"`
	Stderr           string            `json:"stderr"`
	AggregatedOutput string            `json:"aggregated_output"`
	ExitCode         int32             `json:"exit_code"`
	Duration         json.RawMessage   `json:"duration"`
	FormattedOutput  string            `json:"formatted_output"`
	Status           ExecCommandStatus `json:"status"`
}

// ViewImageToolCallEvent reports an image attached via the view_image tool.
type ViewImageToolCallEvent struct {
	CallID string       `json:"call_id"`
	Path   AbsolutePath `json:"path"`
}

// ExecOutputStream enumerates which standard stream produced output.
type ExecOutputStream string

const (
	ExecOutputStreamStdout ExecOutputStream = "stdout"
	ExecOutputStreamStderr ExecOutputStream = "stderr"
)

// ExecCommandOutputDeltaEvent carries an incremental chunk of command output.
//
// Chunk is raw bytes serialized as base64 (serde_with Base64). Go's
// encoding/json encodes/decodes []byte as base64 by default, matching the wire.
type ExecCommandOutputDeltaEvent struct {
	CallID string           `json:"call_id"`
	Stream ExecOutputStream `json:"stream"`
	Chunk  []byte           `json:"chunk"`
}

// TerminalInteractionEvent reports stdin sent to an in-progress command.
type TerminalInteractionEvent struct {
	CallID    string `json:"call_id"`
	ProcessID string `json:"process_id"`
	Stdin     string `json:"stdin"`
}

// DeprecationNoticeEvent advises that something is deprecated.
type DeprecationNoticeEvent struct {
	Summary string  `json:"summary"`
	Details *string `json:"details,omitempty"`
}

// ThreadRolledBackEvent reports that history was rolled back.
type ThreadRolledBackEvent struct {
	NumTurns uint32 `json:"num_turns"`
}

// StreamErrorEvent reports a recoverable model stream error.
type StreamErrorEvent struct {
	Message           string          `json:"message"`
	CodexErrorInfo    *CodexErrorInfo `json:"codex_error_info"`
	AdditionalDetails *string         `json:"additional_details"`
}

// PatchApplyBeginEvent signals the agent is about to apply a code patch.
//
// Changes maps a path (Rust PathBuf) to a FileChange.
type PatchApplyBeginEvent struct {
	CallID       string                `json:"call_id"`
	TurnID       string                `json:"turn_id"`
	AutoApproved bool                  `json:"auto_approved"`
	Changes      map[string]FileChange `json:"changes"`
}

// PatchApplyUpdatedEvent carries the latest structured changes for a patch.
type PatchApplyUpdatedEvent struct {
	CallID  string                `json:"call_id"`
	Changes map[string]FileChange `json:"changes"`
}

// PatchApplyEndEvent signals a patch application finished.
type PatchApplyEndEvent struct {
	CallID  string                `json:"call_id"`
	TurnID  string                `json:"turn_id"`
	Stdout  string                `json:"stdout"`
	Stderr  string                `json:"stderr"`
	Success bool                  `json:"success"`
	Changes map[string]FileChange `json:"changes"`
	Status  PatchApplyStatus      `json:"status"`
}

// FileChangeKind is the discriminator for FileChange. Rust uses
// `#[serde(tag = "type", rename_all = "snake_case")]`.
type FileChangeKind string

const (
	FileChangeKindAdd    FileChangeKind = "add"
	FileChangeKindDelete FileChangeKind = "delete"
	FileChangeKindUpdate FileChangeKind = "update"
)

// FileChange is an internally-tagged (on "type") enum describing a single file
// change in a patch. Exactly one of the variant payloads applies per Kind.
//
// Add/Delete carry `content`; Update carries `unified_diff` and an optional
// `move_path` (Rust PathBuf, modeled as a string; always present on the wire as
// it is `Option<PathBuf>` without skip_serializing_if).
type FileChange struct {
	Kind FileChangeKind

	// Content is set for the Add and Delete variants.
	Content string
	// UnifiedDiff is set for the Update variant.
	UnifiedDiff string
	// MovePath is set for the Update variant (may be nil).
	MovePath *string
}

// MarshalJSON emits the active variant's payload merged with the "type" tag.
func (f FileChange) MarshalJSON() ([]byte, error) {
	switch f.Kind {
	case FileChangeKindAdd, FileChangeKindDelete:
		return json.Marshal(struct {
			Type    FileChangeKind `json:"type"`
			Content string         `json:"content"`
		}{Type: f.Kind, Content: f.Content})
	case FileChangeKindUpdate:
		return json.Marshal(struct {
			Type        FileChangeKind `json:"type"`
			UnifiedDiff string         `json:"unified_diff"`
			MovePath    *string        `json:"move_path"`
		}{Type: f.Kind, UnifiedDiff: f.UnifiedDiff, MovePath: f.MovePath})
	default:
		return nil, fmt.Errorf("unknown FileChange type: %q", f.Kind)
	}
}

// UnmarshalJSON decodes the internally-tagged FileChange.
func (f *FileChange) UnmarshalJSON(data []byte) error {
	var wire struct {
		Type        FileChangeKind `json:"type"`
		Content     string         `json:"content"`
		UnifiedDiff string         `json:"unified_diff"`
		MovePath    *string        `json:"move_path"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*f = FileChange{
		Kind:        wire.Type,
		Content:     wire.Content,
		UnifiedDiff: wire.UnifiedDiff,
		MovePath:    wire.MovePath,
	}
	return nil
}

// PatchApplyStatus enumerates the completion status of a patch application.
type PatchApplyStatus string

const (
	PatchApplyStatusCompleted PatchApplyStatus = "completed"
	PatchApplyStatusFailed    PatchApplyStatus = "failed"
	PatchApplyStatusDeclined  PatchApplyStatus = "declined"
)

// TurnDiffEvent carries the unified diff for a turn.
type TurnDiffEvent struct {
	UnifiedDiff string `json:"unified_diff"`
}

// McpStartupUpdateEvent reports incremental MCP startup progress.
type McpStartupUpdateEvent struct {
	Server string           `json:"server"`
	Status McpStartupStatus `json:"status"`
}

// McpStartupStatusState is the discriminator for McpStartupStatus. Rust uses
// `#[serde(tag = "state")]` with snake_case.
type McpStartupStatusState string

const (
	McpStartupStatusStateStarting  McpStartupStatusState = "starting"
	McpStartupStatusStateReady     McpStartupStatusState = "ready"
	McpStartupStatusStateFailed    McpStartupStatusState = "failed"
	McpStartupStatusStateCancelled McpStartupStatusState = "cancelled"
)

// McpStartupStatus is an internally-tagged (on "state") enum describing an
// MCP server's startup state. The Failed variant carries an `error` field.
type McpStartupStatus struct {
	State McpStartupStatusState
	// Error is populated for the "failed" state.
	Error *string
}

// MarshalJSON emits the state tag, plus `error` for the failed state.
func (s McpStartupStatus) MarshalJSON() ([]byte, error) {
	m := map[string]any{"state": string(s.State)}
	if s.State == McpStartupStatusStateFailed {
		var e string
		if s.Error != nil {
			e = *s.Error
		}
		m["error"] = e
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the state tag and optional error field.
func (s *McpStartupStatus) UnmarshalJSON(data []byte) error {
	var wire struct {
		State McpStartupStatusState `json:"state"`
		Error *string               `json:"error"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	s.State = wire.State
	s.Error = wire.Error
	return nil
}

// McpStartupCompleteEvent summarizes MCP startup completion.
type McpStartupCompleteEvent struct {
	Ready     []string            `json:"ready"`
	Failed    []McpStartupFailure `json:"failed"`
	Cancelled []string            `json:"cancelled"`
}

// McpStartupFailure records a single failed MCP server startup.
type McpStartupFailure struct {
	Server string `json:"server"`
	Error  string `json:"error"`
}

// RealtimeConversationVersion enumerates the realtime conversation protocol
// version.
type RealtimeConversationVersion string

const (
	RealtimeConversationVersionV1 RealtimeConversationVersion = "V1"
	RealtimeConversationVersionV2 RealtimeConversationVersion = "V2"
)

// RealtimeConversationStartedEvent reports a realtime conversation start.
type RealtimeConversationStartedEvent struct {
	RealtimeSessionID *string                     `json:"realtime_session_id"`
	Version           RealtimeConversationVersion `json:"version"`
}

// RealtimeConversationRealtimeEvent carries a realtime streaming payload.
//
// Payload is a RealtimeEvent (owned by realtime module); kept raw.
type RealtimeConversationRealtimeEvent struct {
	Payload json.RawMessage `json:"payload"`
}

// RealtimeConversationClosedEvent reports a realtime conversation close.
type RealtimeConversationClosedEvent struct {
	Reason *string `json:"reason,omitempty"`
}

// RealtimeConversationSdpEvent carries a realtime SDP payload.
type RealtimeConversationSdpEvent struct {
	SDP string `json:"sdp"`
}

// RealtimeConversationListVoicesResponseEvent lists supported realtime voices.
//
// Voices is a RealtimeVoicesList (owned by realtime module); kept raw.
type RealtimeConversationListVoicesResponseEvent struct {
	Voices json.RawMessage `json:"voices"`
}

// SessionConfiguredEvent acks the client's configure message.
//
// PermissionProfile / ActivePermissionProfile / ReasoningEffortConfig are
// owned by sibling config modules; kept raw. RolloutPath is a Rust PathBuf,
// modeled as a string. ThreadSource is owned by a sibling module and kept raw.
// On input, codex also accepts a legacy `sandbox_policy` in lieu of
// `permission_profile`; that legacy projection is performed by the engine, not
// here. This struct round-trips the canonical shape.
type SessionConfiguredEvent struct {
	SessionID               SessionID                   `json:"session_id"`
	ThreadID                ThreadID                    `json:"thread_id"`
	ForkedFromID            *ThreadID                   `json:"forked_from_id,omitempty"`
	ThreadSource            json.RawMessage             `json:"thread_source,omitempty"`
	ThreadName              *string                     `json:"thread_name,omitempty"`
	Model                   string                      `json:"model"`
	ModelProviderID         string                      `json:"model_provider_id"`
	ServiceTier             *string                     `json:"service_tier,omitempty"`
	ApprovalPolicy          AskForApproval              `json:"approval_policy"`
	ApprovalsReviewer       ApprovalsReviewer           `json:"approvals_reviewer"`
	PermissionProfile       json.RawMessage             `json:"permission_profile"`
	ActivePermissionProfile json.RawMessage             `json:"active_permission_profile,omitempty"`
	Cwd                     AbsolutePath                `json:"cwd"`
	ReasoningEffort         json.RawMessage             `json:"reasoning_effort,omitempty"`
	InitialMessages         *[]EventMsg                 `json:"initial_messages,omitempty"`
	NetworkProxy            *SessionNetworkProxyRuntime `json:"network_proxy,omitempty"`
	RolloutPath             *string                     `json:"rollout_path,omitempty"`
}

// SessionNetworkProxyRuntime carries managed proxy bind addresses.
type SessionNetworkProxyRuntime struct {
	HTTPAddr  string `json:"http_addr"`
	SocksAddr string `json:"socks_addr"`
}

// ThreadGoalStatus enumerates the status of a long-running thread goal. Rust:
// camelCase.
type ThreadGoalStatus string

const (
	ThreadGoalStatusActive        ThreadGoalStatus = "active"
	ThreadGoalStatusPaused        ThreadGoalStatus = "paused"
	ThreadGoalStatusBlocked       ThreadGoalStatus = "blocked"
	ThreadGoalStatusUsageLimited  ThreadGoalStatus = "usageLimited"
	ThreadGoalStatusBudgetLimited ThreadGoalStatus = "budgetLimited"
	ThreadGoalStatusComplete      ThreadGoalStatus = "complete"
)

// ThreadGoal describes a long-running goal for the thread. Rust: camelCase.
type ThreadGoal struct {
	ThreadID        ThreadID         `json:"threadId"`
	Objective       string           `json:"objective"`
	Status          ThreadGoalStatus `json:"status"`
	TokenBudget     *int64           `json:"tokenBudget,omitempty"`
	TokensUsed      int64            `json:"tokensUsed"`
	TimeUsedSeconds int64            `json:"timeUsedSeconds"`
	CreatedAt       int64            `json:"createdAt"`
	UpdatedAt       int64            `json:"updatedAt"`
}

// ThreadGoalUpdatedEvent reports updated goal metadata. Rust: camelCase.
type ThreadGoalUpdatedEvent struct {
	ThreadID ThreadID   `json:"threadId"`
	TurnID   *string    `json:"turnId,omitempty"`
	Goal     ThreadGoal `json:"goal"`
}

// TurnAbortReason enumerates why a turn was aborted.
type TurnAbortReason string

const (
	TurnAbortReasonInterrupted   TurnAbortReason = "interrupted"
	TurnAbortReasonReplaced      TurnAbortReason = "replaced"
	TurnAbortReasonReviewEnded   TurnAbortReason = "review_ended"
	TurnAbortReasonBudgetLimited TurnAbortReason = "budget_limited"
)

// TurnAbortedEvent reports that a turn was aborted.
type TurnAbortedEvent struct {
	TurnID      *string         `json:"turn_id"`
	Reason      TurnAbortReason `json:"reason"`
	CompletedAt *int64          `json:"completed_at,omitempty"`
	DurationMs  *int64          `json:"duration_ms,omitempty"`
}

// ExitedReviewModeEvent carries an optional final review result.
type ExitedReviewModeEvent struct {
	ReviewOutput *ReviewOutputEvent `json:"review_output"`
}

// ReviewOutputEvent is the structured result of a child review session.
type ReviewOutputEvent struct {
	Findings               []ReviewFinding `json:"findings"`
	OverallCorrectness     string          `json:"overall_correctness"`
	OverallExplanation     string          `json:"overall_explanation"`
	OverallConfidenceScore float32         `json:"overall_confidence_score"`
}

// ReviewFinding describes a single observed issue or recommendation.
type ReviewFinding struct {
	Title           string             `json:"title"`
	Body            string             `json:"body"`
	ConfidenceScore float32            `json:"confidence_score"`
	Priority        int32              `json:"priority"`
	CodeLocation    ReviewCodeLocation `json:"code_location"`
}

// ReviewCodeLocation locates the code related to a review finding. The file
// path is a Rust PathBuf, modeled as a string.
type ReviewCodeLocation struct {
	AbsoluteFilePath string          `json:"absolute_file_path"`
	LineRange        ReviewLineRange `json:"line_range"`
}

// ReviewLineRange is an inclusive line range associated with a finding.
type ReviewLineRange struct {
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

// RawResponseItemEvent wraps a raw ResponseItem.
type RawResponseItemEvent struct {
	Item ResponseItem `json:"item"`
}

// ItemStartedEvent signals the start of a turn item.
type ItemStartedEvent struct {
	ThreadID  ThreadID `json:"thread_id"`
	TurnID    string   `json:"turn_id"`
	Item      TurnItem `json:"item"`
	StartedAt int64    `json:"started_at_ms"`
}

// ItemCompletedEvent signals the completion of a turn item.
//
// CompletedAt uses serde `#[serde(default)]` (defaults to 0), so it is always
// present on the wire.
type ItemCompletedEvent struct {
	ThreadID    ThreadID `json:"thread_id"`
	TurnID      string   `json:"turn_id"`
	Item        TurnItem `json:"item"`
	CompletedAt int64    `json:"completed_at_ms"`
}

// AgentMessageContentDeltaEvent carries an incremental agent-message delta.
type AgentMessageContentDeltaEvent struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
	Delta    string `json:"delta"`
}

// PlanDeltaEvent carries an incremental plan delta.
type PlanDeltaEvent struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
	Delta    string `json:"delta"`
}

// ReasoningContentDeltaEvent carries an incremental reasoning summary delta.
type ReasoningContentDeltaEvent struct {
	ThreadID     string `json:"thread_id"`
	TurnID       string `json:"turn_id"`
	ItemID       string `json:"item_id"`
	Delta        string `json:"delta"`
	SummaryIndex int64  `json:"summary_index"`
}

// ReasoningRawContentDeltaEvent carries an incremental raw-reasoning delta.
type ReasoningRawContentDeltaEvent struct {
	ThreadID     string `json:"thread_id"`
	TurnID       string `json:"turn_id"`
	ItemID       string `json:"item_id"`
	Delta        string `json:"delta"`
	ContentIndex int64  `json:"content_index"`
}

// HookEventName enumerates the hook lifecycle events. Rust: snake_case.
type HookEventName string

const (
	HookEventNamePreToolUse        HookEventName = "pre_tool_use"
	HookEventNamePermissionRequest HookEventName = "permission_request"
	HookEventNamePostToolUse       HookEventName = "post_tool_use"
	HookEventNamePreCompact        HookEventName = "pre_compact"
	HookEventNamePostCompact       HookEventName = "post_compact"
	HookEventNameSessionStart      HookEventName = "session_start"
	HookEventNameUserPromptSubmit  HookEventName = "user_prompt_submit"
	HookEventNameSubagentStart     HookEventName = "subagent_start"
	HookEventNameSubagentStop      HookEventName = "subagent_stop"
	HookEventNameStop              HookEventName = "stop"
)

// HookHandlerType enumerates how a hook is dispatched. Rust: snake_case.
type HookHandlerType string

const (
	HookHandlerTypeCommand HookHandlerType = "command"
	HookHandlerTypePrompt  HookHandlerType = "prompt"
	HookHandlerTypeAgent   HookHandlerType = "agent"
)

// HookExecutionMode enumerates synchronous vs asynchronous hook execution.
type HookExecutionMode string

const (
	HookExecutionModeSync  HookExecutionMode = "sync"
	HookExecutionModeAsync HookExecutionMode = "async"
)

// HookScope enumerates the lifetime scope of a hook.
type HookScope string

const (
	HookScopeThread HookScope = "thread"
	HookScopeTurn   HookScope = "turn"
)

// HookSource enumerates the origin of a hook configuration. Rust default:
// Unknown.
type HookSource string

const (
	HookSourceSystem                  HookSource = "system"
	HookSourceUser                    HookSource = "user"
	HookSourceProject                 HookSource = "project"
	HookSourceMdm                     HookSource = "mdm"
	HookSourceSessionFlags            HookSource = "session_flags"
	HookSourcePlugin                  HookSource = "plugin"
	HookSourceCloudRequirements       HookSource = "cloud_requirements"
	HookSourceLegacyManagedConfigFile HookSource = "legacy_managed_config_file"
	HookSourceLegacyManagedConfigMdm  HookSource = "legacy_managed_config_mdm"
	HookSourceUnknown                 HookSource = "unknown"
)

// HookTrustStatus enumerates the trust state of a hook. Rust: snake_case.
type HookTrustStatus string

const (
	HookTrustStatusManaged   HookTrustStatus = "managed"
	HookTrustStatusUntrusted HookTrustStatus = "untrusted"
	HookTrustStatusTrusted   HookTrustStatus = "trusted"
	HookTrustStatusModified  HookTrustStatus = "modified"
)

// HookRunStatus enumerates the run state of a hook. Rust: snake_case.
type HookRunStatus string

const (
	HookRunStatusRunning   HookRunStatus = "running"
	HookRunStatusCompleted HookRunStatus = "completed"
	HookRunStatusFailed    HookRunStatus = "failed"
	HookRunStatusBlocked   HookRunStatus = "blocked"
	HookRunStatusStopped   HookRunStatus = "stopped"
)

// HookOutputEntryKind enumerates the kind of a hook output entry.
type HookOutputEntryKind string

const (
	HookOutputEntryKindWarning  HookOutputEntryKind = "warning"
	HookOutputEntryKindStop     HookOutputEntryKind = "stop"
	HookOutputEntryKindFeedback HookOutputEntryKind = "feedback"
	HookOutputEntryKindContext  HookOutputEntryKind = "context"
	HookOutputEntryKindError    HookOutputEntryKind = "error"
)

// HookOutputEntry is a single hook output entry.
type HookOutputEntry struct {
	Kind HookOutputEntryKind `json:"kind"`
	Text string              `json:"text"`
}

// HookRunSummary summarizes a single hook run.
//
// SourcePath is a Rust AbsolutePathBuf, modeled with AbsolutePath. Source uses
// serde `#[serde(default)]` and is therefore always present on the wire.
type HookRunSummary struct {
	ID            string            `json:"id"`
	EventName     HookEventName     `json:"event_name"`
	HandlerType   HookHandlerType   `json:"handler_type"`
	ExecutionMode HookExecutionMode `json:"execution_mode"`
	Scope         HookScope         `json:"scope"`
	SourcePath    AbsolutePath      `json:"source_path"`
	Source        HookSource        `json:"source"`
	DisplayOrder  int64             `json:"display_order"`
	Status        HookRunStatus     `json:"status"`
	StatusMessage *string           `json:"status_message"`
	StartedAt     int64             `json:"started_at"`
	CompletedAt   *int64            `json:"completed_at"`
	DurationMs    *int64            `json:"duration_ms"`
	Entries       []HookOutputEntry `json:"entries"`
}

// HookStartedEvent signals the start of a hook run.
type HookStartedEvent struct {
	TurnID *string        `json:"turn_id"`
	Run    HookRunSummary `json:"run"`
}

// HookCompletedEvent signals the completion of a hook run.
type HookCompletedEvent struct {
	TurnID *string        `json:"turn_id"`
	Run    HookRunSummary `json:"run"`
}

// AgentStatusKind is the discriminator for AgentStatus. Rust uses
// `rename_all = "snake_case"` with an externally-tagged representation: unit
// variants serialize as bare strings, payload variants as single-key objects.
type AgentStatusKind string

const (
	AgentStatusPendingInit AgentStatusKind = "pending_init"
	AgentStatusRunning     AgentStatusKind = "running"
	AgentStatusInterrupted AgentStatusKind = "interrupted"
	AgentStatusCompleted   AgentStatusKind = "completed"
	AgentStatusErrored     AgentStatusKind = "errored"
	AgentStatusShutdown    AgentStatusKind = "shutdown"
	AgentStatusNotFound    AgentStatusKind = "not_found"
)

// AgentStatus is the lifecycle status of an agent. It is an externally-tagged
// enum: the unit variants (PendingInit, Running, ...) serialize as bare
// strings, while Completed(Option<String>) serializes as
// {"completed": <string|null>} and Errored(String) as {"errored": <string>}.
type AgentStatus struct {
	Kind AgentStatusKind
	// CompletedMessage holds the optional final message for the Completed
	// variant (may be nil).
	CompletedMessage *string
	// ErroredMessage holds the error text for the Errored variant.
	ErroredMessage string
}

// MarshalJSON encodes the externally-tagged AgentStatus.
func (a AgentStatus) MarshalJSON() ([]byte, error) {
	switch a.Kind {
	case AgentStatusCompleted:
		return json.Marshal(map[string]*string{string(a.Kind): a.CompletedMessage})
	case AgentStatusErrored:
		msg := a.ErroredMessage
		return json.Marshal(map[string]string{string(a.Kind): msg})
	default:
		return json.Marshal(string(a.Kind))
	}
}

// UnmarshalJSON decodes the externally-tagged AgentStatus.
func (a *AgentStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		a.Kind = AgentStatusKind(s)
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	for k, v := range m {
		a.Kind = AgentStatusKind(k)
		switch a.Kind {
		case AgentStatusCompleted:
			var msg *string
			if err := json.Unmarshal(v, &msg); err != nil {
				return err
			}
			a.CompletedMessage = msg
		case AgentStatusErrored:
			var msg string
			if err := json.Unmarshal(v, &msg); err != nil {
				return err
			}
			a.ErroredMessage = msg
		}
		return nil
	}
	return nil
}

// CollabAgentSpawnBeginEvent reports the start of a collab agent spawn.
//
// ReasoningEffort is a ReasoningEffortConfig (owned by config); kept raw.
type CollabAgentSpawnBeginEvent struct {
	CallID          string          `json:"call_id"`
	StartedAt       int64           `json:"started_at_ms"`
	SenderThreadID  ThreadID        `json:"sender_thread_id"`
	Prompt          string          `json:"prompt"`
	Model           string          `json:"model"`
	ReasoningEffort json.RawMessage `json:"reasoning_effort"`
}

// CollabAgentRef references a collab agent by thread with optional metadata.
type CollabAgentRef struct {
	ThreadID      ThreadID `json:"thread_id"`
	AgentNickname *string  `json:"agent_nickname,omitempty"`
	AgentRole     *string  `json:"agent_role,omitempty"`
}

// CollabAgentStatusEntry pairs an agent reference with its last-known status.
type CollabAgentStatusEntry struct {
	ThreadID      ThreadID    `json:"thread_id"`
	AgentNickname *string     `json:"agent_nickname,omitempty"`
	AgentRole     *string     `json:"agent_role,omitempty"`
	Status        AgentStatus `json:"status"`
}

// CollabAgentSpawnEndEvent reports the completion of a collab agent spawn.
type CollabAgentSpawnEndEvent struct {
	CallID           string          `json:"call_id"`
	CompletedAt      int64           `json:"completed_at_ms"`
	SenderThreadID   ThreadID        `json:"sender_thread_id"`
	NewThreadID      *ThreadID       `json:"new_thread_id"`
	NewAgentNickname *string         `json:"new_agent_nickname,omitempty"`
	NewAgentRole     *string         `json:"new_agent_role,omitempty"`
	Prompt           string          `json:"prompt"`
	Model            string          `json:"model"`
	ReasoningEffort  json.RawMessage `json:"reasoning_effort"`
	Status           AgentStatus     `json:"status"`
}

// CollabAgentInteractionBeginEvent reports the start of a collab interaction.
type CollabAgentInteractionBeginEvent struct {
	CallID           string   `json:"call_id"`
	StartedAt        int64    `json:"started_at_ms"`
	SenderThreadID   ThreadID `json:"sender_thread_id"`
	ReceiverThreadID ThreadID `json:"receiver_thread_id"`
	Prompt           string   `json:"prompt"`
}

// CollabAgentInteractionEndEvent reports the completion of a collab interaction.
type CollabAgentInteractionEndEvent struct {
	CallID                string      `json:"call_id"`
	CompletedAt           int64       `json:"completed_at_ms"`
	SenderThreadID        ThreadID    `json:"sender_thread_id"`
	ReceiverThreadID      ThreadID    `json:"receiver_thread_id"`
	ReceiverAgentNickname *string     `json:"receiver_agent_nickname,omitempty"`
	ReceiverAgentRole     *string     `json:"receiver_agent_role,omitempty"`
	Prompt                string      `json:"prompt"`
	Status                AgentStatus `json:"status"`
}

// CollabWaitingBeginEvent reports the start of a collab waiting period.
type CollabWaitingBeginEvent struct {
	StartedAt         int64            `json:"started_at_ms"`
	SenderThreadID    ThreadID         `json:"sender_thread_id"`
	ReceiverThreadIDs []ThreadID       `json:"receiver_thread_ids"`
	ReceiverAgents    []CollabAgentRef `json:"receiver_agents,omitempty"`
	CallID            string           `json:"call_id"`
}

// CollabWaitingEndEvent reports the completion of a collab waiting period.
//
// Statuses maps a ThreadID to an AgentStatus. ThreadID marshals as a string,
// matching the Rust HashMap<ThreadId, AgentStatus> JSON-object encoding.
type CollabWaitingEndEvent struct {
	SenderThreadID ThreadID                 `json:"sender_thread_id"`
	CallID         string                   `json:"call_id"`
	CompletedAt    int64                    `json:"completed_at_ms"`
	AgentStatuses  []CollabAgentStatusEntry `json:"agent_statuses,omitempty"`
	Statuses       map[string]AgentStatus   `json:"statuses"`
}

// CollabCloseBeginEvent reports the start of a collab close.
type CollabCloseBeginEvent struct {
	CallID           string   `json:"call_id"`
	StartedAt        int64    `json:"started_at_ms"`
	SenderThreadID   ThreadID `json:"sender_thread_id"`
	ReceiverThreadID ThreadID `json:"receiver_thread_id"`
}

// CollabCloseEndEvent reports the completion of a collab close.
type CollabCloseEndEvent struct {
	CallID                string      `json:"call_id"`
	CompletedAt           int64       `json:"completed_at_ms"`
	SenderThreadID        ThreadID    `json:"sender_thread_id"`
	ReceiverThreadID      ThreadID    `json:"receiver_thread_id"`
	ReceiverAgentNickname *string     `json:"receiver_agent_nickname,omitempty"`
	ReceiverAgentRole     *string     `json:"receiver_agent_role,omitempty"`
	Status                AgentStatus `json:"status"`
}

// CollabResumeBeginEvent reports the start of a collab resume.
type CollabResumeBeginEvent struct {
	CallID                string   `json:"call_id"`
	StartedAt             int64    `json:"started_at_ms"`
	SenderThreadID        ThreadID `json:"sender_thread_id"`
	ReceiverThreadID      ThreadID `json:"receiver_thread_id"`
	ReceiverAgentNickname *string  `json:"receiver_agent_nickname,omitempty"`
	ReceiverAgentRole     *string  `json:"receiver_agent_role,omitempty"`
}

// CollabResumeEndEvent reports the completion of a collab resume.
type CollabResumeEndEvent struct {
	CallID                string      `json:"call_id"`
	CompletedAt           int64       `json:"completed_at_ms"`
	SenderThreadID        ThreadID    `json:"sender_thread_id"`
	ReceiverThreadID      ThreadID    `json:"receiver_thread_id"`
	ReceiverAgentNickname *string     `json:"receiver_agent_nickname,omitempty"`
	ReceiverAgentRole     *string     `json:"receiver_agent_role,omitempty"`
	Status                AgentStatus `json:"status"`
}
