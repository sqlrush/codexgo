package unifiedexec

// Tests for per-process call-id threading: the originating exec_command call id
// is stored on the process entry and echoed back by every write_stdin/poll
// (the Rust ProcessEntry.call_id -> ExecCommandToolOutput.event_call_id flow).

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/internal/sandbox"
)

// TestWriteStdinEchoesOriginatingCallID asserts a write against a live session
// reports the exec_command call id, not the write's own.
func TestWriteStdinEchoesOriginatingCallID(t *testing.T) {
	m := NewDefaultProcessManager()
	m.SetDeterministicProcessIDs(true)
	t.Cleanup(m.TerminateAllProcesses)

	pid := m.AllocateProcessID()
	out, err := m.ExecCommand(context.Background(), &ExecCommandRequest{
		Command:     []string{"/bin/cat"},
		HookCommand: "cat",
		CallID:      "call_exec_origin",
		ProcessID:   pid,
		YieldTimeMS: 500,
		Cwd:         t.TempDir(),
		Env:         testEnv(),
		TTY:         true,
		SandboxType: sandbox.SandboxTypeNone,
	})
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	if out.EventCallID != "call_exec_origin" {
		t.Errorf("exec EventCallID = %q, want call_exec_origin", out.EventCallID)
	}
	if out.ProcessID == nil {
		t.Fatal("cat should stay alive")
	}

	wrote, err := m.WriteStdin(context.Background(), &WriteStdinRequest{
		ProcessID:   *out.ProcessID,
		Input:       "ping\n",
		YieldTimeMS: 500,
	})
	if err != nil {
		t.Fatalf("WriteStdin: %v", err)
	}
	if wrote.EventCallID != "call_exec_origin" {
		t.Errorf("write EventCallID = %q, want the originating exec call id", wrote.EventCallID)
	}
}
