package core

import (
	"context"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
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

	out, err := execEx.Handle(context.Background(), &toolHandlerContext{
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

// TestArmSessionUnifiedExecWatcherDiscoversExecutor asserts the production wiring
// seam: armSessionUnifiedExecWatcher finds the unified-exec executor on the
// session's DefaultToolRouter and arms the watcher so a late end is emitted.
func TestArmSessionUnifiedExecWatcherDiscoversExecutor(t *testing.T) {
	requirePTY(t)

	sess, events := newTestSession(t)

	executor := unifiedexec.NewExecutor(nil)
	t.Cleanup(executor.Shutdown)
	router, err := BuiltinToolRouter(BuiltinToolDeps{UnifiedExec: executor})
	if err != nil {
		t.Fatalf("BuiltinToolRouter: %v", err)
	}
	sess.services.ToolRouter = router

	// Discover-and-arm through the same seam Spawn uses in production.
	armSessionUnifiedExecWatcher(sess)

	execEx := newUnifiedExecCommandExecutor(executor, nil)
	tc := turnWithShellFeatures(t.TempDir(), true)
	out, err := execEx.Handle(context.Background(), &toolHandlerContext{
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
