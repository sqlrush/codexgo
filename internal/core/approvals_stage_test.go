package core

import (
	"context"
	"errors"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// stageHooks is a HooksEngine that also answers permission-request hooks and
// records tool decisions.
type stageHooks struct {
	decision  *PermissionRequestDecision
	hookErr   error
	recorded  []ApprovalResolutionSource
	lastRunID string
}

func (h *stageHooks) Fire(context.Context, HookEvent, any) error { return nil }
func (h *stageHooks) PermissionRequest(_ context.Context, _ *TurnContext, runID string, _ PermissionRequestPayload) (*PermissionRequestDecision, error) {
	h.lastRunID = runID
	return h.decision, h.hookErr
}
func (h *stageHooks) RecordToolDecision(_ *TurnContext, _ protocol.ToolName, _ string, _ protocol.ReviewDecision, source ApprovalResolutionSource) {
	h.recorded = append(h.recorded, source)
}

// stageReviewer is a ReviewerApprover returning a fixed decision.
type stageReviewer struct {
	decision protocol.ReviewDecision
	calls    int
}

func (r *stageReviewer) ReviewApproval(context.Context, *TurnContext, ApprovalAction, *string, *string) protocol.ReviewDecision {
	r.calls++
	return r.decision
}

func shellAction(id string) ApprovalAction {
	return ApprovalAction{Kind: ApprovalActionShell, ID: id, Command: []string{"rm", "-rf", "x"}, HookCommand: "rm -rf x", Cwd: "/work"}
}

func TestRequestApprovalHookRoute(t *testing.T) {
	sess, _ := newTestSession(t)
	hooks := &stageHooks{decision: &PermissionRequestDecision{Allow: true}}
	sess.services.HooksEngine = hooks
	tc := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})

	decision, err := sess.RequestApproval(context.Background(), shellAction("c1"), ApprovalContext{Turn: tc, CallID: "c1", ToolName: protocol.PlainToolName("shell_command")})
	if err != nil || decision.Kind != protocol.ReviewDecisionApproved {
		t.Fatalf("hook allow = %+v, %v; want approved", decision, err)
	}
	if hooks.lastRunID != "c1" || len(hooks.recorded) != 1 || hooks.recorded[0] != ApprovalResolvedByHook {
		t.Fatalf("hook run id %q, recorded %v; want c1 recorded by hook", hooks.lastRunID, hooks.recorded)
	}

	hooks.decision = &PermissionRequestDecision{Allow: false, Message: "blocked by policy"}
	retry := "sandbox denied"
	_, err = sess.RequestApproval(context.Background(), shellAction("c1"), ApprovalContext{Turn: tc, CallID: "c1", RetryReason: &retry})
	msg, rejected := RejectionMessage(err)
	if !rejected || msg != "blocked by policy" || !errors.Is(err, ErrApprovalRejected) {
		t.Fatalf("hook deny = %v, want rejection 'blocked by policy'", err)
	}
	if hooks.lastRunID != "c1:retry" {
		t.Fatalf("retry run id = %q, want c1:retry", hooks.lastRunID)
	}
}

func TestRequestApprovalReviewerRoute(t *testing.T) {
	sess, _ := newTestSession(t)
	reviewer := &stageReviewer{decision: protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved}}
	sess.services.Approver = reviewer
	tc := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})

	// Strict auto-review forces the reviewer even when the turn routes to the user.
	decision, err := sess.RequestApproval(context.Background(), shellAction("r1"), ApprovalContext{Turn: tc, CallID: "r1", StrictAutoReview: true})
	if err != nil || decision.Kind != protocol.ReviewDecisionApproved || reviewer.calls != 1 {
		t.Fatalf("strict review = %+v, %v (calls %d); want reviewer approval", decision, err, reviewer.calls)
	}
	// The turn's approvals_reviewer routes to the reviewer too.
	tc.ApprovalsReviewer = protocol.ApprovalsReviewerAutoReview
	reviewer.decision = protocol.NewReviewDecisionDeniedWith("reviewer said no")
	_, err = sess.RequestApproval(context.Background(), shellAction("r2"), ApprovalContext{Turn: tc, CallID: "r2"})
	if msg, ok := RejectionMessage(err); !ok || msg != "reviewer said no" || reviewer.calls != 2 {
		t.Fatalf("reviewer deny = %v (calls %d)", err, reviewer.calls)
	}
	// Timed out and abort map to rejections as well.
	reviewer.decision = protocol.ReviewDecision{Kind: protocol.ReviewDecisionTimedOut}
	if _, err := sess.RequestApproval(context.Background(), shellAction("r3"), ApprovalContext{Turn: tc, CallID: "r3"}); !errors.Is(err, ErrApprovalRejected) {
		t.Fatalf("timed out should reject, got %v", err)
	}
	// No reviewer wired but the route demands one: fail closed.
	sess.services.Approver = nil
	_, err = sess.RequestApproval(context.Background(), shellAction("r4"), ApprovalContext{Turn: tc, CallID: "r4"})
	if msg, ok := RejectionMessage(err); !ok || msg != "automatic approval review is not available" {
		t.Fatalf("missing reviewer should fail closed, got %v", err)
	}
}

func TestRequestApprovalUserRouteUsesCache(t *testing.T) {
	sess, events := newTestSession(t)
	tc := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	action := shellAction("u1")

	// First request prompts the user; approve for session.
	type res struct {
		d   protocol.ReviewDecision
		err error
	}
	done := make(chan res, 1)
	go func() {
		d, err := sess.RequestApproval(context.Background(), action, ApprovalContext{Turn: tc, CallID: "u1", ToolName: protocol.PlainToolName("shell_command")})
		done <- res{d, err}
	}()
	ev := recvEvent(t, events)
	if ev.Msg.Type != protocol.EventMsgKindExecApprovalRequest {
		t.Fatalf("expected an exec approval prompt, got %s", ev.Msg.Type)
	}
	sess.NotifyApproval("u1", protocol.ReviewDecision{Kind: protocol.ReviewDecisionApprovedForSession})
	first := <-done
	if first.err != nil || first.d.Kind != protocol.ReviewDecisionApprovedForSession {
		t.Fatalf("first approval = %+v, %v", first.d, first.err)
	}
	// Same action again: served from the approved-for-session cache, no prompt.
	action.ID = "u2"
	d, err := sess.RequestApproval(context.Background(), action, ApprovalContext{Turn: tc, CallID: "u2"})
	if err != nil || d.Kind != protocol.ReviewDecisionApprovedForSession {
		t.Fatalf("cached approval = %+v, %v", d, err)
	}
	select {
	case ev := <-events:
		t.Fatalf("cached approval must not prompt again, got %s", ev.Msg.Type)
	default:
	}
	// A different command prompts again and a user denial rejects.
	other := ApprovalAction{Kind: ApprovalActionShell, ID: "u3", Command: []string{"cat", "y"}, HookCommand: "cat y", Cwd: "/work"}
	go func() {
		d, err := sess.RequestApproval(context.Background(), other, ApprovalContext{Turn: tc, CallID: "u3"})
		done <- res{d, err}
	}()
	if ev := recvEvent(t, events); ev.Msg.Type != protocol.EventMsgKindExecApprovalRequest {
		t.Fatalf("expected a prompt for the new command, got %s", ev.Msg.Type)
	}
	sess.NotifyApproval("u3", protocol.ReviewDecision{Kind: protocol.ReviewDecisionDenied})
	third := <-done
	if _, rejected := RejectionMessage(third.err); !rejected {
		t.Fatalf("user denial should reject, got %v", third.err)
	}
}

func TestApplyPatchApprovalPreapprovedShortCircuits(t *testing.T) {
	sess, events := newTestSession(t)
	tc := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	action := ApprovalAction{Kind: ApprovalActionApplyPatch, ID: "p1", Cwd: "/work", Patch: "*** Begin Patch", Files: []string{"/work/a.txt"}, PermissionsPreapproved: true}
	d, err := sess.RequestApproval(context.Background(), action, ApprovalContext{Turn: tc, CallID: "p1"})
	if err != nil || d.Kind != protocol.ReviewDecisionApproved {
		t.Fatalf("preapproved patch = %+v, %v; want approved without a prompt", d, err)
	}
	select {
	case ev := <-events:
		t.Fatalf("preapproved patch must not prompt, got %s", ev.Msg.Type)
	default:
	}
}

func TestReviewDecisionDeniedRejectionWire(t *testing.T) {
	d := protocol.NewReviewDecisionDeniedWith("nope")
	raw, _ := d.MarshalJSON()
	if string(raw) != `{"denied":{"rejection":"nope"}}` {
		t.Fatalf("0.147 denied form = %s", raw)
	}
	var back protocol.ReviewDecision
	if err := back.UnmarshalJSON(raw); err != nil || back.Kind != protocol.ReviewDecisionDenied || back.Rejection == nil || *back.Rejection != "nope" {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
	if err := back.UnmarshalJSON([]byte(`"denied"`)); err != nil || back.Kind != protocol.ReviewDecisionDenied || back.Rejection != nil {
		t.Fatalf("0.136 bare denied should still parse: %+v, %v", back, err)
	}
}
