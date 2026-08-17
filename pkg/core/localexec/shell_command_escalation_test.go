package localexec

import (
	"context"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/sandbox"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// These tests cover the shell_command path's sandbox-denial approval escalation
// wiring (runShellWithEscalation): the first attempt runs sandboxed and, on a
// likely sandbox denial under a permitting policy, the path prompts for approval
// and retries WITHOUT the sandbox.

// scriptedEscalationExec is an ExecService that returns a sandbox-denial result
// on the first (sandboxed) attempt and a configurable result on the second
// (unsandboxed) retry. It records each request so the test can assert the retry
// ran with SandboxType none.
type scriptedEscalationExec struct {
	sandboxedResult ExecResult
	retryResult     ExecResult
	calls           []ExecRequest
}

func (s *scriptedEscalationExec) Run(_ context.Context, req ExecRequest) (ExecResult, error) {
	s.calls = append(s.calls, req)
	if req.SandboxType == sandbox.SandboxTypeNone {
		return s.retryResult, nil
	}
	return s.sandboxedResult, nil
}

// shellEscalationHandlerContext builds a handler context for a shell_command
// call under the given approval policy + read-only sandbox.
func shellEscalationHandlerContext(t *testing.T, sess *core.Session, policy protocol.AskForApprovalKind) *core.ToolHandlerContext {
	t.Helper()
	return &core.ToolHandlerContext{
		Session:  sess,
		Turn:     readOnlyTurn(policy),
		CallID:   "call-shell-esc",
		ToolName: protocol.PlainToolName("shell_command"),
		Payload:  tools.ToolPayload{Kind: tools.ToolPayloadKindFunction, Arguments: `{"command":"touch /etc/x"}`},
	}
}

func TestShellCommandEscalationApproveRetriesUnsandboxed(t *testing.T) {
	s, events := newTestSession(t)
	exec := &scriptedEscalationExec{
		// Sandboxed attempt: a likely denial (non-zero exit + denial keyword).
		sandboxedResult: ExecResult{ExitCode: 1, Stderr: "operation not permitted"},
		// Unsandboxed retry: success.
		retryResult: ExecResult{ExitCode: 0, Stdout: "ok"},
	}
	e := newShellCommandExecutor(exec, nil)
	h := shellEscalationHandlerContext(t, s, protocol.AskForApprovalOnFailure)

	result := make(chan tools.ToolOutput, 1)
	errCh := make(chan error, 1)
	go func() {
		out, err := e.Handle(context.Background(), h)
		result <- out
		errCh <- err
	}()

	// First event is the begin; then the approval prompt for the escalation.
	begin := recvEvent(t, events)
	if begin.Msg.Type != protocol.EventMsgKindExecCommandBegin {
		t.Fatalf("first event = %q, want exec_command_begin", begin.Msg.Type)
	}
	approval := recvEvent(t, events)
	if approval.Msg.Type != protocol.EventMsgKindExecApprovalRequest {
		t.Fatalf("second event = %q, want exec_approval_request", approval.Msg.Type)
	}
	s.NotifyApproval("call-shell-esc", protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved})

	// The end event reflects the FINAL (retry) result: exit 0, stdout "ok".
	end := recvEvent(t, events)
	if end.Msg.Type != protocol.EventMsgKindExecCommandEnd {
		t.Fatalf("third event = %q, want exec_command_end", end.Msg.Type)
	}
	if end.Msg.ExecCommandEnd.ExitCode != 0 {
		t.Fatalf("end exit code = %d, want 0 (escalated retry succeeded)", end.Msg.ExecCommandEnd.ExitCode)
	}

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle did not return")
	}
	out := <-result
	if out == nil {
		t.Fatal("expected a tool output from the escalated retry")
	}

	if len(exec.calls) != 2 {
		t.Fatalf("exec calls = %d, want 2 (sandboxed + unsandboxed retry)", len(exec.calls))
	}
	if exec.calls[0].SandboxType == sandbox.SandboxTypeNone {
		t.Fatal("first attempt should be sandboxed (read-only turn)")
	}
	if exec.calls[1].SandboxType != sandbox.SandboxTypeNone {
		t.Fatalf("retry SandboxType = %v, want none", exec.calls[1].SandboxType)
	}
}

func TestShellCommandEscalationDenySurfacesDenial(t *testing.T) {
	s, events := newTestSession(t)
	exec := &scriptedEscalationExec{
		sandboxedResult: ExecResult{ExitCode: 1, Stderr: "permission denied"},
		retryResult:     ExecResult{ExitCode: 0, Stdout: "should-not-run"},
	}
	e := newShellCommandExecutor(exec, nil)
	h := shellEscalationHandlerContext(t, s, protocol.AskForApprovalOnFailure)

	done := make(chan struct{})
	go func() {
		_, _ = e.Handle(context.Background(), h)
		close(done)
	}()

	if begin := recvEvent(t, events); begin.Msg.Type != protocol.EventMsgKindExecCommandBegin {
		t.Fatalf("first event = %q, want exec_command_begin", begin.Msg.Type)
	}
	if approval := recvEvent(t, events); approval.Msg.Type != protocol.EventMsgKindExecApprovalRequest {
		t.Fatalf("second event = %q, want exec_approval_request", approval.Msg.Type)
	}
	s.NotifyApproval("call-shell-esc", protocol.ReviewDecision{Kind: protocol.ReviewDecisionDenied})

	end := recvEvent(t, events)
	if end.Msg.Type != protocol.EventMsgKindExecCommandEnd {
		t.Fatalf("third event = %q, want exec_command_end", end.Msg.Type)
	}
	// Denial surfaced unchanged: the model sees the original sandboxed result.
	if end.Msg.ExecCommandEnd.ExitCode != 1 {
		t.Fatalf("end exit code = %d, want 1 (denial surfaced)", end.Msg.ExecCommandEnd.ExitCode)
	}

	<-done
	if len(exec.calls) != 1 {
		t.Fatalf("exec calls = %d, want 1 (no unsandboxed retry on deny)", len(exec.calls))
	}
}

func TestShellCommandEscalationNeverPolicySkipsPrompt(t *testing.T) {
	s, events := newTestSession(t)
	exec := &scriptedEscalationExec{
		sandboxedResult: ExecResult{ExitCode: 1, Stderr: "operation not permitted"},
		retryResult:     ExecResult{ExitCode: 0, Stdout: "should-not-run"},
	}
	e := newShellCommandExecutor(exec, nil)
	h := shellEscalationHandlerContext(t, s, protocol.AskForApprovalNever)

	done := make(chan struct{})
	go func() {
		_, _ = e.Handle(context.Background(), h)
		close(done)
	}()

	if begin := recvEvent(t, events); begin.Msg.Type != protocol.EventMsgKindExecCommandBegin {
		t.Fatalf("first event = %q, want exec_command_begin", begin.Msg.Type)
	}
	// Under never, no approval prompt: the next event is the end event directly.
	end := recvEvent(t, events)
	if end.Msg.Type != protocol.EventMsgKindExecCommandEnd {
		t.Fatalf("event = %q, want exec_command_end (never policy skips prompt)", end.Msg.Type)
	}
	if end.Msg.ExecCommandEnd.ExitCode != 1 {
		t.Fatalf("end exit code = %d, want 1 (denial surfaced under never)", end.Msg.ExecCommandEnd.ExitCode)
	}

	<-done
	if len(exec.calls) != 1 {
		t.Fatalf("exec calls = %d, want 1 (no retry under never)", len(exec.calls))
	}
}
