package core

import (
	"context"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file tests the approval-routing port (approvals.go), the approval
// decisioning primitives (approval_decision.go), and the auxiliary waiter
// registries (approval_waiters.go). The reference behavior is the
// request_command_approval / request_patch_approval / request_permissions_for_cwd
// / request_user_input / elicitation flows plus the AskForApproval x sandbox
// decisioning and ApprovedForSession memory from codex-rs/core.

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

// newTestSession builds a minimal Session with an active turn and a buffered
// event queue suitable for the synchronous approval-routing path. The returned
// channel receives every event the session emits. Tests run in package core so
// they may touch unexported Session fields directly.
func newTestSession(t *testing.T) (*Session, <-chan protocol.Event) {
	t.Helper()
	events := make(chan protocol.Event, 16)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &Session{
		txEvent:     events,
		state:       NewSessionState(SessionConfiguration{}),
		agentStatus: protocol.AgentStatus{Kind: protocol.AgentStatusPendingInit},
		ctx:         ctx,
		cancel:      cancel,
	}
	s.setActiveTurn(NewActiveTurn())
	t.Cleanup(func() { dropSessionWaiters(s) })
	return s, events
}

// withApprovalTimeout temporarily shortens the approval window so timeout paths
// can be exercised without sleeping for the full default duration. It restores
// the prior value on cleanup.
func withApprovalTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := ApprovalRequestTimeout
	ApprovalRequestTimeout = d
	t.Cleanup(func() { ApprovalRequestTimeout = prev })
}

// testTurnContext builds a TurnContext with the given approval policy and a
// fixed sub id used as the turn/correlation id.
func testTurnContext(policy protocol.AskForApproval) *TurnContext {
	return &TurnContext{SubID: "turn-1", Cwd: "/work", ApprovalPolicy: policy}
}

// recvEvent receives one event or fails after a short deadline.
func recvEvent(t *testing.T, events <-chan protocol.Event) protocol.Event {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return protocol.Event{}
	}
}

func approvalStrPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Exec (command) approval
// ---------------------------------------------------------------------------

// execApprovalCase is a single table row for TestRequestCommandApproval.
type execApprovalCase struct {
	name       string
	approvalID *string
	// respond, when set, is the decision the "user" delivers via NotifyApproval.
	respond *protocol.ReviewDecision
	// cancelBefore cancels the context instead of responding.
	cancelBefore bool
	// shortTimeout forces the timeout branch.
	shortTimeout bool
	wantKind     protocol.ReviewDecisionKind
	// wantApprovalKey is the correlation key the response must target.
	wantApprovalKey string
}

// effectiveKey returns the approval correlation key the test expects.
func (tc execApprovalCase) effectiveKey() string {
	if tc.approvalID != nil {
		return *tc.approvalID
	}
	return "call-1"
}

func TestRequestCommandApproval(t *testing.T) {
	approved := protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved}
	denied := protocol.ReviewDecision{Kind: protocol.ReviewDecisionDenied}

	tests := []execApprovalCase{
		{
			name:            "approved via call id",
			respond:         &approved,
			wantKind:        protocol.ReviewDecisionApproved,
			wantApprovalKey: "call-1",
		},
		{
			name:            "denied via call id",
			respond:         &denied,
			wantKind:        protocol.ReviewDecisionDenied,
			wantApprovalKey: "call-1",
		},
		{
			name:            "approval id overrides call id",
			approvalID:      approvalStrPtr("approval-9"),
			respond:         &approved,
			wantKind:        protocol.ReviewDecisionApproved,
			wantApprovalKey: "approval-9",
		},
		{
			name:         "cancellation yields abort",
			cancelBefore: true,
			wantKind:     protocol.ReviewDecisionAbort,
		},
		{
			name:         "timeout yields timed out",
			shortTimeout: true,
			wantKind:     protocol.ReviewDecisionTimedOut,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.shortTimeout {
				withApprovalTimeout(t, 20*time.Millisecond)
			}
			s, events := newTestSession(t)
			turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			result := make(chan protocol.ReviewDecision, 1)
			go func() {
				result <- s.RequestCommandApproval(ctx, turn, ExecApprovalRequest{
					CallID:     "call-1",
					ApprovalID: tc.approvalID,
					Command:    []string{"echo", "hi"},
					Cwd:        protocol.AbsolutePath("/work"),
				})
			}()

			ev := recvEvent(t, events)
			if ev.Msg.Type != protocol.EventMsgKindExecApprovalRequest {
				t.Fatalf("event type = %q, want exec_approval_request", ev.Msg.Type)
			}
			if ev.ID != turn.SubID {
				t.Fatalf("event id = %q, want %q", ev.ID, turn.SubID)
			}
			gotEv := ev.Msg.ExecApprovalRequest
			if gotEv == nil {
				t.Fatal("ExecApprovalRequest payload is nil")
			}
			if gotEv.EffectiveApprovalID() != tc.effectiveKey() {
				t.Fatalf("effective approval id = %q, want %q", gotEv.EffectiveApprovalID(), tc.effectiveKey())
			}
			if gotEv.AvailableDecisions == nil || len(*gotEv.AvailableDecisions) == 0 {
				t.Fatal("expected default available decisions to be populated")
			}

			switch {
			case tc.cancelBefore:
				cancel()
			case tc.shortTimeout:
				// let the timer fire
			default:
				s.NotifyApproval(tc.wantApprovalKey, *tc.respond)
			}

			got := <-result
			if got.Kind != tc.wantKind {
				t.Fatalf("decision = %q, want %q", got.Kind, tc.wantKind)
			}
		})
	}
}

func TestRequestCommandApprovalNoActiveTurn(t *testing.T) {
	s, events := newTestSession(t)
	s.setActiveTurn(nil) // no task running: nobody can deliver a response

	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	got := s.RequestCommandApproval(context.Background(), turn, ExecApprovalRequest{
		CallID:  "call-x",
		Command: []string{"ls"},
		Cwd:     protocol.AbsolutePath("/work"),
	})
	if got.Kind != protocol.ReviewDecisionAbort {
		t.Fatalf("decision = %q, want abort when no active turn", got.Kind)
	}
	// The request event is still emitted before the immediate abort.
	ev := recvEvent(t, events)
	if ev.Msg.Type != protocol.EventMsgKindExecApprovalRequest {
		t.Fatalf("event type = %q, want exec_approval_request", ev.Msg.Type)
	}
}

func TestRequestCommandApprovalNetworkDecisions(t *testing.T) {
	s, events := newTestSession(t)
	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	netCtx := &protocol.NetworkApprovalContext{
		Host:     "example.com",
		Protocol: protocol.NetworkApprovalProtocolHTTPS,
	}

	approved := protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved}
	go s.RequestCommandApproval(context.Background(), turn, ExecApprovalRequest{
		CallID:                 "call-net",
		Command:                []string{"curl", "https://example.com"},
		Cwd:                    protocol.AbsolutePath("/work"),
		NetworkApprovalContext: netCtx,
	})

	ev := recvEvent(t, events)
	got := ev.Msg.ExecApprovalRequest
	if got == nil {
		t.Fatal("ExecApprovalRequest payload is nil")
	}
	if got.NetworkApprovalContext == nil || got.NetworkApprovalContext.Host != "example.com" {
		t.Fatalf("network context not propagated: %+v", got.NetworkApprovalContext)
	}
	if got.ProposedNetworkPolicyAmendments == nil || len(*got.ProposedNetworkPolicyAmendments) != 2 {
		t.Fatalf("expected allow+deny network policy amendments, got %+v", got.ProposedNetworkPolicyAmendments)
	}
	// The default decision set for a network approval must include the allow
	// amendment plus session approval and abort.
	var sawNetAmendment, sawSession bool
	for _, d := range *got.AvailableDecisions {
		switch d.Kind {
		case protocol.ReviewDecisionNetworkPolicyAmendment:
			sawNetAmendment = true
		case protocol.ReviewDecisionApprovedForSession:
			sawSession = true
		}
	}
	if !sawNetAmendment || !sawSession {
		t.Fatalf("network decisions missing amendment/session: %+v", *got.AvailableDecisions)
	}

	s.NotifyApproval("call-net", approved)
}

// ---------------------------------------------------------------------------
// Patch approval
// ---------------------------------------------------------------------------

func TestRequestPatchApproval(t *testing.T) {
	tests := []struct {
		name         string
		respond      *protocol.ReviewDecision
		cancelBefore bool
		wantKind     protocol.ReviewDecisionKind
	}{
		{
			name:     "approved",
			respond:  &protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved},
			wantKind: protocol.ReviewDecisionApproved,
		},
		{
			name:         "cancel aborts",
			cancelBefore: true,
			wantKind:     protocol.ReviewDecisionAbort,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, events := newTestSession(t)
			turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			changes := map[string]protocol.FileChange{
				"a.txt": {Kind: protocol.FileChangeKindAdd, Content: "hello"},
			}
			result := make(chan protocol.ReviewDecision, 1)
			go func() {
				result <- s.RequestPatchApproval(ctx, turn, "patch-1", changes, approvalStrPtr("why"), approvalStrPtr("/work"))
			}()

			ev := recvEvent(t, events)
			if ev.Msg.Type != protocol.EventMsgKindApplyPatchApprovalRequest {
				t.Fatalf("event type = %q, want apply_patch_approval_request", ev.Msg.Type)
			}
			pe := ev.Msg.ApplyPatchApprovalRequest
			if pe == nil || pe.CallID != "patch-1" {
				t.Fatalf("patch event call id = %+v, want patch-1", pe)
			}
			if pe.GrantRoot == nil || *pe.GrantRoot != "/work" {
				t.Fatalf("grant root = %v, want /work", pe.GrantRoot)
			}

			if tc.cancelBefore {
				cancel()
			} else {
				s.NotifyApproval("patch-1", *tc.respond)
			}

			got := <-result
			if got.Kind != tc.wantKind {
				t.Fatalf("decision = %q, want %q", got.Kind, tc.wantKind)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Request permissions
// ---------------------------------------------------------------------------

func TestRequestPermissionsForCwdShortCircuit(t *testing.T) {
	tests := []struct {
		name   string
		policy protocol.AskForApproval
		// wantPrompt is true when the policy should emit an event and prompt.
		wantPrompt bool
	}{
		{
			name:       "never auto-grants without prompting",
			policy:     protocol.AskForApproval{Kind: protocol.AskForApprovalNever},
			wantPrompt: false,
		},
		{
			name:       "granular without request_permissions auto-grants",
			policy:     protocol.NewAskForApprovalGranular(protocol.GranularApprovalConfig{RequestPermissions: false}),
			wantPrompt: false,
		},
		{
			name:       "granular with request_permissions prompts",
			policy:     protocol.NewAskForApprovalGranular(protocol.GranularApprovalConfig{RequestPermissions: true}),
			wantPrompt: true,
		},
		{
			name:       "on-request prompts",
			policy:     protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest},
			wantPrompt: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, events := newTestSession(t)
			turn := testTurnContext(tc.policy)
			args := protocol.RequestPermissionsArgs{Reason: approvalStrPtr("need fs")}

			if !tc.wantPrompt {
				resp := s.RequestPermissionsForCwd(context.Background(), turn, "p-1", args, protocol.AbsolutePath("/work"))
				if resp == nil {
					t.Fatal("expected non-nil auto-grant response")
				}
				if resp.Scope != protocol.PermissionGrantScopeTurn {
					t.Fatalf("scope = %q, want turn", resp.Scope)
				}
				select {
				case ev := <-events:
					t.Fatalf("unexpected event emitted on short-circuit: %v", ev.Msg.Type)
				default:
				}
				return
			}

			result := make(chan *protocol.RequestPermissionsResponse, 1)
			go func() {
				result <- s.RequestPermissionsForCwd(context.Background(), turn, "p-1", args, protocol.AbsolutePath("/work"))
			}()
			ev := recvEvent(t, events)
			if ev.Msg.Type != protocol.EventMsgKindRequestPermissions {
				t.Fatalf("event type = %q, want request_permissions", ev.Msg.Type)
			}
			rp := ev.Msg.RequestPermissions
			if rp == nil || rp.CallID != "p-1" {
				t.Fatalf("request permissions event = %+v, want call id p-1", rp)
			}
			if rp.Cwd == nil || *rp.Cwd != protocol.AbsolutePath("/work") {
				t.Fatalf("cwd = %v, want /work", rp.Cwd)
			}

			want := protocol.RequestPermissionsResponse{Scope: protocol.PermissionGrantScopeTurn, StrictAutoReview: true}
			s.NotifyRequestPermissionsResponse("p-1", want)

			got := <-result
			if got == nil || !got.StrictAutoReview {
				t.Fatalf("response = %+v, want delivered response with StrictAutoReview", got)
			}
		})
	}
}

func TestRequestPermissionsForCwdCancellation(t *testing.T) {
	s, events := newTestSession(t)
	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	ctx, cancel := context.WithCancel(context.Background())

	result := make(chan *protocol.RequestPermissionsResponse, 1)
	go func() {
		result <- s.RequestPermissionsForCwd(ctx, turn, "p-c", protocol.RequestPermissionsArgs{}, protocol.AbsolutePath("/work"))
	}()
	recvEvent(t, events)
	cancel()

	got := <-result
	if got != nil {
		t.Fatalf("response = %+v, want nil on cancellation", got)
	}
}

func TestRequestPermissionsForCwdTimeout(t *testing.T) {
	withApprovalTimeout(t, 20*time.Millisecond)
	s, events := newTestSession(t)
	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})

	result := make(chan *protocol.RequestPermissionsResponse, 1)
	go func() {
		result <- s.RequestPermissionsForCwd(context.Background(), turn, "p-t", protocol.RequestPermissionsArgs{}, protocol.AbsolutePath("/work"))
	}()
	recvEvent(t, events)

	got := <-result
	if got == nil {
		t.Fatal("expected empty deny response on timeout, got nil")
	}
	if got.Scope != protocol.PermissionGrantScopeTurn {
		t.Fatalf("timeout response scope = %q, want turn", got.Scope)
	}
}

func TestNotifyRequestPermissionsResponseNoWaiter(t *testing.T) {
	s, _ := newTestSession(t)
	// No waiter is pending; the notify must be a no-op and must not panic/block.
	s.NotifyRequestPermissionsResponse("missing", protocol.RequestPermissionsResponse{})
}

// ---------------------------------------------------------------------------
// Request user input
// ---------------------------------------------------------------------------

func TestRequestUserInput(t *testing.T) {
	s, events := newTestSession(t)
	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	args := protocol.RequestUserInputArgs{
		Questions: []protocol.RequestUserInputQuestion{{ID: "q1", Question: "color?"}},
	}

	result := make(chan protocol.RequestUserInputResponse, 1)
	go func() {
		result <- s.RequestUserInput(context.Background(), turn, "call-ui", args)
	}()

	ev := recvEvent(t, events)
	if ev.Msg.Type != protocol.EventMsgKindRequestUserInput {
		t.Fatalf("event type = %q, want request_user_input", ev.Msg.Type)
	}
	ue := ev.Msg.RequestUserInput
	if ue == nil || ue.CallID != "call-ui" || ue.TurnID != turn.SubID {
		t.Fatalf("user input event = %+v, want call-ui/%s", ue, turn.SubID)
	}

	answer := protocol.RequestUserInputResponse{
		Answers: map[string]protocol.RequestUserInputAnswer{"q1": {Answers: []string{"blue"}}},
	}
	// The user-input waiter is keyed by the turn sub id.
	s.NotifyUserInputResponse(turn.SubID, answer)

	got := <-result
	if got.Answers["q1"].Answers[0] != "blue" {
		t.Fatalf("answer = %+v, want blue", got.Answers)
	}
}

func TestRequestUserInputCancellation(t *testing.T) {
	s, events := newTestSession(t)
	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	ctx, cancel := context.WithCancel(context.Background())

	result := make(chan protocol.RequestUserInputResponse, 1)
	go func() {
		result <- s.RequestUserInput(ctx, turn, "call-ui", protocol.RequestUserInputArgs{})
	}()
	recvEvent(t, events)
	cancel()

	got := <-result
	if len(got.Answers) != 0 {
		t.Fatalf("answers = %+v, want empty on cancellation", got.Answers)
	}
}

func TestRequestUserInputTimeout(t *testing.T) {
	withApprovalTimeout(t, 20*time.Millisecond)
	s, events := newTestSession(t)
	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})

	result := make(chan protocol.RequestUserInputResponse, 1)
	go func() {
		result <- s.RequestUserInput(context.Background(), turn, "call-ui", protocol.RequestUserInputArgs{})
	}()
	recvEvent(t, events)

	got := <-result
	if got.Answers == nil || len(got.Answers) != 0 {
		t.Fatalf("answers = %+v, want empty non-nil map on timeout", got.Answers)
	}
}

func TestNotifyUserInputResponseNoWaiter(t *testing.T) {
	s, _ := newTestSession(t)
	s.NotifyUserInputResponse("missing", emptyUserInputResponse())
}

// ---------------------------------------------------------------------------
// Elicitation
// ---------------------------------------------------------------------------

func TestRequestElicitation(t *testing.T) {
	s, events := newTestSession(t)
	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	id := protocol.StringRequestId("req-1")
	req := protocol.ElicitationRequest{Mode: protocol.ElicitationModeForm, Message: "fill in"}

	result := make(chan ElicitationResponse, 1)
	go func() {
		result <- s.RequestElicitation(context.Background(), turn, "srv", id, req)
	}()

	ev := recvEvent(t, events)
	if ev.Msg.Type != protocol.EventMsgKindElicitationRequest {
		t.Fatalf("event type = %q, want elicitation_request", ev.Msg.Type)
	}
	ee := ev.Msg.ElicitationRequest
	if ee == nil || ee.ServerName != "srv" || ee.ID.String() != "req-1" {
		t.Fatalf("elicitation event = %+v, want srv/req-1", ee)
	}
	if ee.TurnID == nil || *ee.TurnID != turn.SubID {
		t.Fatalf("elicitation turn id = %v, want %s", ee.TurnID, turn.SubID)
	}

	resp := ElicitationResponse{Decision: protocol.ElicitationAccept, Content: []byte(`{"x":1}`)}
	if !s.NotifyElicitationResponse("srv", id, resp) {
		t.Fatal("NotifyElicitationResponse reported no waiter, want true")
	}

	got := <-result
	if got.Decision != protocol.ElicitationAccept {
		t.Fatalf("decision = %q, want accept", got.Decision)
	}
}

func TestRequestElicitationCancellation(t *testing.T) {
	s, events := newTestSession(t)
	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	ctx, cancel := context.WithCancel(context.Background())
	id := protocol.StringRequestId("req-c")

	result := make(chan ElicitationResponse, 1)
	go func() {
		result <- s.RequestElicitation(ctx, turn, "srv", id, protocol.ElicitationRequest{Mode: protocol.ElicitationModeForm})
	}()
	recvEvent(t, events)
	cancel()

	got := <-result
	if got.Decision != protocol.ElicitationCancel {
		t.Fatalf("decision = %q, want cancel on cancellation", got.Decision)
	}
}

func TestRequestElicitationTimeout(t *testing.T) {
	withApprovalTimeout(t, 20*time.Millisecond)
	s, events := newTestSession(t)
	turn := testTurnContext(protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest})
	id := protocol.StringRequestId("req-t")

	result := make(chan ElicitationResponse, 1)
	go func() {
		result <- s.RequestElicitation(context.Background(), turn, "srv", id, protocol.ElicitationRequest{Mode: protocol.ElicitationModeForm})
	}()
	recvEvent(t, events)

	got := <-result
	if got.Decision != protocol.ElicitationCancel {
		t.Fatalf("decision = %q, want cancel on timeout", got.Decision)
	}
}

func TestNotifyElicitationResponseNoWaiter(t *testing.T) {
	s, _ := newTestSession(t)
	id := protocol.StringRequestId("nope")
	if s.NotifyElicitationResponse("srv", id, emptyElicitationResponse()) {
		t.Fatal("NotifyElicitationResponse reported a waiter, want false (none pending)")
	}
}

func TestElicitationKeyDistinguishesServerAndID(t *testing.T) {
	a := elicitationKey("srv", protocol.StringRequestId("1"))
	b := elicitationKey("sr", protocol.StringRequestId("v1"))
	if a == b {
		t.Fatalf("elicitation keys collided: %q == %q", a, b)
	}
	// Same server + same id must be stable for correlation.
	if elicitationKey("srv", protocol.IntegerRequestId(7)) != elicitationKey("srv", protocol.IntegerRequestId(7)) {
		t.Fatal("elicitation key is not stable for equal inputs")
	}
}

// ---------------------------------------------------------------------------
// ApprovalStore / WithCachedApproval (ApprovedForSession memory)
// ---------------------------------------------------------------------------

func TestApprovalStoreGetPut(t *testing.T) {
	store := NewApprovalStore()
	if _, ok := store.Get("k"); ok {
		t.Fatal("expected miss on empty store")
	}
	store.Put("k", protocol.ReviewDecision{Kind: protocol.ReviewDecisionApprovedForSession})
	got, ok := store.Get("k")
	if !ok || got.Kind != protocol.ReviewDecisionApprovedForSession {
		t.Fatalf("get = (%+v,%v), want approved_for_session hit", got, ok)
	}
}

func TestWithCachedApproval(t *testing.T) {
	session := protocol.ReviewDecision{Kind: protocol.ReviewDecisionApprovedForSession}
	approved := protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved}

	t.Run("all cached skips fetch", func(t *testing.T) {
		store := NewApprovalStore()
		store.Put("a", session)
		store.Put("b", session)
		called := false
		got := store.WithCachedApproval(context.Background(), []any{"a", "b"}, func(context.Context) protocol.ReviewDecision {
			called = true
			return approved
		})
		if called {
			t.Fatal("fetch invoked despite all keys cached")
		}
		if got.Kind != protocol.ReviewDecisionApprovedForSession {
			t.Fatalf("decision = %q, want approved_for_session", got.Kind)
		}
	})

	t.Run("miss invokes fetch and records session grant", func(t *testing.T) {
		store := NewApprovalStore()
		store.Put("a", session) // partial: "b" missing
		called := false
		got := store.WithCachedApproval(context.Background(), []any{"a", "b"}, func(context.Context) protocol.ReviewDecision {
			called = true
			return session
		})
		if !called {
			t.Fatal("fetch not invoked despite a missing key")
		}
		if got.Kind != protocol.ReviewDecisionApprovedForSession {
			t.Fatalf("decision = %q, want approved_for_session", got.Kind)
		}
		// The session grant must now be recorded for every key.
		if d, ok := store.Get("b"); !ok || d.Kind != protocol.ReviewDecisionApprovedForSession {
			t.Fatalf("key b not recorded after session grant: (%+v,%v)", d, ok)
		}
	})

	t.Run("non-session decision is not recorded", func(t *testing.T) {
		store := NewApprovalStore()
		got := store.WithCachedApproval(context.Background(), []any{"a"}, func(context.Context) protocol.ReviewDecision {
			return approved
		})
		if got.Kind != protocol.ReviewDecisionApproved {
			t.Fatalf("decision = %q, want approved", got.Kind)
		}
		if _, ok := store.Get("a"); ok {
			t.Fatal("non-session decision must not be cached")
		}
	})

	t.Run("empty keys always fetches", func(t *testing.T) {
		store := NewApprovalStore()
		called := false
		store.WithCachedApproval(context.Background(), nil, func(context.Context) protocol.ReviewDecision {
			called = true
			return approved
		})
		if !called {
			t.Fatal("fetch not invoked for empty key list")
		}
	})
}

// ---------------------------------------------------------------------------
// AskForApproval x sandbox decisioning
// ---------------------------------------------------------------------------

func TestDefaultExecApprovalRequirement(t *testing.T) {
	restricted := FilesystemSandboxState{Restricted: true}
	unrestricted := FilesystemSandboxState{Restricted: false}
	granularWith := protocol.NewAskForApprovalGranular(protocol.GranularApprovalConfig{SandboxApproval: true})
	granularWithout := protocol.NewAskForApprovalGranular(protocol.GranularApprovalConfig{SandboxApproval: false})

	tests := []struct {
		name   string
		policy protocol.AskForApproval
		fs     FilesystemSandboxState
		want   ExecApprovalRequirementKind
	}{
		{"never never asks", protocol.AskForApproval{Kind: protocol.AskForApprovalNever}, restricted, ExecApprovalSkip},
		{"on-failure never asks", protocol.AskForApproval{Kind: protocol.AskForApprovalOnFailure}, restricted, ExecApprovalSkip},
		{"on-request unrestricted skips", protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest}, unrestricted, ExecApprovalSkip},
		{"on-request restricted asks", protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest}, restricted, ExecApprovalNeedsApproval},
		{"unless-trusted always asks", protocol.AskForApproval{Kind: protocol.AskForApprovalUnlessTrusted}, unrestricted, ExecApprovalNeedsApproval},
		{"granular restricted with sandbox approval asks", granularWith, restricted, ExecApprovalNeedsApproval},
		{"granular restricted without sandbox approval forbidden", granularWithout, restricted, ExecApprovalForbidden},
		{"granular unrestricted skips", granularWithout, unrestricted, ExecApprovalSkip},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultExecApprovalRequirement(tc.policy, tc.fs)
			if got.Kind != tc.want {
				t.Fatalf("requirement kind = %v, want %v", got.Kind, tc.want)
			}
			if tc.want == ExecApprovalForbidden && got.Reason == nil {
				t.Fatal("forbidden requirement must carry a reason")
			}
		})
	}
}

func TestSandboxOverrideForFirstAttempt(t *testing.T) {
	skipBypass := ExecApprovalRequirement{Kind: ExecApprovalSkip, BypassSandbox: true}
	skipNoBypass := ExecApprovalRequirement{Kind: ExecApprovalSkip, BypassSandbox: false}
	allowUnsandboxed := FilesystemSandboxState{DeniedReadRestrictions: false}
	deniedReads := FilesystemSandboxState{DeniedReadRestrictions: true}

	tests := []struct {
		name      string
		escalated bool
		req       ExecApprovalRequirement
		fs        FilesystemSandboxState
		want      SandboxOverride
	}{
		{"denied reads force no override", false, skipBypass, deniedReads, SandboxNoOverride},
		{"skip+bypass full trust bypasses", false, skipBypass, allowUnsandboxed, SandboxBypassFirstAttempt},
		{"escalated permissions bypass", true, skipNoBypass, allowUnsandboxed, SandboxBypassFirstAttempt},
		{"default keeps sandbox", false, skipNoBypass, allowUnsandboxed, SandboxNoOverride},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SandboxOverrideForFirstAttempt(tc.escalated, tc.req, tc.fs)
			if got != tc.want {
				t.Fatalf("override = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DefaultAvailableDecisions
// ---------------------------------------------------------------------------

func TestDefaultAvailableDecisions(t *testing.T) {
	netCtx := &protocol.NetworkApprovalContext{Host: "h", Protocol: protocol.NetworkApprovalProtocolHTTPS}
	netAmendments := []protocol.NetworkPolicyAmendment{
		{Host: "h", Action: protocol.NetworkPolicyRuleActionAllow},
		{Host: "h", Action: protocol.NetworkPolicyRuleActionDeny},
	}
	execAmendment := &protocol.ExecPolicyAmendment{}
	addlPerms := &protocol.AdditionalPermissionProfile{}

	t.Run("network offers session + allow amendment + abort", func(t *testing.T) {
		got := DefaultAvailableDecisions(netCtx, nil, netAmendments, nil)
		kinds := decisionKinds(got)
		mustContain(t, kinds, protocol.ReviewDecisionApproved)
		mustContain(t, kinds, protocol.ReviewDecisionApprovedForSession)
		mustContain(t, kinds, protocol.ReviewDecisionNetworkPolicyAmendment)
		mustContain(t, kinds, protocol.ReviewDecisionAbort)
	})

	t.Run("additional permissions offer approved + abort only", func(t *testing.T) {
		got := DefaultAvailableDecisions(nil, nil, nil, addlPerms)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2 (approved, abort)", len(got))
		}
		kinds := decisionKinds(got)
		mustContain(t, kinds, protocol.ReviewDecisionApproved)
		mustContain(t, kinds, protocol.ReviewDecisionAbort)
	})

	t.Run("execpolicy amendment offered when present", func(t *testing.T) {
		got := DefaultAvailableDecisions(nil, execAmendment, nil, nil)
		kinds := decisionKinds(got)
		mustContain(t, kinds, protocol.ReviewDecisionApproved)
		mustContain(t, kinds, protocol.ReviewDecisionApprovedExecpolicyAmendment)
		mustContain(t, kinds, protocol.ReviewDecisionAbort)
	})

	t.Run("plain offers approved + abort", func(t *testing.T) {
		got := DefaultAvailableDecisions(nil, nil, nil, nil)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2 (approved, abort)", len(got))
		}
	})
}

func decisionKinds(ds []protocol.ReviewDecision) map[protocol.ReviewDecisionKind]bool {
	out := make(map[protocol.ReviewDecisionKind]bool, len(ds))
	for _, d := range ds {
		out[d.Kind] = true
	}
	return out
}

func mustContain(t *testing.T, kinds map[protocol.ReviewDecisionKind]bool, want protocol.ReviewDecisionKind) {
	t.Helper()
	if !kinds[want] {
		t.Fatalf("decision set missing %q", want)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func TestNetworkPolicyAmendmentsFor(t *testing.T) {
	if got := networkPolicyAmendmentsFor(nil); got != nil {
		t.Fatalf("nil context = %+v, want nil", got)
	}
	got := networkPolicyAmendmentsFor(&protocol.NetworkApprovalContext{Host: "h"})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (allow, deny)", len(got))
	}
	if got[0].Action != protocol.NetworkPolicyRuleActionAllow || got[1].Action != protocol.NetworkPolicyRuleActionDeny {
		t.Fatalf("amendments = %+v, want allow then deny", got)
	}
}

func TestExecApprovalRequestEffectiveID(t *testing.T) {
	if got := (ExecApprovalRequest{CallID: "c"}).EffectiveApprovalID(); got != "c" {
		t.Fatalf("effective id = %q, want c", got)
	}
	if got := (ExecApprovalRequest{CallID: "c", ApprovalID: approvalStrPtr("a")}).EffectiveApprovalID(); got != "a" {
		t.Fatalf("effective id = %q, want a (override)", got)
	}
}

func TestParseCommandBestEffort(t *testing.T) {
	got := parseCommandBestEffort([]string{"echo", "hi there"})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	// The joined command must round-trip the argv (best-effort Unknown entry).
	if got[0].Type != protocol.ParsedCommandUnknown || got[0].Cmd != "echo hi there" {
		t.Fatalf("parsed = %+v, want joined unknown command", got[0])
	}
}

func TestFallbackResponses(t *testing.T) {
	if r := emptyPermissionsResponse(); r.Scope != protocol.PermissionGrantScopeTurn || r.StrictAutoReview {
		t.Fatalf("empty permissions = %+v, want turn scope, no strict review", r)
	}
	if r := emptyUserInputResponse(); r.Answers == nil || len(r.Answers) != 0 {
		t.Fatalf("empty user input = %+v, want empty non-nil answers", r)
	}
	if r := emptyElicitationResponse(); r.Decision != protocol.ElicitationCancel {
		t.Fatalf("empty elicitation = %+v, want cancel", r)
	}
}
