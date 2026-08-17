package localexec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/unifiedexec"
	"github.com/sqlrush/codexgo/internal/utils/truncation"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/core/coretest"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// drainForExecEnd collects events until it sees an exec_command_end (or times
// out), returning that event. It tolerates the begin/output-delta events that
// precede the late end.
func drainForExecEnd(t *testing.T, events <-chan protocol.Event) *protocol.ExecCommandEndEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Msg.Type == protocol.EventMsgKindExecCommandEnd {
				return ev.Msg.ExecCommandEnd
			}
		case <-deadline:
			t.Fatal("timed out waiting for late exec_command_end")
			return nil
		}
	}
}

// TestUnifiedExecLateExecEnd opens a live unified-exec session through the
// exec_command tool, lets the tool call return (the process keeps running), then
// kills the session AFTER the call returned. The background watcher must emit a
// late exec_command_end tagged with the ORIGINATING call id, the
// unified_exec_startup source, and the live session id. Mirrors
// async_watcher::spawn_exit_watcher's emit_exec_end_for_unified_exec.
func TestUnifiedExecLateExecEnd(t *testing.T) {
	requirePTY(t)

	sess, events := newTestSession(t)

	executor := unifiedexec.NewExecutor(nil)
	t.Cleanup(executor.Shutdown)
	// Arm the background watcher (the core bridge for async_watcher.rs).
	armUnifiedExecWatcher(sess, executor)

	execEx := newUnifiedExecCommandExecutor(executor, nil)
	tc := turnWithShellFeatures(t.TempDir(), true)

	out, err := execEx.Handle(context.Background(), &core.ToolHandlerContext{
		Session: sess, Turn: tc, CallID: "orig-call", ToolName: execEx.Name(),
		Payload: tools.FunctionPayload(`{"cmd":"cat","tty":true,"yield_time_ms":300}`),
	})
	if err != nil {
		t.Fatalf("exec_command: %v", err)
	}
	first, ok := out.(unifiedExecToolOutput)
	if !ok || first.processID == nil {
		t.Fatalf("expected a live session, got %#v", out)
	}
	sessionID := *first.processID

	// The tool call has returned; now the process exits in the background.
	if err := executor.Kill(sessionID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	end := drainForExecEnd(t, events)
	if end.CallID != "orig-call" {
		t.Errorf("late end CallID = %q, want orig-call", end.CallID)
	}
	if end.Source != protocol.ExecCommandSourceUnifiedExecStartup {
		t.Errorf("late end Source = %q, want unified_exec_startup", end.Source)
	}
	if end.ProcessID == nil {
		t.Errorf("late end missing process_id")
	}
}

// TestWatcherLateEndTruncatesFormattedOutput verifies that the late
// exec_command_end formats its FormattedOutput with the originating turn's
// truncation policy (the Rust format_exec_output_str(turn.truncation_policy)),
// while the aggregated output stays verbatim. A small byte policy must truncate
// a large transcript, so FormattedOutput differs from AggregatedOutput and
// carries the truncation header.
func TestWatcherLateEndTruncatesFormattedOutput(t *testing.T) {
	sess, events := newTestSession(t)

	largeOutput := strings.Repeat("abcdefghij\n", 500) // ~5500 bytes, many lines.
	policy := truncation.BytesPolicy(64)

	sink := &unifiedExecWatcherSink{
		session:          sess,
		turnID:           "turn-1",
		truncationPolicy: policy,
	}
	sink.ExecEnd(unifiedexec.ExecEndInfo{
		CallID:    "call-1",
		Command:   []string{"cat"},
		Cwd:       "/tmp",
		ProcessID: 1234,
		Output:    largeOutput,
		ExitCode:  0,
		Duration:  time.Second,
	})

	end := drainForExecEnd(t, events)
	if end.AggregatedOutput != largeOutput {
		t.Errorf("AggregatedOutput should be verbatim; got %d bytes, want %d", len(end.AggregatedOutput), len(largeOutput))
	}
	wantFormatted := truncation.FormattedTruncateText(largeOutput, policy)
	if end.FormattedOutput != wantFormatted {
		t.Errorf("FormattedOutput not truncated with policy:\n got: %q\nwant: %q", end.FormattedOutput, wantFormatted)
	}
	if end.FormattedOutput == largeOutput {
		t.Error("FormattedOutput should be truncated, but equals the verbatim output")
	}
	if len(end.FormattedOutput) >= len(largeOutput) {
		t.Errorf("FormattedOutput length %d should be smaller than verbatim %d", len(end.FormattedOutput), len(largeOutput))
	}
	if !strings.Contains(end.FormattedOutput, "Total output lines:") {
		t.Errorf("FormattedOutput missing truncation header: %q", end.FormattedOutput)
	}
}

// TestWatcherLateEndFailureTruncatesFormattedOutput verifies the failed-end arm
// also formats its FormattedOutput with the policy (Rust routes the failed end
// through the same ToolEventFailure::Output -> format_exec_output_str arm).
func TestWatcherLateEndFailureTruncatesFormattedOutput(t *testing.T) {
	sess, events := newTestSession(t)

	largeOutput := strings.Repeat("line-of-text\n", 500)
	policy := truncation.BytesPolicy(48)

	sink := &unifiedExecWatcherSink{
		session:          sess,
		turnID:           "turn-2",
		truncationPolicy: policy,
	}
	sink.ExecEnd(unifiedexec.ExecEndInfo{
		CallID:    "call-2",
		Command:   []string{"cat"},
		Cwd:       "/tmp",
		ProcessID: 99,
		Output:    largeOutput,
		Failure:   "session terminated",
		Duration:  time.Second,
	})

	end := drainForExecEnd(t, events)
	if end.Status != protocol.ExecCommandStatusFailed {
		t.Errorf("Status = %q, want failed", end.Status)
	}
	// The failed-end aggregated output is "stdout\nmessage".
	wantAggregated := largeOutput + "\n" + "session terminated"
	if end.AggregatedOutput != wantAggregated {
		t.Errorf("AggregatedOutput mismatch on failure path")
	}
	wantFormatted := truncation.FormattedTruncateText(wantAggregated, policy)
	if end.FormattedOutput != wantFormatted {
		t.Errorf("failed FormattedOutput not truncated with policy:\n got: %q\nwant: %q", end.FormattedOutput, wantFormatted)
	}
}

// TestArmSessionUnifiedExecWatcherDiscoversExecutor asserts the production wiring
// seam: core.Spawn calls ArmSession on the exec_command executor registered in
// its router (core.SessionArmer), which arms the watcher so a late end is
// emitted through the owning session.
func TestArmSessionUnifiedExecWatcherDiscoversExecutor(t *testing.T) {
	requirePTY(t)

	executor := unifiedexec.NewExecutor(nil)
	t.Cleanup(executor.Shutdown)
	shell := ShellExecutors(Deps{UnifiedExec: executor})
	fixture := coretest.NewSession(t, shell...)
	sess, events := fixture.Session, fixture.Events

	execEx := shell[0].(unifiedExecCommandExecutor)
	tc := turnWithShellFeatures(t.TempDir(), true)
	out, err := execEx.Handle(context.Background(), &core.ToolHandlerContext{
		Session: sess, Turn: tc, CallID: "seam-call", ToolName: execEx.Name(),
		Payload: tools.FunctionPayload(`{"cmd":"cat","tty":true,"yield_time_ms":300}`),
	})
	if err != nil {
		t.Fatalf("exec_command: %v", err)
	}
	live, ok := out.(unifiedExecToolOutput)
	if !ok || live.processID == nil {
		t.Fatalf("expected a live session, got %#v", out)
	}
	if err := executor.Kill(*live.processID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	end := drainForExecEnd(t, events)
	if end.CallID != "seam-call" {
		t.Errorf("late end CallID = %q, want seam-call", end.CallID)
	}
}
