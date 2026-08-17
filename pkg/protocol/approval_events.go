package protocol

import "encoding/json"

// This file completes the port of codex-rs/protocol/src/approvals.rs. The op.go
// and event.go ports reference these shared approval/guardian/elicitation types
// by bare identifier; they are defined here to keep them in one cohesive place.

// ---------------------------------------------------------------------------
// Policy amendments
// ---------------------------------------------------------------------------

// ExecPolicyAmendment is a proposed execpolicy change to allow commands starting
// with a token prefix. Rust derives #[serde(transparent)], so the JSON wire form
// is a bare array of strings.
type ExecPolicyAmendment struct {
	Command []string
}

// MarshalJSON emits the transparent array form.
func (e ExecPolicyAmendment) MarshalJSON() ([]byte, error) { return json.Marshal(e.Command) }

// UnmarshalJSON reads the transparent array form.
func (e *ExecPolicyAmendment) UnmarshalJSON(b []byte) error { return json.Unmarshal(b, &e.Command) }

// NetworkPolicyRuleAction is the allow/deny action of a network policy amendment.
type NetworkPolicyRuleAction string

const (
	NetworkPolicyRuleActionAllow NetworkPolicyRuleAction = "allow"
	NetworkPolicyRuleActionDeny  NetworkPolicyRuleAction = "deny"
)

// NetworkPolicyAmendment proposes allowing/denying a host for future requests.
type NetworkPolicyAmendment struct {
	Host   string                  `json:"host"`
	Action NetworkPolicyRuleAction `json:"action"`
}

// ---------------------------------------------------------------------------
// Guardian assessment
// ---------------------------------------------------------------------------

// GuardianRiskLevel is the coarse risk label of a guardian assessment.
type GuardianRiskLevel string

const (
	GuardianRiskLow      GuardianRiskLevel = "low"
	GuardianRiskMedium   GuardianRiskLevel = "medium"
	GuardianRiskHigh     GuardianRiskLevel = "high"
	GuardianRiskCritical GuardianRiskLevel = "critical"
)

// GuardianUserAuthorization is how directly the transcript authorizes an action.
type GuardianUserAuthorization string

const (
	GuardianUserAuthUnknown GuardianUserAuthorization = "unknown"
	GuardianUserAuthLow     GuardianUserAuthorization = "low"
	GuardianUserAuthMedium  GuardianUserAuthorization = "medium"
	GuardianUserAuthHigh    GuardianUserAuthorization = "high"
)

// GuardianAssessmentStatus is the lifecycle status of a guardian review.
type GuardianAssessmentStatus string

const (
	GuardianStatusInProgress GuardianAssessmentStatus = "in_progress"
	GuardianStatusApproved   GuardianAssessmentStatus = "approved"
	GuardianStatusDenied     GuardianAssessmentStatus = "denied"
	GuardianStatusTimedOut   GuardianAssessmentStatus = "timed_out"
	GuardianStatusAborted    GuardianAssessmentStatus = "aborted"
)

// GuardianAssessmentDecisionSource identifies what produced the terminal decision.
type GuardianAssessmentDecisionSource string

const (
	GuardianDecisionSourceAgent GuardianAssessmentDecisionSource = "agent"
)

// GuardianCommandSource is the surface a reviewed command came through.
type GuardianCommandSource string

const (
	GuardianCommandSourceShell       GuardianCommandSource = "shell"
	GuardianCommandSourceUnifiedExec GuardianCommandSource = "unified_exec"
)

// GuardianAssessmentActionKind is the discriminator for GuardianAssessmentAction.
type GuardianAssessmentActionKind string

const (
	GuardianActionCommand            GuardianAssessmentActionKind = "command"
	GuardianActionExecve             GuardianAssessmentActionKind = "execve"
	GuardianActionApplyPatch         GuardianAssessmentActionKind = "apply_patch"
	GuardianActionNetworkAccess      GuardianAssessmentActionKind = "network_access"
	GuardianActionMcpToolCall        GuardianAssessmentActionKind = "mcp_tool_call"
	GuardianActionRequestPermissions GuardianAssessmentActionKind = "request_permissions"
)

// GuardianAssessmentAction is the canonical action payload reviewed by the
// guardian. Rust models it as an internally-tagged enum (tag = "type"); here it
// is a single struct with a Kind discriminator and custom JSON.
type GuardianAssessmentAction struct {
	Kind GuardianAssessmentActionKind

	// command / execve
	Source GuardianCommandSource
	// command
	Command string
	// execve
	Program string
	Argv    []string
	// command / execve / apply_patch
	Cwd AbsolutePath
	// apply_patch
	Files []AbsolutePath
	// network_access
	Target   string
	Host     string
	Protocol NetworkApprovalProtocol
	Port     uint16
	// mcp_tool_call
	Server        string
	ToolName      string
	ConnectorID   *string
	ConnectorName *string
	ToolTitle     *string
	// request_permissions
	Reason      *string
	Permissions *RequestPermissionProfile
}

// MarshalJSON emits the internally-tagged form keyed by Kind.
func (a GuardianAssessmentAction) MarshalJSON() ([]byte, error) {
	switch a.Kind {
	case GuardianActionCommand:
		return json.Marshal(struct {
			Type    GuardianAssessmentActionKind `json:"type"`
			Source  GuardianCommandSource        `json:"source"`
			Command string                       `json:"command"`
			Cwd     AbsolutePath                 `json:"cwd"`
		}{a.Kind, a.Source, a.Command, a.Cwd})
	case GuardianActionExecve:
		return json.Marshal(struct {
			Type    GuardianAssessmentActionKind `json:"type"`
			Source  GuardianCommandSource        `json:"source"`
			Program string                       `json:"program"`
			Argv    []string                     `json:"argv"`
			Cwd     AbsolutePath                 `json:"cwd"`
		}{a.Kind, a.Source, a.Program, a.Argv, a.Cwd})
	case GuardianActionApplyPatch:
		return json.Marshal(struct {
			Type  GuardianAssessmentActionKind `json:"type"`
			Cwd   AbsolutePath                 `json:"cwd"`
			Files []AbsolutePath               `json:"files"`
		}{a.Kind, a.Cwd, a.Files})
	case GuardianActionNetworkAccess:
		return json.Marshal(struct {
			Type     GuardianAssessmentActionKind `json:"type"`
			Target   string                       `json:"target"`
			Host     string                       `json:"host"`
			Protocol NetworkApprovalProtocol      `json:"protocol"`
			Port     uint16                       `json:"port"`
		}{a.Kind, a.Target, a.Host, a.Protocol, a.Port})
	case GuardianActionMcpToolCall:
		return json.Marshal(struct {
			Type          GuardianAssessmentActionKind `json:"type"`
			Server        string                       `json:"server"`
			ToolName      string                       `json:"tool_name"`
			ConnectorID   *string                      `json:"connector_id"`
			ConnectorName *string                      `json:"connector_name"`
			ToolTitle     *string                      `json:"tool_title"`
		}{a.Kind, a.Server, a.ToolName, a.ConnectorID, a.ConnectorName, a.ToolTitle})
	case GuardianActionRequestPermissions:
		return json.Marshal(struct {
			Type        GuardianAssessmentActionKind `json:"type"`
			Reason      *string                      `json:"reason"`
			Permissions *RequestPermissionProfile    `json:"permissions"`
		}{a.Kind, a.Reason, a.Permissions})
	default:
		return nil, &json.UnsupportedValueError{Str: string(a.Kind)}
	}
}

// UnmarshalJSON reads the internally-tagged form.
func (a *GuardianAssessmentAction) UnmarshalJSON(b []byte) error {
	var head struct {
		Type GuardianAssessmentActionKind `json:"type"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	a.Kind = head.Type
	var full struct {
		Source        GuardianCommandSource     `json:"source"`
		Command       string                    `json:"command"`
		Program       string                    `json:"program"`
		Argv          []string                  `json:"argv"`
		Cwd           AbsolutePath              `json:"cwd"`
		Files         []AbsolutePath            `json:"files"`
		Target        string                    `json:"target"`
		Host          string                    `json:"host"`
		Protocol      NetworkApprovalProtocol   `json:"protocol"`
		Port          uint16                    `json:"port"`
		Server        string                    `json:"server"`
		ToolName      string                    `json:"tool_name"`
		ConnectorID   *string                   `json:"connector_id"`
		ConnectorName *string                   `json:"connector_name"`
		ToolTitle     *string                   `json:"tool_title"`
		Reason        *string                   `json:"reason"`
		Permissions   *RequestPermissionProfile `json:"permissions"`
	}
	if err := json.Unmarshal(b, &full); err != nil {
		return err
	}
	a.Source = full.Source
	a.Command = full.Command
	a.Program = full.Program
	a.Argv = full.Argv
	a.Cwd = full.Cwd
	a.Files = full.Files
	a.Target = full.Target
	a.Host = full.Host
	a.Protocol = full.Protocol
	a.Port = full.Port
	a.Server = full.Server
	a.ToolName = full.ToolName
	a.ConnectorID = full.ConnectorID
	a.ConnectorName = full.ConnectorName
	a.ToolTitle = full.ToolTitle
	a.Reason = full.Reason
	a.Permissions = full.Permissions
	return nil
}

// GuardianAssessmentEvent reports the lifecycle of a guardian review.
type GuardianAssessmentEvent struct {
	ID             string                            `json:"id"`
	TargetItemID   *string                           `json:"target_item_id,omitempty"`
	TurnID         string                            `json:"turn_id"`
	StartedAtMs    int64                             `json:"started_at_ms"`
	CompletedAtMs  *int64                            `json:"completed_at_ms,omitempty"`
	Status         GuardianAssessmentStatus          `json:"status"`
	RiskLevel      *GuardianRiskLevel                `json:"risk_level,omitempty"`
	UserAuth       *GuardianUserAuthorization        `json:"user_authorization,omitempty"`
	Rationale      *string                           `json:"rationale,omitempty"`
	DecisionSource *GuardianAssessmentDecisionSource `json:"decision_source,omitempty"`
	Action         GuardianAssessmentAction          `json:"action"`
}

// ---------------------------------------------------------------------------
// Exec / apply-patch approval requests
// ---------------------------------------------------------------------------

// ExecApprovalRequestEvent asks the user to approve a command execution.
type ExecApprovalRequestEvent struct {
	CallID                          string                       `json:"call_id"`
	ApprovalID                      *string                      `json:"approval_id,omitempty"`
	TurnID                          string                       `json:"turn_id"`
	StartedAtMs                     int64                        `json:"started_at_ms"`
	Command                         []string                     `json:"command"`
	Cwd                             AbsolutePath                 `json:"cwd"`
	Reason                          *string                      `json:"reason,omitempty"`
	NetworkApprovalContext          *NetworkApprovalContext      `json:"network_approval_context,omitempty"`
	ProposedExecpolicyAmendment     *ExecPolicyAmendment         `json:"proposed_execpolicy_amendment,omitempty"`
	ProposedNetworkPolicyAmendments *[]NetworkPolicyAmendment    `json:"proposed_network_policy_amendments,omitempty"`
	AdditionalPermissions           *AdditionalPermissionProfile `json:"additional_permissions,omitempty"`
	AvailableDecisions              *[]ReviewDecision            `json:"available_decisions,omitempty"`
	ParsedCmd                       []ParsedCommand              `json:"parsed_cmd"`
}

// EffectiveApprovalID returns ApprovalID when set, else CallID.
func (e ExecApprovalRequestEvent) EffectiveApprovalID() string {
	if e.ApprovalID != nil {
		return *e.ApprovalID
	}
	return e.CallID
}

// ApplyPatchApprovalRequestEvent asks the user to approve a patch application.
type ApplyPatchApprovalRequestEvent struct {
	CallID      string                `json:"call_id"`
	TurnID      string                `json:"turn_id"`
	StartedAtMs int64                 `json:"started_at_ms"`
	Changes     map[string]FileChange `json:"changes"`
	Reason      *string               `json:"reason,omitempty"`
	GrantRoot   *string               `json:"grant_root,omitempty"`
}

// ---------------------------------------------------------------------------
// Elicitation
// ---------------------------------------------------------------------------

// ElicitationAction is the user's response to an elicitation request.
type ElicitationAction string

const (
	ElicitationAccept  ElicitationAction = "accept"
	ElicitationDecline ElicitationAction = "decline"
	ElicitationCancel  ElicitationAction = "cancel"
)

// ElicitationRequestMode is the discriminator for ElicitationRequest (tag "mode").
type ElicitationRequestMode string

const (
	ElicitationModeForm ElicitationRequestMode = "form"
	ElicitationModeURL  ElicitationRequestMode = "url"
)

// ElicitationRequest is an MCP elicitation, internally tagged on "mode".
type ElicitationRequest struct {
	Mode ElicitationRequestMode
	// Meta is the optional "_meta" field, present for both modes.
	Meta json.RawMessage
	// Message is present for both modes.
	Message string
	// RequestedSchema is present for the Form mode.
	RequestedSchema json.RawMessage
	// URL and ElicitationID are present for the Url mode.
	URL           string
	ElicitationID string
}

// MarshalJSON emits the internally-tagged ("mode") form.
func (r ElicitationRequest) MarshalJSON() ([]byte, error) {
	switch r.Mode {
	case ElicitationModeForm:
		out := map[string]any{
			"mode":             string(r.Mode),
			"message":          r.Message,
			"requested_schema": r.RequestedSchema,
		}
		if len(r.Meta) > 0 {
			out["_meta"] = r.Meta
		}
		return json.Marshal(out)
	case ElicitationModeURL:
		out := map[string]any{
			"mode":           string(r.Mode),
			"message":        r.Message,
			"url":            r.URL,
			"elicitation_id": r.ElicitationID,
		}
		if len(r.Meta) > 0 {
			out["_meta"] = r.Meta
		}
		return json.Marshal(out)
	default:
		return nil, &json.UnsupportedValueError{Str: string(r.Mode)}
	}
}

// UnmarshalJSON reads the internally-tagged ("mode") form.
func (r *ElicitationRequest) UnmarshalJSON(b []byte) error {
	var full struct {
		Mode            ElicitationRequestMode `json:"mode"`
		Meta            json.RawMessage        `json:"_meta"`
		Message         string                 `json:"message"`
		RequestedSchema json.RawMessage        `json:"requested_schema"`
		URL             string                 `json:"url"`
		ElicitationID   string                 `json:"elicitation_id"`
	}
	if err := json.Unmarshal(b, &full); err != nil {
		return err
	}
	r.Mode = full.Mode
	r.Meta = full.Meta
	r.Message = full.Message
	r.RequestedSchema = full.RequestedSchema
	r.URL = full.URL
	r.ElicitationID = full.ElicitationID
	return nil
}

// Message returns the human-readable message for either mode.
func (r ElicitationRequest) MessageText() string { return r.Message }

// ElicitationRequestEvent wraps an MCP elicitation request.
type ElicitationRequestEvent struct {
	TurnID     *string            `json:"turn_id,omitempty"`
	ServerName string             `json:"server_name"`
	ID         RequestId          `json:"id"`
	Request    ElicitationRequest `json:"request"`
}
