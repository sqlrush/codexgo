package tui

// Conversions from engine interaction events to the TUI overlay request types
// (A1 approval + A3 user-input/permissions wiring). These let chat_bottom's
// handleCoreEvent translate a decoded protocol event into the request a
// ListSelection/Approval/UserInput overlay renders, then push it on the stack.

import (
	"encoding/json"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// derefStr returns *s or "" when nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// execApprovalRequest builds the approval modal request for an exec approval.
// ThreadID is left empty so the decision routes to the active thread
// (Engine.SubmitCommand falls back to the running thread for an empty target).
func execApprovalRequest(e *protocol.ExecApprovalRequestEvent) ApprovalRequest {
	id := e.CallID
	if e.ApprovalID != nil && *e.ApprovalID != "" {
		id = *e.ApprovalID
	}
	return ApprovalRequest{
		Kind:    ApprovalExec,
		ID:      id,
		Command: e.Command,
		Reason:  derefStr(e.Reason),
		Cwd:     string(e.Cwd),
	}
}

// patchApprovalRequest builds the approval modal request for an apply-patch.
func patchApprovalRequest(e *protocol.ApplyPatchApprovalRequestEvent) ApprovalRequest {
	return ApprovalRequest{
		Kind:   ApprovalPatch,
		ID:     e.CallID,
		Reason: derefStr(e.Reason),
		Cwd:    derefStr(e.GrantRoot),
	}
}

// permissionsApprovalRequest builds the approval modal request for a
// permission-grant prompt. The opaque permission profile is echoed back
// verbatim on grant.
func permissionsApprovalRequest(e *protocol.RequestPermissionsEvent) ApprovalRequest {
	perms, _ := json.Marshal(e.Permissions)
	cwd := ""
	if e.Cwd != nil {
		cwd = string(*e.Cwd)
	}
	return ApprovalRequest{
		Kind:        ApprovalPermissions,
		ID:          e.CallID,
		Reason:      derefStr(e.Reason),
		Cwd:         cwd,
		Permissions: perms,
	}
}

// userInputRequestFromEvent converts a request_user_input event into the
// overlay's request model.
func userInputRequestFromEvent(e *protocol.RequestUserInputEvent) UserInputRequest {
	questions := make([]UserInputQuestion, 0, len(e.Questions))
	for _, q := range e.Questions {
		uq := UserInputQuestion{
			ID:       q.ID,
			Header:   q.Header,
			Question: q.Question,
			IsOther:  q.IsOther,
		}
		if q.Options != nil {
			for _, opt := range *q.Options {
				uq.Options = append(uq.Options, UserInputOption{
					Label:       opt.Label,
					Description: opt.Description,
				})
			}
		}
		questions = append(questions, uq)
	}
	return UserInputRequest{
		TurnID:    e.TurnID,
		ItemID:    e.CallID,
		Questions: questions,
	}
}
