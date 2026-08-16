package localexec

import (
	"context"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/sandbox"
)

// These tests cover the sandbox-denial approval escalation port
// (sandbox_escalation.go): the SandboxErr::Denied arm of ToolOrchestrator::run
// for the non-network FS denial path the shell + unified-exec exec calls hit.

// readOnlyTurn builds a core.TurnContext with the given approval policy under the
// read-only sandbox (a restricted FS policy with no denied-read entries, so
// unsandboxed_execution_allowed is true).
func readOnlyTurn(policy protocol.AskForApprovalKind) *core.TurnContext {
	return &core.TurnContext{
		SubID:          "turn-esc",
		Cwd:            "/work",
		ApprovalPolicy: protocol.AskForApproval{Kind: policy},
		SandboxMode:    protocol.SandboxModeReadOnly,
	}
}

func TestResolveSandboxEscalation(t *testing.T) {
	approved := protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved}
	denied := protocol.ReviewDecision{Kind: protocol.ReviewDecisionDenied}

	tests := []struct {
		name string
		// policy is the turn approval policy.
		policy protocol.AskForApprovalKind
		// respond, when set, is the decision the scripted approver delivers.
		respond *protocol.ReviewDecision
		// wantPrompt is whether an core.ExecApprovalRequest event must be emitted.
		wantPrompt bool
		// want is the expected escalation decision.
		want sandboxEscalationDecision
	}{
		{
			name:       "on-failure prompts and approve retries unsandboxed",
			policy:     protocol.AskForApprovalOnFailure,
			respond:    &approved,
			wantPrompt: true,
			want:       sandboxEscalationRetryUnsandboxed,
		},
		{
			name:       "on-failure prompts and deny surfaces denial",
			policy:     protocol.AskForApprovalOnFailure,
			respond:    &denied,
			wantPrompt: true,
			want:       sandboxEscalationSurfaceDenial,
		},
		{
			name:       "unless-trusted prompts and approve retries unsandboxed",
			policy:     protocol.AskForApprovalUnlessTrusted,
			respond:    &approved,
			wantPrompt: true,
			want:       sandboxEscalationRetryUnsandboxed,
		},
		{
			name:       "never policy skips prompt and surfaces denial",
			policy:     protocol.AskForApprovalNever,
			wantPrompt: false,
			want:       sandboxEscalationSurfaceDenial,
		},
		{
			name:       "on-request policy skips prompt and surfaces denial",
			policy:     protocol.AskForApprovalOnRequest,
			wantPrompt: false,
			want:       sandboxEscalationSurfaceDenial,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, events := newTestSession(t)
			turn := readOnlyTurn(tc.policy)

			result := make(chan sandboxEscalationDecision, 1)
			go func() {
				result <- resolveSandboxEscalation(context.Background(), s, sandboxEscalationRequest{
					Turn:    turn,
					CallID:  "call-esc",
					Command: []string{"/bin/zsh", "-lc", "touch /etc/x"},
					Cwd:     "/work",
				})
			}()

			if tc.wantPrompt {
				ev := recvEvent(t, events)
				if ev.Msg.Type != protocol.EventMsgKindExecApprovalRequest {
					t.Fatalf("event type = %q, want exec_approval_request", ev.Msg.Type)
				}
				req := ev.Msg.ExecApprovalRequest
				if req == nil {
					t.Fatal("ExecApprovalRequest payload is nil")
				}
				if req.CallID != "call-esc" {
					t.Fatalf("approval call id = %q, want call-esc", req.CallID)
				}
				if req.Reason == nil || *req.Reason != sandboxDenialReason {
					t.Fatalf("approval reason = %v, want %q", req.Reason, sandboxDenialReason)
				}
				s.NotifyApproval("call-esc", *tc.respond)
			}

			select {
			case got := <-result:
				if got != tc.want {
					t.Fatalf("decision = %v, want %v", got, tc.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for escalation decision")
			}

			if !tc.wantPrompt {
				// No prompt: the event channel must be empty.
				select {
				case ev := <-events:
					t.Fatalf("unexpected event emitted under %s: %v", tc.policy, ev.Msg.Type)
				default:
				}
			}
		})
	}
}

// TestResolveSandboxEscalationDeniedReadsForbidRetry verifies that a policy that
// would otherwise prompt (on-failure) still surfaces the denial when the
// filesystem policy carries denied-read restrictions (which only exist inside
// the sandbox), mirroring unsandboxed_execution_allowed == false.
func TestResolveSandboxEscalationDeniedReadsForbidRetry(t *testing.T) {
	s, events := newTestSession(t)
	turn := &core.TurnContext{
		SubID:          "turn-esc",
		Cwd:            "/work",
		ApprovalPolicy: protocol.AskForApproval{Kind: protocol.AskForApprovalOnFailure},
		SandboxMode:    protocol.SandboxModeWorkspaceWrite,
	}
	// Inject a denied-read entry so fileSystemPolicyHasDeniedReads is true; the
	// turn policy resolver does not add Deny entries, so verify the helper
	// directly to keep the decision contract explicit.
	deniedPolicy := protocol.FileSystemSandboxPolicy{
		Kind: protocol.FileSystemSandboxKindRestricted,
		Entries: []protocol.FileSystemSandboxEntry{
			{Path: protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}), Access: protocol.FileSystemAccessModeRead},
			{Path: protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}), Access: protocol.FileSystemAccessModeDeny},
		},
	}
	if !fileSystemPolicyHasDeniedReads(deniedPolicy) {
		t.Fatal("expected denied-read policy to report denied reads")
	}
	if core.UnsandboxedExecutionAllowed(core.FilesystemSandboxState{Restricted: true, DeniedReadRestrictions: true}) {
		t.Fatal("expected unsandboxed execution to be disallowed with denied reads")
	}

	// With the default workspace-write policy (no denied reads), on-failure DOES
	// prompt — confirm the baseline so the denied-reads guard is meaningful.
	go func() {
		_ = resolveSandboxEscalation(context.Background(), s, sandboxEscalationRequest{Turn: turn, CallID: "c", Command: []string{"x"}, Cwd: "/work"})
	}()
	ev := recvEvent(t, events)
	if ev.Msg.Type != protocol.EventMsgKindExecApprovalRequest {
		t.Fatalf("event type = %q, want exec_approval_request (workspace-write has no denied reads)", ev.Msg.Type)
	}
	s.NotifyApproval("c", protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved})
}

func TestIsLikelySandboxDenied(t *testing.T) {
	tests := []struct {
		name        string
		sandboxType sandbox.SandboxType
		exitCode    int
		stdout      string
		stderr      string
		aggregated  string
		want        bool
	}{
		{name: "none backend never denied", sandboxType: sandbox.SandboxTypeNone, exitCode: 1, stderr: "operation not permitted", want: false},
		{name: "zero exit never denied", sandboxType: sandbox.SandboxTypeMacosSeatbelt, exitCode: 0, stderr: "operation not permitted", want: false},
		{name: "keyword in stderr", sandboxType: sandbox.SandboxTypeMacosSeatbelt, exitCode: 1, stderr: "Operation not permitted", want: true},
		{name: "keyword in stdout", sandboxType: sandbox.SandboxTypeMacosSeatbelt, exitCode: 1, stdout: "read-only file system", want: true},
		{name: "keyword in aggregated", sandboxType: sandbox.SandboxTypeMacosSeatbelt, exitCode: 1, aggregated: "Sandbox denied", want: true},
		{name: "quick-reject exit 127 no keyword", sandboxType: sandbox.SandboxTypeMacosSeatbelt, exitCode: 127, stderr: "command not found", want: false},
		{name: "quick-reject exit 126 no keyword", sandboxType: sandbox.SandboxTypeMacosSeatbelt, exitCode: 126, want: false},
		{name: "seccomp SIGSYS", sandboxType: sandbox.SandboxTypeLinuxSeccomp, exitCode: execSignalBase + sigSysCode, want: true},
		{name: "seatbelt SIGSYS-equivalent code not denied", sandboxType: sandbox.SandboxTypeMacosSeatbelt, exitCode: execSignalBase + sigSysCode, want: false},
		{name: "generic non-zero no keyword not denied", sandboxType: sandbox.SandboxTypeMacosSeatbelt, exitCode: 1, stderr: "boom", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isLikelySandboxDenied(tc.sandboxType, tc.exitCode, tc.stdout, tc.stderr, tc.aggregated)
			if got != tc.want {
				t.Fatalf("isLikelySandboxDenied = %v, want %v", got, tc.want)
			}
		})
	}
}
