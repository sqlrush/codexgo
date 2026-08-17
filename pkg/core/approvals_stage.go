package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// This file ports `core/src/tools/approvals.rs` (upstream 0.147; spec 50 D0.4):
// the centralized approval stage every action-taking tool goes through.
// Precedence mirrors upstream:
//
//  1. Hooks — a permission-request hook may allow or deny outright;
//  2. Reviewer — under strict auto-review, or when the turn routes approvals to
//     an automated reviewer, the injected [ReviewerApprover] decides (upstream
//     Guardian; codexgo ships no reviewer, hosts inject one — airush AD-9);
//  3. User — otherwise the user is prompted (exec / patch approval events), with
//     the "approved for session" cache consulted first.
//
// Every resolution is recorded through [ToolDecisionRecorder] (upstream
// session_telemetry.tool_decision) and mapped to the tool result: denied /
// timed-out / aborted decisions become a rejection the model sees; approvals
// are returned for the caller to act on.

// ApprovalActionKind names the kind of action awaiting approval.
type ApprovalActionKind string

const (
	// ApprovalActionShell is a shell_command invocation.
	ApprovalActionShell ApprovalActionKind = "shell"
	// ApprovalActionExecCommand is a unified-exec (exec_command) invocation.
	ApprovalActionExecCommand ApprovalActionKind = "unified_exec"
	// ApprovalActionApplyPatch is an apply_patch invocation.
	ApprovalActionApplyPatch ApprovalActionKind = "apply_patch"
)

// ApprovalAction describes what is being approved. Mirrors the Rust
// `ApprovalAction` enum (flattened: fields not relevant to a kind stay zero).
type ApprovalAction struct {
	Kind ApprovalActionKind
	// ID is the call id of the action.
	ID string
	// EnvironmentID names the environment the action runs in (empty = local).
	EnvironmentID string

	// Command / HookCommand / Cwd / TTY / Justification / additional permissions
	// apply to Shell and ExecCommand.
	Command                     []string
	HookCommand                 string
	Cwd                         string
	TTY                         bool
	Justification               *string
	ProposedExecpolicyAmendment *protocol.ExecPolicyAmendment
	AdditionalPermissions       *protocol.AdditionalPermissionProfile
	// SandboxPermissions is the sandbox permission request (opaque here; part
	// of the cache key).
	SandboxPermissions string

	// Patch / Files / Changes / PermissionsPreapproved apply to ApplyPatch.
	Patch                  string
	Files                  []string
	Changes                map[string]protocol.FileChange
	PermissionsPreapproved bool
}

// PermissionRequestPayload is the hook-facing description of the action.
// Mirrors the Rust `PermissionRequestPayload` (bash / apply_patch shapes).
type PermissionRequestPayload struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// PermissionRequestPayload renders the hook payload for the action.
func (a ApprovalAction) PermissionRequestPayload() PermissionRequestPayload {
	switch a.Kind {
	case ApprovalActionApplyPatch:
		input, _ := json.Marshal(map[string]any{"command": a.Patch})
		return PermissionRequestPayload{ToolName: "apply_patch", ToolInput: input}
	default:
		body := map[string]any{"command": a.HookCommand}
		if a.Justification != nil {
			body["justification"] = *a.Justification
		}
		input, _ := json.Marshal(body)
		return PermissionRequestPayload{ToolName: "bash", ToolInput: input}
	}
}

// approvalCacheKey is the JSON-serializable key for the approved-for-session
// cache. Mirrors the Rust `ApprovalCacheKey` variants.
type approvalCacheKey struct {
	Kind                  ApprovalActionKind                    `json:"kind"`
	EnvironmentID         string                                `json:"environment_id"`
	Command               []string                              `json:"command,omitempty"`
	Cwd                   string                                `json:"cwd"`
	SandboxPermissions    string                                `json:"sandbox_permissions,omitempty"`
	AdditionalPermissions *protocol.AdditionalPermissionProfile `json:"additional_permissions,omitempty"`
	TTY                   bool                                  `json:"tty,omitempty"`
	Files                 []string                              `json:"files,omitempty"`
}

// CacheKeys returns the approved-for-session cache keys for the action.
func (a ApprovalAction) CacheKeys() []any {
	switch a.Kind {
	case ApprovalActionApplyPatch:
		return []any{approvalCacheKey{Kind: a.Kind, EnvironmentID: a.EnvironmentID, Cwd: a.Cwd, Files: a.Files}}
	default:
		return []any{approvalCacheKey{
			Kind: a.Kind, EnvironmentID: a.EnvironmentID, Command: a.Command, Cwd: a.Cwd,
			SandboxPermissions: a.SandboxPermissions, AdditionalPermissions: a.AdditionalPermissions, TTY: a.TTY,
		}}
	}
}

// ApprovalContext carries the per-call inputs of an approval request. Mirrors
// the Rust `ApprovalContext`.
type ApprovalContext struct {
	Turn     *TurnContext
	CallID   string
	ToolName protocol.ToolName
	// StrictAutoReview forces the automated reviewer route.
	StrictAutoReview bool
	// ApprovalReason is the reason the action needs approval; RetryReason is
	// set when the approval is a retry after a sandbox denial.
	ApprovalReason         *string
	RetryReason            *string
	NetworkApprovalContext *protocol.NetworkApprovalContext
}

// ApprovalResolutionSource says who resolved an approval.
type ApprovalResolutionSource string

const (
	ApprovalResolvedByHook     ApprovalResolutionSource = "hook"
	ApprovalResolvedByReviewer ApprovalResolutionSource = "reviewer"
	ApprovalResolvedByUser     ApprovalResolutionSource = "user"
)

// ApprovalResolution is a decision plus its source.
type ApprovalResolution struct {
	Decision protocol.ReviewDecision
	Source   ApprovalResolutionSource
}

// PermissionRequestDecision is a hook's verdict on an action.
type PermissionRequestDecision struct {
	// Allow approves; otherwise Deny with Message.
	Allow   bool
	Message string
}

// PermissionRequestHook is the optional hook interface consulted first by the
// approval stage (upstream `run_permission_request_hooks`). A hooks engine that
// implements it may allow / deny an action; nil result = no opinion.
type PermissionRequestHook interface {
	PermissionRequest(ctx context.Context, tc *TurnContext, runID string, payload PermissionRequestPayload) (*PermissionRequestDecision, error)
}

// ReviewerApprover is the automated approval reviewer (upstream Guardian). It
// receives the action with its reasons and returns the review decision. codexgo
// ships no reviewer; hosts inject one through [SessionServices.Approver].
type ReviewerApprover interface {
	ReviewApproval(ctx context.Context, tc *TurnContext, action ApprovalAction, approvalReason, retryReason *string) protocol.ReviewDecision
}

// ToolDecisionRecorder receives every approval resolution (upstream
// `session_telemetry.tool_decision`), letting hosts audit who allowed what.
type ToolDecisionRecorder interface {
	RecordToolDecision(tc *TurnContext, toolName protocol.ToolName, callID string, decision protocol.ReviewDecision, source ApprovalResolutionSource)
}

// ErrApprovalRejected is the base of rejection errors so callers can detect
// "the action was rejected" regardless of the phrasing.
var ErrApprovalRejected = errors.New("core: approval rejected")

// approvalRejectionError wraps the model-facing rejection message.
type approvalRejectionError struct{ message string }

func (e *approvalRejectionError) Error() string { return e.message }
func (e *approvalRejectionError) Unwrap() error { return ErrApprovalRejected }

// ToolError converts the rejection into the model-facing tool error.
func (e *approvalRejectionError) ToolError() *tools.FunctionCallError {
	return tools.RespondToModelError(e.message)
}

// RejectionMessage returns the rejection message when err is an approval
// rejection.
func RejectionMessage(err error) (string, bool) {
	var rej *approvalRejectionError
	if errors.As(err, &rej) {
		return rej.message, true
	}
	return "", false
}

// intoToolResult maps a resolution to the tool result: denied / timed-out /
// aborted / deny-amendment decisions become rejections; approvals pass through.
// Mirrors `ApprovalResolution::into_tool_result`.
func (r ApprovalResolution) intoToolResult() (protocol.ReviewDecision, error) {
	switch r.Decision.Kind {
	case protocol.ReviewDecisionDenied:
		msg := "rejected"
		if r.Decision.Rejection != nil && *r.Decision.Rejection != "" {
			msg = *r.Decision.Rejection
		}
		return protocol.ReviewDecision{}, &approvalRejectionError{message: msg}
	case protocol.ReviewDecisionTimedOut:
		return protocol.ReviewDecision{}, &approvalRejectionError{message: "approval request timed out"}
	case protocol.ReviewDecisionAbort:
		return protocol.ReviewDecision{}, &approvalRejectionError{message: "approval request aborted"}
	case protocol.ReviewDecisionNetworkPolicyAmendment:
		if r.Decision.NetworkPolicyAmendment != nil && r.Decision.NetworkPolicyAmendment.Action == protocol.NetworkPolicyRuleActionDeny {
			msg := "rejected by user"
			switch r.Source {
			case ApprovalResolvedByHook:
				msg = "rejected by configuration"
			case ApprovalResolvedByReviewer:
				msg = "automatic approval review denied the action"
			}
			return protocol.ReviewDecision{}, &approvalRejectionError{message: msg}
		}
		return r.Decision, nil
	default:
		return r.Decision, nil
	}
}

// RequestApproval is the centralized approval stage. Mirrors
// `Session::request_approval`.
func (s *Session) RequestApproval(ctx context.Context, action ApprovalAction, actx ApprovalContext) (protocol.ReviewDecision, error) {
	runID := actx.CallID
	if actx.RetryReason != nil {
		runID = actx.CallID + ":retry"
	}
	var resolution ApprovalResolution
	if hook, ok := s.services.HooksEngine.(PermissionRequestHook); ok && hook != nil {
		decision, err := hook.PermissionRequest(ctx, actx.Turn, runID, action.PermissionRequestPayload())
		if err != nil {
			slog.Warn("permission request hook failed; falling through to reviewer", "call_id", actx.CallID, "error", err)
		} else if decision != nil {
			if decision.Allow {
				resolution = ApprovalResolution{Decision: protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved}, Source: ApprovalResolvedByHook}
			} else {
				msg := decision.Message
				resolution = ApprovalResolution{Decision: protocol.ReviewDecision{Kind: protocol.ReviewDecisionDenied, Rejection: &msg}, Source: ApprovalResolvedByHook}
			}
			s.recordResolution(actx, resolution)
			return resolution.intoToolResult()
		}
	}
	resolution = s.requestReviewerApproval(ctx, action, actx)
	s.recordResolution(actx, resolution)
	return resolution.intoToolResult()
}

// requestReviewerApproval picks the reviewer route (automated reviewer or the
// user) and returns its decision with the source. Mirrors
// `request_reviewer_approval`.
func (s *Session) requestReviewerApproval(ctx context.Context, action ApprovalAction, actx ApprovalContext) ApprovalResolution {
	useReviewer := actx.StrictAutoReview || (actx.Turn != nil && protocol.NormalizeApprovalsReviewer(string(actx.Turn.ApprovalsReviewer)) == protocol.ApprovalsReviewerAutoReview)
	if useReviewer {
		reviewer := s.services.Approver
		if reviewer == nil {
			// Strict / automated review with no reviewer wired: fail closed.
			msg := "automatic approval review is not available"
			return ApprovalResolution{Decision: protocol.ReviewDecision{Kind: protocol.ReviewDecisionDenied, Rejection: &msg}, Source: ApprovalResolvedByReviewer}
		}
		return ApprovalResolution{Decision: reviewer.ReviewApproval(ctx, actx.Turn, action, actx.ApprovalReason, actx.RetryReason), Source: ApprovalResolvedByReviewer}
	}
	return ApprovalResolution{Decision: s.requestUserApproval(ctx, action, actx), Source: ApprovalResolvedByUser}
}

// requestUserApproval prompts the user, consulting the approved-for-session
// cache first. Mirrors `request_user_approval`.
func (s *Session) requestUserApproval(ctx context.Context, action ApprovalAction, actx ApprovalContext) protocol.ReviewDecision {
	switch action.Kind {
	case ApprovalActionApplyPatch:
		reason := firstNonNil(actx.RetryReason, actx.ApprovalReason)
		if action.PermissionsPreapproved && reason == nil {
			return protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved}
		}
		fetch := func(ctx context.Context) protocol.ReviewDecision {
			return s.RequestPatchApproval(ctx, actx.Turn, actx.CallID, action.Changes, reason, nil)
		}
		if reason != nil {
			return fetch(ctx)
		}
		return s.approvalStore().WithCachedApproval(ctx, action.CacheKeys(), fetch)
	default:
		reason := firstNonNil(actx.RetryReason, actx.ApprovalReason, action.Justification)
		return s.approvalStore().WithCachedApproval(ctx, action.CacheKeys(), func(ctx context.Context) protocol.ReviewDecision {
			return s.RequestCommandApproval(ctx, actx.Turn, ExecApprovalRequest{
				CallID:                      actx.CallID,
				Command:                     action.Command,
				Cwd:                         protocol.AbsolutePath(action.Cwd),
				Reason:                      reason,
				NetworkApprovalContext:      actx.NetworkApprovalContext,
				ProposedExecpolicyAmendment: action.ProposedExecpolicyAmendment,
				AdditionalPermissions:       action.AdditionalPermissions,
			})
		})
	}
}

// recordResolution reports the resolution to the host recorder (if any) and
// the log. Mirrors `record_resolution`.
func (s *Session) recordResolution(actx ApprovalContext, res ApprovalResolution) {
	if rec, ok := s.services.HooksEngine.(ToolDecisionRecorder); ok && rec != nil {
		rec.RecordToolDecision(actx.Turn, actx.ToolName, actx.CallID, res.Decision, res.Source)
	}
	if rec, ok := s.services.Approver.(ToolDecisionRecorder); ok && rec != nil {
		rec.RecordToolDecision(actx.Turn, actx.ToolName, actx.CallID, res.Decision, res.Source)
	}
	slog.Debug("tool approval resolved", "tool", actx.ToolName.String(), "call_id", actx.CallID, "decision", res.Decision.Kind, "source", res.Source)
}

// approvalStore returns the session's approved-for-session cache, creating it
// on first use.
func (s *Session) approvalStore() *ApprovalStore {
	s.approvalStoreOnce.Do(func() {
		if s.approvals == nil {
			s.approvals = NewApprovalStore()
		}
	})
	return s.approvals
}

func firstNonNil(vals ...*string) *string {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}
