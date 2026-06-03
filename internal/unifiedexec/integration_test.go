package unifiedexec

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/sandbox"
)

// testEnv returns a minimal environment with PATH so spawned shells resolve.
func testEnv() map[string]string {
	return map[string]string{"PATH": os.Getenv("PATH")}
}

func TestExecCommandShortLivedCapturesOutput(t *testing.T) {
	m := NewDefaultProcessManager()
	m.SetDeterministicProcessIDs(true)
	t.Cleanup(m.TerminateAllProcesses)

	pid := m.AllocateProcessID()
	req := &ExecCommandRequest{
		Command:     []string{"/bin/sh", "-c", "printf hello-world"},
		HookCommand: "printf hello-world",
		ProcessID:   pid,
		YieldTimeMS: 2000,
		Cwd:         t.TempDir(),
		Env:         testEnv(),
		TTY:         false,
		SandboxType: sandbox.SandboxTypeNone,
	}

	out, err := m.ExecCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	if !strings.Contains(string(out.RawOutput), "hello-world") {
		t.Fatalf("output = %q, want to contain hello-world", out.RawOutput)
	}
	if out.ProcessID != nil {
		t.Fatalf("ProcessID = %v, want nil for short-lived command", *out.ProcessID)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0", out.ExitCode)
	}
	if out.HookCommand != "printf hello-world" {
		t.Fatalf("HookCommand = %q", out.HookCommand)
	}
	if len(out.ChunkID) != 6 {
		t.Fatalf("ChunkID = %q, want 6 hex chars", out.ChunkID)
	}
}

func TestExecCommandNonZeroExit(t *testing.T) {
	m := NewDefaultProcessManager()
	m.SetDeterministicProcessIDs(true)
	t.Cleanup(m.TerminateAllProcesses)

	pid := m.AllocateProcessID()
	req := &ExecCommandRequest{
		Command:     []string{"/bin/sh", "-c", "exit 3"},
		HookCommand: "exit 3",
		ProcessID:   pid,
		YieldTimeMS: 2000,
		Cwd:         t.TempDir(),
		Env:         testEnv(),
		SandboxType: sandbox.SandboxTypeNone,
	}
	out, err := m.ExecCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	if out.ExitCode == nil || *out.ExitCode != 3 {
		t.Fatalf("ExitCode = %v, want 3", out.ExitCode)
	}
}

func TestExecCommandPersistsAndWriteStdinEchoes(t *testing.T) {
	m := NewDefaultProcessManager()
	m.SetDeterministicProcessIDs(true)
	t.Cleanup(m.TerminateAllProcesses)

	pid := m.AllocateProcessID()
	// `cat` with a tty stays alive and echoes stdin back on stdout.
	req := &ExecCommandRequest{
		Command:     []string{"/bin/cat"},
		HookCommand: "cat",
		ProcessID:   pid,
		YieldTimeMS: 500,
		Cwd:         t.TempDir(),
		Env:         testEnv(),
		TTY:         true,
		SandboxType: sandbox.SandboxTypeNone,
	}
	out, err := m.ExecCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	if out.ProcessID == nil {
		t.Fatalf("ProcessID = nil, want a live session id")
	}
	if *out.ProcessID != pid {
		t.Fatalf("ProcessID = %d, want %d", *out.ProcessID, pid)
	}

	// Write to stdin; the pty echoes input plus cat re-emits it.
	wOut, err := m.WriteStdin(context.Background(), &WriteStdinRequest{
		ProcessID:   pid,
		Input:       "ping\n",
		YieldTimeMS: 1000,
	})
	if err != nil {
		t.Fatalf("WriteStdin: %v", err)
	}
	if !strings.Contains(string(wOut.RawOutput), "ping") {
		t.Fatalf("write output = %q, want to contain ping", wOut.RawOutput)
	}
	if wOut.ProcessID == nil || *wOut.ProcessID != pid {
		t.Fatalf("write ProcessID = %v, want %d", wOut.ProcessID, pid)
	}

	// Kill terminates and removes the session.
	if err := m.Kill(pid); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if status := m.refreshProcessState(pid); status.kind != statusUnknown {
		t.Fatalf("status after kill = %d, want statusUnknown", status.kind)
	}
}

func TestWriteStdinNonTTYIsClosed(t *testing.T) {
	m := NewDefaultProcessManager()
	m.SetDeterministicProcessIDs(true)
	t.Cleanup(m.TerminateAllProcesses)

	pid := m.AllocateProcessID()
	// A long-running non-tty process stays alive so write_stdin can be attempted.
	req := &ExecCommandRequest{
		Command:     []string{"/bin/sh", "-c", "sleep 5"},
		HookCommand: "sleep 5",
		ProcessID:   pid,
		YieldTimeMS: 300,
		Cwd:         t.TempDir(),
		Env:         testEnv(),
		TTY:         false,
		SandboxType: sandbox.SandboxTypeNone,
	}
	out, err := m.ExecCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	if out.ProcessID == nil {
		t.Fatalf("ProcessID = nil, want a live session")
	}

	_, werr := m.WriteStdin(context.Background(), &WriteStdinRequest{
		ProcessID:   pid,
		Input:       "data\n",
		YieldTimeMS: 300,
	})
	if e, ok := werr.(*Error); !ok || e.Kind != ErrStdinClosed {
		t.Fatalf("WriteStdin error = %v, want ErrStdinClosed", werr)
	}
}

func TestWriteStdinUnknownProcess(t *testing.T) {
	m := NewDefaultProcessManager()
	_, err := m.WriteStdin(context.Background(), &WriteStdinRequest{ProcessID: 4242, YieldTimeMS: 100})
	if e, ok := err.(*Error); !ok || e.Kind != ErrUnknownProcessID {
		t.Fatalf("WriteStdin error = %v, want ErrUnknownProcessID", err)
	}
}

func TestExecutorEndToEnd(t *testing.T) {
	exec := NewExecutor(nil)
	exec.Manager().SetDeterministicProcessIDs(true)
	t.Cleanup(exec.Shutdown)

	out, err := exec.ExecCommand(context.Background(), &ExecCommandRequest{
		Command:     []string{"/bin/sh", "-c", "printf done"},
		HookCommand: "printf done",
		YieldTimeMS: 2000,
		Cwd:         t.TempDir(),
		Env:         testEnv(),
		SandboxType: sandbox.SandboxTypeNone,
	})
	if err != nil {
		t.Fatalf("Executor.ExecCommand: %v", err)
	}
	if !strings.Contains(string(out.RawOutput), "done") {
		t.Fatalf("output = %q, want done", out.RawOutput)
	}
}

func TestPollEmptyInputReturnsPromptly(t *testing.T) {
	m := NewProcessManager(MinEmptyYieldTimeMS, SandboxSpawner{})
	m.SetDeterministicProcessIDs(true)
	t.Cleanup(m.TerminateAllProcesses)

	pid := m.AllocateProcessID()
	out, err := m.ExecCommand(context.Background(), &ExecCommandRequest{
		Command:     []string{"/bin/cat"},
		HookCommand: "cat",
		ProcessID:   pid,
		YieldTimeMS: 300,
		Cwd:         t.TempDir(),
		Env:         testEnv(),
		TTY:         true,
		SandboxType: sandbox.SandboxTypeNone,
	})
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	if out.ProcessID == nil {
		t.Fatalf("ProcessID = nil, want live session")
	}

	// An empty poll should return once its (clamped) window elapses; bound the
	// test so a hang is caught.
	done := make(chan struct{})
	go func() {
		_, _ = m.WriteStdin(context.Background(), &WriteStdinRequest{
			ProcessID:   pid,
			Input:       "",
			YieldTimeMS: MinEmptyYieldTimeMS,
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Duration(MinEmptyYieldTimeMS)*time.Millisecond + 5*time.Second):
		t.Fatalf("empty poll did not return within its yield window")
	}
}
