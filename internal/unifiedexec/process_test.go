package unifiedexec

import (
	"context"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/execserver"
	"github.com/sqlrush/codexgo/internal/sandbox"
)

// mockExecProcess is a minimal execserver.ExecProcess for transport tests. It
// returns a fixed write status and exposes no live events (the write-status
// transitions are what these tests exercise).
type mockExecProcess struct {
	processID   execserver.ProcessId
	writeStatus execserver.WriteStatus
}

func (p *mockExecProcess) ProcessID() execserver.ProcessId { return p.processID }

// SubscribeEvents returns an empty receiver: an ExecProcessEventReceiver with a
// live channel cannot be constructed from outside execserver, so these tests
// drive state through Write instead of the event stream.
func (p *mockExecProcess) SubscribeEvents() *execserver.ExecProcessEventReceiver {
	return execserver.EmptyEventReceiver()
}

func (p *mockExecProcess) Read(ctx context.Context, afterSeq *uint64, maxBytes *int, waitMs *uint64) (execserver.ReadResponse, error) {
	return execserver.ReadResponse{NextSeq: 1}, nil
}

func (p *mockExecProcess) Write(ctx context.Context, chunk []byte) (execserver.WriteResponse, error) {
	return execserver.WriteResponse{Status: p.writeStatus}, nil
}

func (p *mockExecProcess) Terminate(ctx context.Context) error { return nil }

// newRemoteProcess builds a Process wrapping a mock exec-server process with the
// given write status. The EmptyEventReceiver means runExecServerOutputTask will
// observe a closed stream and finalize state; for the write-status tests this is
// fine because Write drives the state transitions under test.
func newRemoteProcess(t *testing.T, status execserver.WriteStatus) *Process {
	t.Helper()
	mock := &mockExecProcess{
		processID:   execserver.NewProcessId("test-process"),
		writeStatus: status,
	}
	started := execserver.StartedExecProcess{Process: mock}
	process, err := fromExecServerStarted(started, sandbox.SandboxTypeNone)
	if err != nil {
		t.Fatalf("fromExecServerStarted: %v", err)
	}
	return process
}

func TestRemoteWriteUnknownProcessMarksExited(t *testing.T) {
	process := newRemoteProcess(t, execserver.WriteStatusUnknownProcess)
	err := process.Write(context.Background(), []byte("hello"))
	if e, ok := err.(*Error); !ok || e.Kind != ErrWriteToStdin {
		t.Fatalf("Write error = %v, want ErrWriteToStdin", err)
	}
	if !process.HasExited() {
		t.Fatalf("process not marked exited after unknown-process write")
	}
}

func TestRemoteWriteClosedStdinMarksExited(t *testing.T) {
	process := newRemoteProcess(t, execserver.WriteStatusStdinClosed)
	err := process.Write(context.Background(), []byte("hello"))
	if e, ok := err.(*Error); !ok || e.Kind != ErrWriteToStdin {
		t.Fatalf("Write error = %v, want ErrWriteToStdin", err)
	}
	if !process.HasExited() {
		t.Fatalf("process not marked exited after closed-stdin write")
	}
}

func TestRemoteWriteStartingFails(t *testing.T) {
	process := newRemoteProcess(t, execserver.WriteStatusStarting)
	err := process.Write(context.Background(), []byte("hello"))
	if e, ok := err.(*Error); !ok || e.Kind != ErrWriteToStdin {
		t.Fatalf("Write error = %v, want ErrWriteToStdin", err)
	}
}

func TestFailAndTerminatePreservesFailureMessage(t *testing.T) {
	process := newRemoteProcess(t, execserver.WriteStatusAccepted)
	process.FailAndTerminate("network denied")
	process.FailAndTerminate("second failure")

	if !process.HasExited() {
		t.Fatalf("process not exited after fail_and_terminate")
	}
	msg, ok := process.FailureMessage()
	if !ok || msg != "network denied" {
		t.Fatalf("FailureMessage = %q (%v), want network denied", msg, ok)
	}
}

func TestCheckForSandboxDenialNoneSandboxIsOK(t *testing.T) {
	process := newRemoteProcess(t, execserver.WriteStatusAccepted)
	// SandboxTypeNone => denial check is always a no-op.
	if err := process.checkForSandboxDenialWithText("anything"); err != nil {
		t.Fatalf("checkForSandboxDenialWithText = %v, want nil", err)
	}
}

func TestNotifyWaitersWakesWaiter(t *testing.T) {
	n := newNotify()
	w := n.waiter()
	go func() {
		time.Sleep(10 * time.Millisecond)
		n.notifyWaiters()
	}()
	select {
	case <-w:
		// woken
	case <-time.After(time.Second):
		t.Fatalf("waiter not woken by notifyWaiters")
	}
}
