package protocol

// This file ports the client->engine submission protocol from
// `protocol.rs`: the Submission envelope, the Op enum (internally tagged on
// `type`, snake_case, `#[non_exhaustive]`), the externally-tagged ReviewDecision
// enum (owned here), and the Op-only helper types — including realtime
// conversation parameters near the top of protocol.rs.
//
// Fidelity notes:
//   - Op is `#[serde(tag = "type", rename_all = "snake_case")]`. serde's
//     internally-tagged representation flattens newtype-variant payloads
//     (e.g. RealtimeConversationStart(ConversationStartParams)) alongside the
//     "type" key, and likewise flattens the fields of struct variants.
//   - The UserInput and ThreadSettings variants carry `#[serde(flatten)]`
//     ThreadSettingsOverrides, so its fields appear inline in the op object.
//   - Op is `#[non_exhaustive]`; unknown variants are tolerated on decode and
//     preserved verbatim so they can be re-emitted (forward compatibility).
//   - ReviewDecision is an externally-tagged enum: unit variants serialize as
//     bare strings, data variants as {"variant_name": {...}}.
//
// Several payload types are owned by sibling files in this package and are used
// here by bare identifier: ElicitationAction, ExecPolicyAmendment,
// NetworkPolicyAmendment, NetworkPolicyRuleAction, GuardianAssessmentEvent,
// PermissionProfile, ActivePermissionProfile, and ResponseInputItem.

import (
	"encoding/json"
	"fmt"
)

// W3cTraceContext is an optional W3C trace carrier propagated across async
// submission handoffs. Both fields use skip_serializing_if Option::is_none.
type W3cTraceContext struct {
	Traceparent *string `json:"traceparent,omitempty"`
	Tracestate  *string `json:"tracestate,omitempty"`
}

// Submission is a Submission Queue Entry: a request from the user.
//
// Rust: `Submission { id, op, client_user_message_id?, trace? }`. The optional
// fields use skip_serializing_if Option::is_none.
type Submission struct {
	// ID is the unique id for this Submission, used to correlate with Events.
	ID string `json:"id"`
	// Op is the submission payload.
	Op Op `json:"op"`
	// ClientUserMessageID is a client-provided id for the user message
	// represented by Op::UserInput.
	ClientUserMessageID *string `json:"client_user_message_id,omitempty"`
	// Trace is an optional W3C trace carrier.
	Trace *W3cTraceContext `json:"trace,omitempty"`
}

// McpServerRefreshConfig is the config payload for refreshing MCP servers.
// Both fields are opaque JSON values in Rust (serde_json::Value).
type McpServerRefreshConfig struct {
	McpServers                   json.RawMessage `json:"mcp_servers"`
	McpOauthCredentialsStoreMode json.RawMessage `json:"mcp_oauth_credentials_store_mode"`
}

// ----------------------------------------------------------------------------
// Realtime conversation parameters (top of protocol.rs).
// ----------------------------------------------------------------------------

// RealtimeOutputModality selects whether the realtime session produces text or
// audio output. Rust: snake_case.
type RealtimeOutputModality string

const (
	RealtimeOutputModalityText  RealtimeOutputModality = "text"
	RealtimeOutputModalityAudio RealtimeOutputModality = "audio"
)

// RealtimeVoice enumerates the voices supported by realtime conversation
// streams. Rust: snake_case (all lowercase single words here).
type RealtimeVoice string

const (
	RealtimeVoiceAlloy   RealtimeVoice = "alloy"
	RealtimeVoiceArbor   RealtimeVoice = "arbor"
	RealtimeVoiceAsh     RealtimeVoice = "ash"
	RealtimeVoiceBallad  RealtimeVoice = "ballad"
	RealtimeVoiceBreeze  RealtimeVoice = "breeze"
	RealtimeVoiceCedar   RealtimeVoice = "cedar"
	RealtimeVoiceCoral   RealtimeVoice = "coral"
	RealtimeVoiceCove    RealtimeVoice = "cove"
	RealtimeVoiceEcho    RealtimeVoice = "echo"
	RealtimeVoiceEmber   RealtimeVoice = "ember"
	RealtimeVoiceJuniper RealtimeVoice = "juniper"
	RealtimeVoiceMaple   RealtimeVoice = "maple"
	RealtimeVoiceMarin   RealtimeVoice = "marin"
	RealtimeVoiceSage    RealtimeVoice = "sage"
	RealtimeVoiceShimmer RealtimeVoice = "shimmer"
	RealtimeVoiceSol     RealtimeVoice = "sol"
	RealtimeVoiceSpruce  RealtimeVoice = "spruce"
	RealtimeVoiceVale    RealtimeVoice = "vale"
	RealtimeVoiceVerse   RealtimeVoice = "verse"
)

// ConversationStartTransportKind is the discriminator for
// ConversationStartTransport. Rust: `#[serde(tag = "type", rename_all =
// "snake_case")]`.
type ConversationStartTransportKind string

const (
	ConversationStartTransportWebsocket ConversationStartTransportKind = "websocket"
	ConversationStartTransportWebrtc    ConversationStartTransportKind = "webrtc"
)

// ConversationStartTransport selects the realtime transport. The Webrtc variant
// carries an SDP offer; Websocket carries no fields.
type ConversationStartTransport struct {
	Type ConversationStartTransportKind

	// SDP is populated only for the Webrtc variant.
	SDP string
}

// MarshalJSON emits the internally-tagged transport form.
func (t ConversationStartTransport) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(t.Type)}
	switch t.Type {
	case ConversationStartTransportWebsocket:
		// no fields
	case ConversationStartTransportWebrtc:
		m["sdp"] = t.SDP
	default:
		return nil, fmt.Errorf("unknown ConversationStartTransport type: %q", t.Type)
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes an internally-tagged transport value.
func (t *ConversationStartTransport) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type ConversationStartTransportKind `json:"type"`
		SDP  string                         `json:"sdp"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := ConversationStartTransport{Type: raw.Type}
	switch raw.Type {
	case ConversationStartTransportWebsocket:
		// no fields
	case ConversationStartTransportWebrtc:
		out.SDP = raw.SDP
	default:
		return fmt.Errorf("unknown ConversationStartTransport type: %q", raw.Type)
	}
	*t = out
	return nil
}

// ConversationStartParams configures a realtime conversation stream start.
//
// The `prompt` field is `Option<Option<String>>` in Rust with double-option
// serde semantics and skip_serializing_if Option::is_none: absent outer => key
// omitted; Some(None) => `"prompt": null`; Some(Some(s)) => `"prompt": "s"`.
// It is modeled here as a DoubleOptionString. The remaining optional fields use
// plain skip_serializing_if Option::is_none.
type ConversationStartParams struct {
	OutputModality    RealtimeOutputModality      `json:"output_modality"`
	Prompt            DoubleOptionString          `json:"-"`
	RealtimeSessionID *string                     `json:"realtime_session_id,omitempty"`
	Transport         *ConversationStartTransport `json:"transport,omitempty"`
	Voice             *RealtimeVoice              `json:"voice,omitempty"`
}

// MarshalJSON encodes ConversationStartParams, applying the double-option
// semantics for `prompt`.
func (p ConversationStartParams) MarshalJSON() ([]byte, error) {
	type alias ConversationStartParams
	raw, err := json.Marshal(alias(p))
	if err != nil {
		return nil, err
	}
	return mergeDoubleOption(raw, "prompt", p.Prompt)
}

// UnmarshalJSON decodes ConversationStartParams, capturing the double-option
// `prompt` field.
func (p *ConversationStartParams) UnmarshalJSON(data []byte) error {
	type alias ConversationStartParams
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if rawPrompt, ok := probe["prompt"]; ok {
		a.Prompt.Set = true
		var s string
		if string(rawPrompt) == "null" {
			a.Prompt.Value = nil
		} else if err := json.Unmarshal(rawPrompt, &s); err != nil {
			return err
		} else {
			a.Prompt.Value = &s
		}
	}
	*p = ConversationStartParams(a)
	return nil
}

// ConversationAudioParams carries an audio frame for the realtime stream. The
// RealtimeAudioFrame type is owned by a sibling realtime/event file.
type ConversationAudioParams struct {
	Frame RealtimeAudioFrame `json:"frame"`
}

// ConversationTextParams carries text input for the realtime stream.
type ConversationTextParams struct {
	Text string `json:"text"`
}

// ----------------------------------------------------------------------------
// Additional context and turn environment selection.
// ----------------------------------------------------------------------------

// TurnEnvironmentSelection selects a turn-scoped environment by id and cwd.
type TurnEnvironmentSelection struct {
	EnvironmentID string       `json:"environment_id"`
	Cwd           AbsolutePath `json:"cwd"`
}

// AdditionalContextKind classifies client-supplied context. Rust: snake_case.
type AdditionalContextKind string

const (
	AdditionalContextKindUntrusted   AdditionalContextKind = "untrusted"
	AdditionalContextKindApplication AdditionalContextKind = "application"
)

// AdditionalContextEntry is client-supplied context keyed by an opaque source
// identifier in the surrounding map.
type AdditionalContextEntry struct {
	Value string                `json:"value"`
	Kind  AdditionalContextKind `json:"kind"`
}

// ----------------------------------------------------------------------------
// ThreadSettingsOverrides.
// ----------------------------------------------------------------------------

// ThreadSettingsOverrides are persistent thread-settings overrides that can be
// applied before user input or on their own. It is flattened into the
// UserInput and ThreadSettings Op variants.
//
// The double-option fields `effort` and `service_tier` follow serde
// double-option semantics with skip_serializing_if Option::is_none:
//   - absent outer Option => key omitted entirely;
//   - Some(None) => the key is emitted with a JSON null;
//   - Some(Some(v)) => the key is emitted with the value.
//
// All other fields use plain skip_serializing_if Option::is_none.
type ThreadSettingsOverrides struct {
	Cwd                     *AbsolutePath               `json:"cwd,omitempty"`
	WorkspaceRoots          *[]AbsolutePath             `json:"workspace_roots,omitempty"`
	ProfileWorkspaceRoots   *[]AbsolutePath             `json:"profile_workspace_roots,omitempty"`
	ApprovalPolicy          *AskForApproval             `json:"approval_policy,omitempty"`
	ApprovalsReviewer       *ApprovalsReviewer          `json:"approvals_reviewer,omitempty"`
	SandboxPolicy           *SandboxPolicy              `json:"sandbox_policy,omitempty"`
	PermissionProfile       *PermissionProfile          `json:"permission_profile,omitempty"`
	ActivePermissionProfile *ActivePermissionProfile    `json:"active_permission_profile,omitempty"`
	WindowsSandboxLevel     *WindowsSandboxLevel        `json:"windows_sandbox_level,omitempty"`
	Model                   *string                     `json:"model,omitempty"`
	Effort                  DoubleOptionReasoningEffort `json:"-"`
	Summary                 *ReasoningSummary           `json:"summary,omitempty"`
	ServiceTier             DoubleOptionString          `json:"-"`
	CollaborationMode       *CollaborationMode          `json:"collaboration_mode,omitempty"`
	Personality             *Personality                `json:"personality,omitempty"`
}

// MarshalJSON encodes ThreadSettingsOverrides, applying double-option semantics
// for `effort` and `service_tier`.
func (o ThreadSettingsOverrides) MarshalJSON() ([]byte, error) {
	type alias ThreadSettingsOverrides
	raw, err := json.Marshal(alias(o))
	if err != nil {
		return nil, err
	}
	raw, err = mergeDoubleOptionReasoningEffort(raw, "effort", o.Effort)
	if err != nil {
		return nil, err
	}
	return mergeDoubleOption(raw, "service_tier", o.ServiceTier)
}

// UnmarshalJSON decodes ThreadSettingsOverrides, capturing the double-option
// `effort` and `service_tier` fields.
func (o *ThreadSettingsOverrides) UnmarshalJSON(data []byte) error {
	type alias ThreadSettingsOverrides
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if rawEffort, ok := probe["effort"]; ok {
		a.Effort.Set = true
		if string(rawEffort) == "null" {
			a.Effort.Value = nil
		} else {
			var v ReasoningEffort
			if err := json.Unmarshal(rawEffort, &v); err != nil {
				return err
			}
			a.Effort.Value = &v
		}
	}
	if rawTier, ok := probe["service_tier"]; ok {
		a.ServiceTier.Set = true
		if string(rawTier) == "null" {
			a.ServiceTier.Value = nil
		} else {
			var s string
			if err := json.Unmarshal(rawTier, &s); err != nil {
				return err
			}
			a.ServiceTier.Value = &s
		}
	}
	*o = ThreadSettingsOverrides(a)
	return nil
}

// isEmpty reports whether the overrides carry no fields. Used to mirror serde's
// flatten of a defaulted struct (which contributes no keys).
func (o ThreadSettingsOverrides) isEmpty() bool {
	b, err := json.Marshal(o)
	if err != nil {
		return false
	}
	return string(b) == "{}"
}

// ----------------------------------------------------------------------------
// Double-option helpers.
// ----------------------------------------------------------------------------

// DoubleOptionString models a serde `Option<Option<String>>` with
// skip_serializing_if Option::is_none on the outer Option. Set reports whether
// the outer Option is present; when Set is true, a nil Value means inner None
// (serialized as JSON null) and a non-nil Value means the contained string.
type DoubleOptionString struct {
	Set   bool
	Value *string
}

// SetString returns a present double-option holding the given string.
func SetString(s string) DoubleOptionString {
	return DoubleOptionString{Set: true, Value: &s}
}

// ClearString returns a present double-option holding inner None (JSON null).
func ClearString() DoubleOptionString {
	return DoubleOptionString{Set: true, Value: nil}
}

// DoubleOptionReasoningEffort models a serde
// `Option<Option<ReasoningEffort>>` with the same semantics as
// DoubleOptionString.
type DoubleOptionReasoningEffort struct {
	Set   bool
	Value *ReasoningEffort
}

// SetReasoningEffort returns a present double-option holding the given effort.
func SetReasoningEffort(e ReasoningEffort) DoubleOptionReasoningEffort {
	return DoubleOptionReasoningEffort{Set: true, Value: &e}
}

// ClearReasoningEffort returns a present double-option holding inner None.
func ClearReasoningEffort() DoubleOptionReasoningEffort {
	return DoubleOptionReasoningEffort{Set: true, Value: nil}
}

// mergeDoubleOption injects (or removes) the given key into a JSON object
// according to double-option semantics for a string-valued field.
func mergeDoubleOption(obj []byte, key string, opt DoubleOptionString) ([]byte, error) {
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(obj, &m); err != nil {
		return nil, err
	}
	delete(m, key)
	if opt.Set {
		if opt.Value == nil {
			m[key] = json.RawMessage("null")
		} else {
			v, err := json.Marshal(*opt.Value)
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
	}
	return json.Marshal(m)
}

// mergeDoubleOptionReasoningEffort is the ReasoningEffort analogue of
// mergeDoubleOption.
func mergeDoubleOptionReasoningEffort(obj []byte, key string, opt DoubleOptionReasoningEffort) ([]byte, error) {
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(obj, &m); err != nil {
		return nil, err
	}
	delete(m, key)
	if opt.Set {
		if opt.Value == nil {
			m[key] = json.RawMessage("null")
		} else {
			v, err := json.Marshal(*opt.Value)
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
	}
	return json.Marshal(m)
}

// ----------------------------------------------------------------------------
// InterAgentCommunication.
// ----------------------------------------------------------------------------

// InterAgentCommunication is inter-agent communication recorded as assistant
// history while using the normal thread submission lifecycle. `other_recipients`
// is `#[serde(default)]` and always emitted (no skip_serializing_if); serde
// serializes an empty Vec as [] (never null), so a nil slice is normalized to
// an empty array on marshal.
type InterAgentCommunication struct {
	Author          AgentPath   `json:"author"`
	Recipient       AgentPath   `json:"recipient"`
	OtherRecipients []AgentPath `json:"other_recipients"`
	Content         string      `json:"content"`
	TriggerTurn     bool        `json:"trigger_turn"`
}

// MarshalJSON ensures OtherRecipients serializes as [] rather than null when
// empty, matching serde's always-emit-Vec behavior.
func (c InterAgentCommunication) MarshalJSON() ([]byte, error) {
	type alias InterAgentCommunication
	a := alias(c)
	if a.OtherRecipients == nil {
		a.OtherRecipients = []AgentPath{}
	}
	return json.Marshal(a)
}

// ----------------------------------------------------------------------------
// ThreadMemoryMode.
// ----------------------------------------------------------------------------

// ThreadMemoryMode controls whether a thread remains eligible for memory
// generation. Rust: lowercase.
type ThreadMemoryMode string

const (
	ThreadMemoryModeEnabled  ThreadMemoryMode = "enabled"
	ThreadMemoryModeDisabled ThreadMemoryMode = "disabled"
)

// ----------------------------------------------------------------------------
// Review request/target.
// ----------------------------------------------------------------------------

// ReviewTargetKind is the discriminator for ReviewTarget. Rust:
// `#[serde(tag = "type", rename_all = "camelCase")]`. Single-word variants are
// unchanged under camelCase.
type ReviewTargetKind string

const (
	ReviewTargetUncommittedChanges ReviewTargetKind = "uncommittedChanges"
	ReviewTargetBaseBranch         ReviewTargetKind = "baseBranch"
	ReviewTargetCommit             ReviewTargetKind = "commit"
	ReviewTargetCustom             ReviewTargetKind = "custom"
)

// ReviewTarget selects what the agent should review. It is internally tagged on
// "type"; each variant additionally uses camelCase for its own fields.
type ReviewTarget struct {
	Type ReviewTargetKind

	// BaseBranch variant.
	Branch string
	// Commit variant.
	SHA   string
	Title *string
	// Custom variant.
	Instructions string
}

// MarshalJSON emits the internally-tagged review target form.
func (t ReviewTarget) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(t.Type)}
	switch t.Type {
	case ReviewTargetUncommittedChanges:
		// no fields
	case ReviewTargetBaseBranch:
		m["branch"] = t.Branch
	case ReviewTargetCommit:
		m["sha"] = t.SHA
		// `title: Option<String>` has no skip_serializing_if, so serde always
		// emits the field (null when absent).
		m["title"] = t.Title
	case ReviewTargetCustom:
		m["instructions"] = t.Instructions
	default:
		return nil, fmt.Errorf("unknown ReviewTarget type: %q", t.Type)
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes an internally-tagged review target value.
func (t *ReviewTarget) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type         ReviewTargetKind `json:"type"`
		Branch       string           `json:"branch"`
		SHA          string           `json:"sha"`
		Title        *string          `json:"title"`
		Instructions string           `json:"instructions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := ReviewTarget{Type: raw.Type}
	switch raw.Type {
	case ReviewTargetUncommittedChanges:
		// no fields
	case ReviewTargetBaseBranch:
		out.Branch = raw.Branch
	case ReviewTargetCommit:
		out.SHA = raw.SHA
		out.Title = raw.Title
	case ReviewTargetCustom:
		out.Instructions = raw.Instructions
	default:
		return fmt.Errorf("unknown ReviewTarget type: %q", raw.Type)
	}
	*t = out
	return nil
}

// ReviewRequest is a request for a code review from the agent. `user_facing_hint`
// uses skip_serializing_if Option::is_none.
type ReviewRequest struct {
	Target         ReviewTarget `json:"target"`
	UserFacingHint *string      `json:"user_facing_hint,omitempty"`
}

// ----------------------------------------------------------------------------
// ReviewDecision (owned here).
// ----------------------------------------------------------------------------

// ReviewDecisionKind is the discriminator for ReviewDecision. Rust:
// `#[serde(rename_all = "snake_case")]` with the default (external) tagging:
// unit variants serialize as bare strings; data variants serialize as
// {"variant_name": {...}}.
type ReviewDecisionKind string

const (
	ReviewDecisionApproved                    ReviewDecisionKind = "approved"
	ReviewDecisionApprovedExecpolicyAmendment ReviewDecisionKind = "approved_execpolicy_amendment"
	ReviewDecisionApprovedForSession          ReviewDecisionKind = "approved_for_session"
	ReviewDecisionNetworkPolicyAmendment      ReviewDecisionKind = "network_policy_amendment"
	ReviewDecisionDenied                      ReviewDecisionKind = "denied"
	ReviewDecisionTimedOut                    ReviewDecisionKind = "timed_out"
	ReviewDecisionAbort                       ReviewDecisionKind = "abort"
)

// ReviewDecision is the user's decision in response to an ExecApprovalRequest.
//
// It is an externally-tagged enum. Two variants carry a struct payload:
//   - ApprovedExecpolicyAmendment { proposed_execpolicy_amendment }
//   - NetworkPolicyAmendment { network_policy_amendment }
//
// The remaining variants are unit variants and serialize as bare strings. The
// Rust default is Denied.
type ReviewDecision struct {
	Kind ReviewDecisionKind

	// Populated only for the ApprovedExecpolicyAmendment variant.
	ProposedExecpolicyAmendment *ExecPolicyAmendment
	// Populated only for the NetworkPolicyAmendment variant.
	NetworkPolicyAmendment *NetworkPolicyAmendment
	// Rejection is the model-facing reason of a Denied decision (upstream 0.147
	// `Denied { rejection }`). nil marshals the 0.136 bare "denied" string; a
	// value marshals the 0.147 object form; both are accepted on input.
	Rejection *string
}

// NewReviewDecisionApproved returns a unit Approved decision.
func NewReviewDecisionApproved() ReviewDecision {
	return ReviewDecision{Kind: ReviewDecisionApproved}
}

// NewReviewDecisionDenied returns a unit Denied decision (the Rust default).
func NewReviewDecisionDenied() ReviewDecision {
	return ReviewDecision{Kind: ReviewDecisionDenied}
}

// MarshalJSON encodes the externally-tagged ReviewDecision.
func (d ReviewDecision) MarshalJSON() ([]byte, error) {
	switch d.Kind {
	case ReviewDecisionDenied:
		if d.Rejection != nil {
			return json.Marshal(map[string]any{string(d.Kind): map[string]any{"rejection": *d.Rejection}})
		}
		return json.Marshal(string(d.Kind))
	case ReviewDecisionApproved,
		ReviewDecisionApprovedForSession,
		ReviewDecisionTimedOut,
		ReviewDecisionAbort:
		return json.Marshal(string(d.Kind))
	case ReviewDecisionApprovedExecpolicyAmendment:
		payload := map[string]any{
			"proposed_execpolicy_amendment": d.ProposedExecpolicyAmendment,
		}
		return json.Marshal(map[string]any{string(d.Kind): payload})
	case ReviewDecisionNetworkPolicyAmendment:
		payload := map[string]any{
			"network_policy_amendment": d.NetworkPolicyAmendment,
		}
		return json.Marshal(map[string]any{string(d.Kind): payload})
	default:
		return nil, fmt.Errorf("unknown ReviewDecision kind: %q", d.Kind)
	}
}

// UnmarshalJSON decodes the externally-tagged ReviewDecision.
func (d *ReviewDecision) UnmarshalJSON(data []byte) error {
	// Unit variants arrive as bare strings.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch ReviewDecisionKind(s) {
		case ReviewDecisionApproved,
			ReviewDecisionApprovedForSession,
			ReviewDecisionDenied,
			ReviewDecisionTimedOut,
			ReviewDecisionAbort:
			*d = ReviewDecision{Kind: ReviewDecisionKind(s)}
			return nil
		default:
			return fmt.Errorf("unknown ReviewDecision string variant: %q", s)
		}
	}

	// Data variants arrive as a single-key object.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	if raw, ok := obj[string(ReviewDecisionDenied)]; ok {
		var payload struct {
			Rejection string `json:"rejection"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return err
		}
		*d = ReviewDecision{Kind: ReviewDecisionDenied, Rejection: &payload.Rejection}
		return nil
	}
	if raw, ok := obj[string(ReviewDecisionApprovedExecpolicyAmendment)]; ok {
		var payload struct {
			ProposedExecpolicyAmendment ExecPolicyAmendment `json:"proposed_execpolicy_amendment"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return err
		}
		*d = ReviewDecision{
			Kind:                        ReviewDecisionApprovedExecpolicyAmendment,
			ProposedExecpolicyAmendment: &payload.ProposedExecpolicyAmendment,
		}
		return nil
	}
	if raw, ok := obj[string(ReviewDecisionNetworkPolicyAmendment)]; ok {
		var payload struct {
			NetworkPolicyAmendment NetworkPolicyAmendment `json:"network_policy_amendment"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return err
		}
		*d = ReviewDecision{
			Kind:                   ReviewDecisionNetworkPolicyAmendment,
			NetworkPolicyAmendment: &payload.NetworkPolicyAmendment,
		}
		return nil
	}
	return fmt.Errorf("unknown ReviewDecision object variant: %s", string(data))
}

// ----------------------------------------------------------------------------
// Op enum.
// ----------------------------------------------------------------------------

// OpKind is the discriminator for the Op enum. Rust:
// `#[serde(tag = "type", rename_all = "snake_case")]`. The UserInputAnswer
// variant is renamed to "user_input_answer" with the deserialize alias
// "request_user_input_response".
type OpKind string

const (
	OpInterrupt                      OpKind = "interrupt"
	OpCleanBackgroundTerminals       OpKind = "clean_background_terminals"
	OpRealtimeConversationStart      OpKind = "realtime_conversation_start"
	OpRealtimeConversationAudio      OpKind = "realtime_conversation_audio"
	OpRealtimeConversationText       OpKind = "realtime_conversation_text"
	OpRealtimeConversationClose      OpKind = "realtime_conversation_close"
	OpRealtimeConversationListVoices OpKind = "realtime_conversation_list_voices"
	OpUserInput                      OpKind = "user_input"
	OpThreadSettings                 OpKind = "thread_settings"
	OpInterAgentCommunication        OpKind = "inter_agent_communication"
	OpExecApproval                   OpKind = "exec_approval"
	OpPatchApproval                  OpKind = "patch_approval"
	OpResolveElicitation             OpKind = "resolve_elicitation"
	OpUserInputAnswer                OpKind = "user_input_answer"
	OpRequestPermissionsResponse     OpKind = "request_permissions_response"
	OpDynamicToolResponse            OpKind = "dynamic_tool_response"
	OpRefreshMcpServers              OpKind = "refresh_mcp_servers"
	OpReloadUserConfig               OpKind = "reload_user_config"
	OpCompact                        OpKind = "compact"
	OpSetThreadMemoryMode            OpKind = "set_thread_memory_mode"
	OpThreadRollback                 OpKind = "thread_rollback"
	OpReview                         OpKind = "review"
	OpApproveGuardianDeniedAction    OpKind = "approve_guardian_denied_action"
	OpShutdown                       OpKind = "shutdown"
	OpRunUserShellCommand            OpKind = "run_user_shell_command"
)

// opUserInputAnswerAlias is the accepted deserialize alias for the
// UserInputAnswer variant, mirroring Rust's
// `alias = "request_user_input_response"`.
const opUserInputAnswerAlias = "request_user_input_response"

// Op is a submission operation: the client->engine payload. It is an
// internally-tagged enum (discriminator "type", snake_case) and is
// `#[non_exhaustive]` in Rust, so unknown variants are tolerated and preserved.
//
// Because each variant carries different fields, Op is modeled as a single
// struct holding all possible payloads plus the Type discriminator. Custom JSON
// marshaling emits exactly the fields the matching Rust variant would, flattening
// newtype-variant payloads and the embedded ThreadSettingsOverrides per serde's
// internally-tagged representation.
type Op struct {
	Type OpKind

	// Realtime newtype-variant payloads. Each is flattened alongside "type".
	RealtimeConversationStartParams *ConversationStartParams
	RealtimeConversationAudioParams *ConversationAudioParams
	RealtimeConversationTextParams  *ConversationTextParams

	// UserInput variant.
	Items                      []UserInput
	Environments               *[]TurnEnvironmentSelection
	FinalOutputJSONSchema      json.RawMessage
	ResponsesAPIClientMetadata map[string]string
	AdditionalContext          map[string]AdditionalContextEntry
	// ThreadSettings is flattened in both UserInput and ThreadSettings variants.
	ThreadSettings ThreadSettingsOverrides

	// InterAgentCommunication variant.
	Communication *InterAgentCommunication

	// ExecApproval / PatchApproval / UserInputAnswer /
	// RequestPermissionsResponse / DynamicToolResponse share an "id" field.
	ID string

	// ExecApproval variant additionally carries an optional turn id.
	TurnID *string

	// ExecApproval / PatchApproval decision.
	Decision *ReviewDecision

	// ResolveElicitation variant.
	ServerName          string
	RequestID           *RequestId
	ElicitationDecision *ElicitationAction
	Content             json.RawMessage
	Meta                json.RawMessage

	// UserInputAnswer variant response.
	UserInputResponse *RequestUserInputResponse

	// RequestPermissionsResponse variant response.
	PermissionsResponse *RequestPermissionsResponse

	// DynamicToolResponse variant response.
	DynamicResponse *DynamicToolResponse

	// RefreshMcpServers variant.
	RefreshConfig *McpServerRefreshConfig

	// SetThreadMemoryMode variant.
	MemoryMode ThreadMemoryMode

	// ThreadRollback variant.
	NumTurns uint32

	// Review variant.
	ReviewRequest *ReviewRequest

	// ApproveGuardianDeniedAction variant.
	GuardianEvent *GuardianAssessmentEvent

	// RunUserShellCommand variant.
	Command string

	// Extra captures the fields of unknown (forward-compatible) variants so
	// they can be re-emitted unchanged.
	Extra map[string]json.RawMessage
}

// Kind returns the snake_case wire discriminator for this Op, mirroring the
// Rust `Op::kind` method.
func (o Op) Kind() string { return string(o.Type) }

// MarshalJSON encodes the internally-tagged Op object.
func (o Op) MarshalJSON() ([]byte, error) {
	m := map[string]json.RawMessage{}

	put := func(key string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		m[key] = b
		return nil
	}

	switch o.Type {
	case OpInterrupt,
		OpCleanBackgroundTerminals,
		OpRealtimeConversationClose,
		OpRealtimeConversationListVoices,
		OpReloadUserConfig,
		OpCompact,
		OpShutdown:
		// unit variants: only "type"
	case OpRealtimeConversationStart:
		if err := flattenInto(m, o.RealtimeConversationStartParams); err != nil {
			return nil, err
		}
	case OpRealtimeConversationAudio:
		if err := flattenInto(m, o.RealtimeConversationAudioParams); err != nil {
			return nil, err
		}
	case OpRealtimeConversationText:
		if err := flattenInto(m, o.RealtimeConversationTextParams); err != nil {
			return nil, err
		}
	case OpUserInput:
		items := o.Items
		if items == nil {
			items = []UserInput{}
		}
		if err := put("items", items); err != nil {
			return nil, err
		}
		if o.Environments != nil {
			if err := put("environments", *o.Environments); err != nil {
				return nil, err
			}
		}
		if o.FinalOutputJSONSchema != nil {
			m["final_output_json_schema"] = o.FinalOutputJSONSchema
		}
		if o.ResponsesAPIClientMetadata != nil {
			if err := put("responsesapi_client_metadata", o.ResponsesAPIClientMetadata); err != nil {
				return nil, err
			}
		}
		if len(o.AdditionalContext) > 0 {
			if err := put("additional_context", o.AdditionalContext); err != nil {
				return nil, err
			}
		}
		if err := flattenInto(m, o.ThreadSettings); err != nil {
			return nil, err
		}
	case OpThreadSettings:
		if err := flattenInto(m, o.ThreadSettings); err != nil {
			return nil, err
		}
	case OpInterAgentCommunication:
		if err := put("communication", o.Communication); err != nil {
			return nil, err
		}
	case OpExecApproval:
		if err := put("id", o.ID); err != nil {
			return nil, err
		}
		if o.TurnID != nil {
			if err := put("turn_id", *o.TurnID); err != nil {
				return nil, err
			}
		}
		if err := put("decision", o.Decision); err != nil {
			return nil, err
		}
	case OpPatchApproval:
		if err := put("id", o.ID); err != nil {
			return nil, err
		}
		if err := put("decision", o.Decision); err != nil {
			return nil, err
		}
	case OpResolveElicitation:
		if err := put("server_name", o.ServerName); err != nil {
			return nil, err
		}
		if err := put("request_id", o.RequestID); err != nil {
			return nil, err
		}
		if err := put("decision", o.ElicitationDecision); err != nil {
			return nil, err
		}
		if o.Content != nil {
			m["content"] = o.Content
		}
		if o.Meta != nil {
			m["meta"] = o.Meta
		}
	case OpUserInputAnswer:
		if err := put("id", o.ID); err != nil {
			return nil, err
		}
		if err := put("response", o.UserInputResponse); err != nil {
			return nil, err
		}
	case OpRequestPermissionsResponse:
		if err := put("id", o.ID); err != nil {
			return nil, err
		}
		if err := put("response", o.PermissionsResponse); err != nil {
			return nil, err
		}
	case OpDynamicToolResponse:
		if err := put("id", o.ID); err != nil {
			return nil, err
		}
		if err := put("response", o.DynamicResponse); err != nil {
			return nil, err
		}
	case OpRefreshMcpServers:
		if err := put("config", o.RefreshConfig); err != nil {
			return nil, err
		}
	case OpSetThreadMemoryMode:
		if err := put("mode", string(o.MemoryMode)); err != nil {
			return nil, err
		}
	case OpThreadRollback:
		if err := put("num_turns", o.NumTurns); err != nil {
			return nil, err
		}
	case OpReview:
		if err := put("review_request", o.ReviewRequest); err != nil {
			return nil, err
		}
	case OpApproveGuardianDeniedAction:
		if err := put("event", o.GuardianEvent); err != nil {
			return nil, err
		}
	case OpRunUserShellCommand:
		if err := put("command", o.Command); err != nil {
			return nil, err
		}
	default:
		// Forward-compatible unknown variant: re-emit captured fields.
		for k, v := range o.Extra {
			m[k] = v
		}
	}

	tag, err := json.Marshal(string(o.Type))
	if err != nil {
		return nil, err
	}
	m["type"] = tag
	return json.Marshal(m)
}

// flattenInto marshals v (expected to encode to a JSON object) and copies its
// keys into m, mirroring serde's internally-tagged flattening of newtype and
// flattened struct payloads.
func flattenInto(m map[string]json.RawMessage, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if string(b) == "null" {
		return nil
	}
	sub := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &sub); err != nil {
		return fmt.Errorf("flattened payload is not a JSON object: %w", err)
	}
	for k, val := range sub {
		m[k] = val
	}
	return nil
}

// UnmarshalJSON decodes an internally-tagged Op object. Unknown variants are
// tolerated: their non-"type" fields are captured in Extra.
func (o *Op) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}

	kind := OpKind(probe.Type)
	if probe.Type == opUserInputAnswerAlias {
		kind = OpUserInputAnswer
	}
	out := Op{Type: kind}

	switch kind {
	case OpInterrupt,
		OpCleanBackgroundTerminals,
		OpRealtimeConversationClose,
		OpRealtimeConversationListVoices,
		OpReloadUserConfig,
		OpCompact,
		OpShutdown:
		// unit variants: nothing else to read
	case OpRealtimeConversationStart:
		var p ConversationStartParams
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		out.RealtimeConversationStartParams = &p
	case OpRealtimeConversationAudio:
		var p ConversationAudioParams
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		out.RealtimeConversationAudioParams = &p
	case OpRealtimeConversationText:
		var p ConversationTextParams
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		out.RealtimeConversationTextParams = &p
	case OpUserInput:
		var raw struct {
			Items                      []UserInput                       `json:"items"`
			Environments               *[]TurnEnvironmentSelection       `json:"environments"`
			FinalOutputJSONSchema      json.RawMessage                   `json:"final_output_json_schema"`
			ResponsesAPIClientMetadata map[string]string                 `json:"responsesapi_client_metadata"`
			AdditionalContext          map[string]AdditionalContextEntry `json:"additional_context"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.Items = raw.Items
		out.Environments = raw.Environments
		out.FinalOutputJSONSchema = raw.FinalOutputJSONSchema
		out.ResponsesAPIClientMetadata = raw.ResponsesAPIClientMetadata
		out.AdditionalContext = raw.AdditionalContext
		if err := json.Unmarshal(data, &out.ThreadSettings); err != nil {
			return err
		}
	case OpThreadSettings:
		if err := json.Unmarshal(data, &out.ThreadSettings); err != nil {
			return err
		}
	case OpInterAgentCommunication:
		var raw struct {
			Communication InterAgentCommunication `json:"communication"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.Communication = &raw.Communication
	case OpExecApproval:
		var raw struct {
			ID       string         `json:"id"`
			TurnID   *string        `json:"turn_id"`
			Decision ReviewDecision `json:"decision"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.ID = raw.ID
		out.TurnID = raw.TurnID
		out.Decision = &raw.Decision
	case OpPatchApproval:
		var raw struct {
			ID       string         `json:"id"`
			Decision ReviewDecision `json:"decision"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.ID = raw.ID
		out.Decision = &raw.Decision
	case OpResolveElicitation:
		var raw struct {
			ServerName string            `json:"server_name"`
			RequestID  RequestId         `json:"request_id"`
			Decision   ElicitationAction `json:"decision"`
			Content    json.RawMessage   `json:"content"`
			Meta       json.RawMessage   `json:"meta"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.ServerName = raw.ServerName
		out.RequestID = &raw.RequestID
		out.ElicitationDecision = &raw.Decision
		out.Content = raw.Content
		out.Meta = raw.Meta
	case OpUserInputAnswer:
		var raw struct {
			ID       string                   `json:"id"`
			Response RequestUserInputResponse `json:"response"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.ID = raw.ID
		out.UserInputResponse = &raw.Response
	case OpRequestPermissionsResponse:
		var raw struct {
			ID       string                     `json:"id"`
			Response RequestPermissionsResponse `json:"response"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.ID = raw.ID
		out.PermissionsResponse = &raw.Response
	case OpDynamicToolResponse:
		var raw struct {
			ID       string              `json:"id"`
			Response DynamicToolResponse `json:"response"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.ID = raw.ID
		out.DynamicResponse = &raw.Response
	case OpRefreshMcpServers:
		var raw struct {
			Config McpServerRefreshConfig `json:"config"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.RefreshConfig = &raw.Config
	case OpSetThreadMemoryMode:
		var raw struct {
			Mode ThreadMemoryMode `json:"mode"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.MemoryMode = raw.Mode
	case OpThreadRollback:
		var raw struct {
			NumTurns uint32 `json:"num_turns"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.NumTurns = raw.NumTurns
	case OpReview:
		var raw struct {
			ReviewRequest ReviewRequest `json:"review_request"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.ReviewRequest = &raw.ReviewRequest
	case OpApproveGuardianDeniedAction:
		var raw struct {
			Event GuardianAssessmentEvent `json:"event"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.GuardianEvent = &raw.Event
	case OpRunUserShellCommand:
		var raw struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		out.Command = raw.Command
	default:
		// Forward-compatible unknown variant: capture all non-"type" fields.
		var all map[string]json.RawMessage
		if err := json.Unmarshal(data, &all); err != nil {
			return err
		}
		delete(all, "type")
		out.Extra = all
	}

	*o = out
	return nil
}

// NewReviewDecisionDeniedWith returns a Denied decision carrying the
// model-facing rejection reason (upstream 0.147 `ReviewDecision::denied`).
func NewReviewDecisionDeniedWith(rejection string) ReviewDecision {
	return ReviewDecision{Kind: ReviewDecisionDenied, Rejection: &rejection}
}
