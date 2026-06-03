package core

import (
	"context"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file ports the approval *routing* core of codex-core: the Session methods
// that emit an approval/permission/input request event (with deterministic,
// caller-supplied correlation ids), register a pending waiter, await the
// matching Op response (honoring cancellation and a bounded timeout), and the
// reciprocal Notify* methods the submission loop calls to resolve a waiter when
// the user's response arrives.
//
// It is a faithful, reduced port of the request_command_approval /
// request_patch_approval / request_permissions_for_cwd / request_user_input /
// elicitation flows from codex-rs/core/src/session/mod.rs and session/mcp.rs,
// plus the AskForApproval x sandbox decisioning in tools/sandboxing.rs (see
// approval_decision.go).
//
// STUB: guardian review routing (routes_approval_to_guardian /
// spawn_approval_request_review), the delegated sub-agent forwarding loop
// (codex_delegate.rs forward_events / forward_ops), MCP-tool legacy approval
// reconciliation, granted-permission merging into turn state, and
// request-permissions response normalization against the cwd are owned by the
// guardian / multi-agent / permissions area agents and are omitted here.

// defaultApprovalRequestTimeout is the ~30s approval window of the reference
// engine; without it an interrupted client could leave a turn blocked forever.
const defaultApprovalRequestTimeout = 30 * time.Second

// ApprovalRequestTimeout bounds how long the engine waits for a user's approval
// response before falling back to a deny/abort outcome. It mirrors the ~30s
// approval window of the reference engine.
//
// It is a package var (not a const) so the approval-routing tests can shorten
// the window without sleeping for the full duration; production callers leave it
// at [defaultApprovalRequestTimeout].
var ApprovalRequestTimeout = defaultApprovalRequestTimeout

// ElicitationResponse is the user's reply to an MCP elicitation request,
// delivered back to the waiting tool. It carries the data of the
// ResolveElicitation op: the user's action plus optional structured content and
// client metadata.
//
// The rich rmcp-level elicitation response type lives outside core; this is the
// reduced engine-facing view sufficient for the turn-running approval path.
type ElicitationResponse struct {
	// Decision is the user's action on the elicitation.
	Decision protocol.ElicitationAction
	// Content is the structured user input for accepted elicitations, if any.
	Content []byte
	// Meta is optional client metadata associated with the response, if any.
	Meta []byte
}

// nowUnixMillis returns the current Unix time in milliseconds, used to stamp
// request events (Rust `now_unix_timestamp_ms`).
func nowUnixMillis() int64 { return time.Now().UnixMilli() }

// emptyPermissionsResponse is the deny/abort fallback permissions response: an
// empty profile granted only for the current turn. Mirrors the Rust default
// returned on cancellation/timeout.
func emptyPermissionsResponse() protocol.RequestPermissionsResponse {
	return protocol.RequestPermissionsResponse{
		Permissions:      protocol.RequestPermissionProfile{},
		Scope:            protocol.PermissionGrantScopeTurn,
		StrictAutoReview: false,
	}
}

// emptyUserInputResponse is the fallback user-input response with no answers.
func emptyUserInputResponse() protocol.RequestUserInputResponse {
	return protocol.RequestUserInputResponse{Answers: map[string]protocol.RequestUserInputAnswer{}}
}

// emptyElicitationResponse is the fallback elicitation response (a cancel).
func emptyElicitationResponse() ElicitationResponse {
	return ElicitationResponse{Decision: protocol.ElicitationCancel}
}

// ----------------------------------------------------------------------------
// Exec (command) approval
// ----------------------------------------------------------------------------

// ExecApprovalRequest bundles the inputs for [Session.RequestCommandApproval].
// Grouping them keeps the call site readable and avoids a long positional
// argument list (the Rust signature is `#[allow(clippy::too_many_arguments)]`).
type ExecApprovalRequest struct {
	// CallID identifies the tool call awaiting approval (command-level key).
	CallID string
	// ApprovalID is the optional subcommand-callback approval id; when nil the
	// CallID is used as the effective approval id.
	ApprovalID *string
	// Command is the argv proposed for execution.
	Command []string
	// Cwd is the working directory for the command.
	Cwd protocol.AbsolutePath
	// Reason is an optional human-readable justification.
	Reason *string
	// NetworkApprovalContext describes a host/protocol awaiting approval, if any.
	NetworkApprovalContext *protocol.NetworkApprovalContext
	// ProposedExecpolicyAmendment is an optional execpolicy amendment.
	ProposedExecpolicyAmendment *protocol.ExecPolicyAmendment
	// AdditionalPermissions is an optional per-command permission overlay.
	AdditionalPermissions *protocol.AdditionalPermissionProfile
	// AvailableDecisions overrides the default decision set when non-nil.
	AvailableDecisions []protocol.ReviewDecision
}

// EffectiveApprovalID returns the id used to correlate the response: the
// explicit ApprovalID when set, otherwise the CallID. Mirrors the Rust
// `approval_id.clone().unwrap_or_else(|| call_id.clone())`.
func (r ExecApprovalRequest) EffectiveApprovalID() string {
	if r.ApprovalID != nil {
		return *r.ApprovalID
	}
	return r.CallID
}

// RequestCommandApproval emits an ExecApprovalRequest event, registers a pending
// waiter keyed by the effective approval id, and awaits the user's decision via
// a matching ExecApproval op. It returns Abort on cancellation or timeout.
//
// The waiter is registered before the event is emitted so a fast response cannot
// race ahead of registration (mirroring the Rust ordering).
func (s *Session) RequestCommandApproval(ctx context.Context, tc *TurnContext, req ExecApprovalRequest) protocol.ReviewDecision {
	effectiveID := req.EffectiveApprovalID()

	recv, replaced := s.insertExecPatchWaiter(effectiveID)
	if replaced != nil {
		// An overwritten waiter is unblocked with the default deny so it does not
		// hang; mirrors the Rust warn-and-overwrite (the oneshot sender is dropped).
		close(replaced)
	}

	proposedNetworkPolicyAmendments := networkPolicyAmendmentsFor(req.NetworkApprovalContext)
	available := req.AvailableDecisions
	if available == nil {
		available = DefaultAvailableDecisions(
			req.NetworkApprovalContext,
			req.ProposedExecpolicyAmendment,
			proposedNetworkPolicyAmendments,
			req.AdditionalPermissions,
		)
	}

	event := protocol.ExecApprovalRequestEvent{
		CallID:                      req.CallID,
		ApprovalID:                  req.ApprovalID,
		TurnID:                      tc.SubID,
		StartedAtMs:                 nowUnixMillis(),
		Command:                     req.Command,
		Cwd:                         req.Cwd,
		Reason:                      req.Reason,
		NetworkApprovalContext:      req.NetworkApprovalContext,
		ProposedExecpolicyAmendment: req.ProposedExecpolicyAmendment,
		AdditionalPermissions:       req.AdditionalPermissions,
		AvailableDecisions:          &available,
		ParsedCmd:                   parseCommandBestEffort(req.Command),
	}
	if len(proposedNetworkPolicyAmendments) > 0 {
		event.ProposedNetworkPolicyAmendments = &proposedNetworkPolicyAmendments
	}

	s.SendEvent(tc.SubID, protocol.EventMsg{
		Type:                protocol.EventMsgKindExecApprovalRequest,
		ExecApprovalRequest: &event,
	})

	return s.awaitReviewDecision(ctx, effectiveID, recv)
}

// RequestPatchApproval emits an ApplyPatchApprovalRequest event, registers a
// pending waiter keyed by the call id, and awaits the user's decision via a
// matching PatchApproval op. It returns Abort on cancellation or timeout.
func (s *Session) RequestPatchApproval(
	ctx context.Context,
	tc *TurnContext,
	callID string,
	changes map[string]protocol.FileChange,
	reason *string,
	grantRoot *string,
) protocol.ReviewDecision {
	approvalID := callID

	recv, replaced := s.insertExecPatchWaiter(approvalID)
	if replaced != nil {
		close(replaced)
	}

	event := protocol.ApplyPatchApprovalRequestEvent{
		CallID:      callID,
		TurnID:      tc.SubID,
		StartedAtMs: nowUnixMillis(),
		Changes:     changes,
		Reason:      reason,
		GrantRoot:   grantRoot,
	}
	s.SendEvent(tc.SubID, protocol.EventMsg{
		Type:                      protocol.EventMsgKindApplyPatchApprovalRequest,
		ApplyPatchApprovalRequest: &event,
	})

	return s.awaitReviewDecision(ctx, approvalID, recv)
}

// insertExecPatchWaiter registers a ReviewDecision waiter on the active turn's
// state, returning the receive channel and any replaced channel. When no turn is
// active it returns a pre-closed channel so the caller's await immediately
// yields the default deny (there is nobody to deliver a response).
func (s *Session) insertExecPatchWaiter(key string) (<-chan protocol.ReviewDecision, chan protocol.ReviewDecision) {
	at := s.ActiveTurn()
	if at == nil || at.State == nil {
		closed := make(chan protocol.ReviewDecision)
		close(closed)
		return closed, nil
	}
	return at.State.InsertPendingApproval(key)
}

// awaitReviewDecision blocks until the waiter receives a decision, the context
// is cancelled, or the approval timeout elapses. On cancellation or timeout it
// resolves the pending waiter (via NotifyApproval) with Abort so a later
// response cannot leak into a subsequent turn, and returns Abort.
func (s *Session) awaitReviewDecision(ctx context.Context, approvalID string, recv <-chan protocol.ReviewDecision) protocol.ReviewDecision {
	timer := time.NewTimer(ApprovalRequestTimeout)
	defer timer.Stop()

	select {
	case decision, ok := <-recv:
		if !ok {
			// Waiter cleared (interrupt/overwrite) before a response: treat as abort.
			return protocol.ReviewDecision{Kind: protocol.ReviewDecisionAbort}
		}
		return decision
	case <-ctx.Done():
		s.NotifyApproval(approvalID, protocol.ReviewDecision{Kind: protocol.ReviewDecisionAbort})
		return protocol.ReviewDecision{Kind: protocol.ReviewDecisionAbort}
	case <-timer.C:
		s.NotifyApproval(approvalID, protocol.ReviewDecision{Kind: protocol.ReviewDecisionTimedOut})
		return protocol.ReviewDecision{Kind: protocol.ReviewDecisionTimedOut}
	}
}

// NotifyApproval resolves a pending exec/patch approval waiter by delivering
// decision, mirroring the Rust `notify_approval`. It is a no-op (logged in Rust)
// when no waiter is pending for approvalID.
func (s *Session) NotifyApproval(approvalID string, decision protocol.ReviewDecision) {
	at := s.ActiveTurn()
	if at == nil || at.State == nil {
		return
	}
	at.State.ResolvePendingApproval(approvalID, decision)
}

// ----------------------------------------------------------------------------
// Request permissions
// ----------------------------------------------------------------------------

// RequestPermissionsForCwd routes a request_permissions tool call: it short
// circuits under policies that never prompt, otherwise emits a RequestPermissions
// event, registers a waiter keyed by call id, and awaits a matching
// RequestPermissionsResponse op. It returns nil only on cancellation; on timeout
// it returns the empty (deny) response.
//
// Faithful to the Rust `request_permissions_for_cwd` (non-guardian path):
//   - Never: auto-grant an empty profile for the turn.
//   - Granular without request_permissions allowed: auto-grant an empty profile.
//   - Otherwise: prompt the user.
func (s *Session) RequestPermissionsForCwd(
	ctx context.Context,
	tc *TurnContext,
	callID string,
	args protocol.RequestPermissionsArgs,
	cwd protocol.AbsolutePath,
) *protocol.RequestPermissionsResponse {
	switch tc.ApprovalPolicy.Kind {
	case protocol.AskForApprovalNever:
		resp := emptyPermissionsResponse()
		return &resp
	case protocol.AskForApprovalGranular:
		if !granularAllowsRequestPermissions(tc.ApprovalPolicy.Granular) {
			resp := emptyPermissionsResponse()
			return &resp
		}
	}

	reg := waitersFor(s)
	recv, replaced := reg.insertPendingPermissions(callID)
	if replaced != nil {
		close(replaced)
	}

	cwdPtr := cwd
	event := protocol.RequestPermissionsEvent{
		CallID:      callID,
		TurnID:      tc.SubID,
		StartedAtMs: nowUnixMillis(),
		Reason:      args.Reason,
		Permissions: args.Permissions,
		Cwd:         &cwdPtr,
	}
	s.SendEvent(tc.SubID, protocol.EventMsg{
		Type:               protocol.EventMsgKindRequestPermissions,
		RequestPermissions: &event,
	})

	timer := time.NewTimer(ApprovalRequestTimeout)
	defer timer.Stop()

	select {
	case resp, ok := <-recv:
		if !ok {
			return nil
		}
		return &resp
	case <-ctx.Done():
		// Detach the waiter so a later response is dropped; return nil (cancelled).
		reg.removePendingPermissions(callID)
		return nil
	case <-timer.C:
		reg.removePendingPermissions(callID)
		resp := emptyPermissionsResponse()
		return &resp
	}
}

// NotifyRequestPermissionsResponse resolves a pending request_permissions waiter,
// mirroring the Rust `notify_request_permissions_response`. It is a no-op when no
// waiter is pending for callID.
//
// STUB: response normalization against the originating cwd and granted-permission
// recording into the turn state are owned by the permissions area; here the
// response is delivered verbatim.
func (s *Session) NotifyRequestPermissionsResponse(callID string, response protocol.RequestPermissionsResponse) {
	reg := waitersFor(s)
	ch, ok := reg.removePendingPermissions(callID)
	if !ok {
		return
	}
	ch <- response
}

// ----------------------------------------------------------------------------
// Request user input
// ----------------------------------------------------------------------------

// RequestUserInput emits a RequestUserInput event, registers a waiter keyed by
// the turn's sub id, and awaits a matching UserInputAnswer op. It returns the
// empty (no-answers) response on cancellation or timeout.
func (s *Session) RequestUserInput(
	ctx context.Context,
	tc *TurnContext,
	callID string,
	args protocol.RequestUserInputArgs,
) protocol.RequestUserInputResponse {
	subID := tc.SubID

	reg := waitersFor(s)
	recv, replaced := reg.insertPendingUserInput(subID)
	if replaced != nil {
		close(replaced)
	}

	event := protocol.RequestUserInputEvent{
		CallID:    callID,
		TurnID:    subID,
		Questions: args.Questions,
	}
	s.SendEvent(subID, protocol.EventMsg{
		Type:             protocol.EventMsgKindRequestUserInput,
		RequestUserInput: &event,
	})

	timer := time.NewTimer(ApprovalRequestTimeout)
	defer timer.Stop()

	select {
	case resp, ok := <-recv:
		if !ok {
			return emptyUserInputResponse()
		}
		return resp
	case <-ctx.Done():
		reg.removePendingUserInput(subID)
		s.NotifyUserInputResponse(subID, emptyUserInputResponse())
		return emptyUserInputResponse()
	case <-timer.C:
		reg.removePendingUserInput(subID)
		return emptyUserInputResponse()
	}
}

// NotifyUserInputResponse resolves a pending request_user_input waiter, mirroring
// the Rust `notify_user_input_response`. It is a no-op when no waiter is pending
// for subID.
func (s *Session) NotifyUserInputResponse(subID string, response protocol.RequestUserInputResponse) {
	reg := waitersFor(s)
	ch, ok := reg.removePendingUserInput(subID)
	if !ok {
		return
	}
	ch <- response
}

// ----------------------------------------------------------------------------
// Elicitation
// ----------------------------------------------------------------------------

// RequestElicitation emits an ElicitationRequest event, registers a waiter keyed
// by the server name + request id, and awaits a matching ResolveElicitation op.
// It returns the cancel fallback on cancellation or timeout.
func (s *Session) RequestElicitation(
	ctx context.Context,
	tc *TurnContext,
	serverName string,
	id protocol.RequestId,
	request protocol.ElicitationRequest,
) ElicitationResponse {
	key := elicitationKey(serverName, id)

	reg := waitersFor(s)
	recv, replaced := reg.insertPendingElicitation(key)
	if replaced != nil {
		close(replaced)
	}

	turnID := tc.SubID
	event := protocol.ElicitationRequestEvent{
		TurnID:     &turnID,
		ServerName: serverName,
		ID:         id,
		Request:    request,
	}
	s.SendEvent(tc.SubID, protocol.EventMsg{
		Type:               protocol.EventMsgKindElicitationRequest,
		ElicitationRequest: &event,
	})

	timer := time.NewTimer(ApprovalRequestTimeout)
	defer timer.Stop()

	select {
	case resp, ok := <-recv:
		if !ok {
			return emptyElicitationResponse()
		}
		return resp
	case <-ctx.Done():
		reg.removePendingElicitation(key)
		return emptyElicitationResponse()
	case <-timer.C:
		reg.removePendingElicitation(key)
		return emptyElicitationResponse()
	}
}

// NotifyElicitationResponse resolves a pending elicitation waiter, mirroring the
// Rust `resolve_elicitation` (in-turn path). It reports whether a waiter was
// present; when false, the caller should fall back to the MCP connection manager
// (deferred here).
func (s *Session) NotifyElicitationResponse(serverName string, id protocol.RequestId, response ElicitationResponse) bool {
	reg := waitersFor(s)
	ch, ok := reg.removePendingElicitation(elicitationKey(serverName, id))
	if !ok {
		return false
	}
	ch <- response
	return true
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// networkPolicyAmendmentsFor derives the allow/deny network policy amendments
// offered for a network approval, mirroring the Rust mapping in
// request_command_approval. Returns nil when there is no network context.
func networkPolicyAmendmentsFor(ctx *protocol.NetworkApprovalContext) []protocol.NetworkPolicyAmendment {
	if ctx == nil {
		return nil
	}
	return []protocol.NetworkPolicyAmendment{
		{Host: ctx.Host, Action: protocol.NetworkPolicyRuleActionAllow},
		{Host: ctx.Host, Action: protocol.NetworkPolicyRuleActionDeny},
	}
}

// parseCommandBestEffort produces a best-effort structured interpretation of a
// command for the ExecApprovalRequest event. The full command parser lives in
// the tools/parsing area; here we emit a single Unknown entry carrying the
// joined command so the event shape matches the wire model.
//
// STUB: real command classification (read/list_files/search) is owned by the
// command-parsing area; this conservative placeholder never misclassifies.
func parseCommandBestEffort(command []string) []protocol.ParsedCommand {
	joined := ""
	for i, part := range command {
		if i > 0 {
			joined += " "
		}
		joined += part
	}
	return []protocol.ParsedCommand{protocol.NewUnknownParsedCommand(joined)}
}
