//go:build darwin

package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/sandbox"
	"github.com/sqlrush/codexgo/internal/tools"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
)

// TestUnifiedExecEscalationApproveRetriesUnsandboxed is an end-to-end behavioral
// test for the exec_command (UnifiedExec) sandbox-denial approval escalation on
// the real macOS Seatbelt backend: a write outside the workspace is DENIED under
// a read-only turn, the bridge prompts for approval (on-failure policy), and on
// approval the escalated retry runs WITHOUT the sandbox and the write succeeds.
// It mirrors the seatbelt/unified-exec test guards.
func TestUnifiedExecEscalationApproveRetriesUnsandboxed(t *testing.T) {
	requirePTY(t)
	if _, err := os.Stat(sandbox.MacosPathToSeatbeltExecutable); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}

	workspace := t.TempDir()
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", workspace, err)
	}
	outside := t.TempDir()
	outsideResolved, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", outside, err)
	}
	target := filepath.Join(outsideResolved, "escalated.txt")
	_ = os.Remove(target)

	executor := unifiedexec.NewExecutor(nil)
	t.Cleanup(executor.Shutdown)
	execEx := newUnifiedExecCommandExecutor(executor, nil)

	sess, events := newTestSession(t)
	tc := turnWithShellFeatures(resolved, true)
	tc.SandboxMode = protocol.SandboxModeReadOnly
	tc.ApprovalPolicy = protocol.AskForApproval{Kind: protocol.AskForApprovalOnFailure}

	done := make(chan error, 1)
	go func() {
		_, herr := execEx.Handle(context.Background(), &toolHandlerContext{
			Session: sess, Turn: tc, CallID: "c-esc", ToolName: execEx.Name(),
			Payload: tools.FunctionPayload(
				`{"cmd":": > ` + escapeForJSON(target) + `","yield_time_ms":3000}`),
		})
		done <- herr
	}()

	// Drain events until the escalation approval prompt arrives, then approve.
	deadline := time.After(20 * time.Second)
	approved := false
	for !approved {
		select {
		case ev := <-events:
			if ev.Msg.Type == protocol.EventMsgKindExecApprovalRequest {
				if ev.Msg.ExecApprovalRequest.CallID != "c-esc" {
					t.Fatalf("approval call id = %q, want c-esc", ev.Msg.ExecApprovalRequest.CallID)
				}
				sess.NotifyApproval("c-esc", protocol.ReviewDecision{Kind: protocol.ReviewDecisionApproved})
				approved = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for escalation approval prompt")
		}
	}

	select {
	case herr := <-done:
		if herr != nil {
			t.Fatalf("exec_command: %v", herr)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Handle did not return after approval")
	}

	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("escalated unsandboxed retry did not write %q: %v", target, statErr)
	}
}
